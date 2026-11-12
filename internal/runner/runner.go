// Package runner isolates every cloud/network side effect behind an interface.
// The drift-detection and rendering logic never touch Terraform directly; they
// operate on plan JSON bytes, which is what makes them unit-testable offline.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PlanProvider yields a `terraform show -json` plan document for a refresh-only
// plan. The real implementation shells out to Terraform; tests substitute a
// fake that returns fixture bytes.
type PlanProvider interface {
	Plan(ctx context.Context) ([]byte, error)
}

// Terraform runs a refresh-only plan in Dir and returns its JSON representation.
type Terraform struct {
	// Dir is the working directory passed to `terraform -chdir`.
	Dir string
	// Binary is the terraform executable; defaults to "terraform". OpenTofu
	// users set it to "tofu", which accepts the same commands.
	Binary string
	// Workspace, when set, is passed as TF_WORKSPACE so commands act on that
	// workspace without changing the one selected in Dir.
	Workspace string
}

// NewTerraform builds a Terraform runner for the given working directory.
// An empty bin means "terraform".
func NewTerraform(dir, bin string) *Terraform {
	if bin == "" {
		bin = "terraform"
	}
	return &Terraform{Dir: dir, Binary: bin}
}

// Plan produces a refresh-only plan and returns `terraform show -json` output.
// It writes the binary plan to a temp file so the drift section carries full
// attribute-level before/after values (the -json log stream does not).
func (t *Terraform) Plan(ctx context.Context) ([]byte, error) {
	bin, err := t.binary()
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "tfstate-drift-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	planFile := filepath.Join(tmp, "refresh.tfplan")

	if out, err := t.run(ctx, bin, t.args("plan", "-refresh-only", "-input=false", "-out="+planFile)...); err != nil {
		return nil, fmt.Errorf("terraform plan failed: %w\n%s", err, out)
	}

	out, err := t.run(ctx, bin, t.args("show", "-json", planFile)...)
	if err != nil {
		return nil, fmt.Errorf("terraform show failed: %w\n%s", err, out)
	}
	return out, nil
}

// Workspaces lists the workspaces of the backend configured in Dir, in the
// order `terraform workspace list` prints them.
func (t *Terraform) Workspaces(ctx context.Context) ([]string, error) {
	bin, err := t.binary()
	if err != nil {
		return nil, err
	}
	out, err := t.run(ctx, bin, t.args("workspace", "list")...)
	if err != nil {
		return nil, fmt.Errorf("terraform workspace list failed: %w\n%s", err, out)
	}
	return ParseWorkspaceList(out), nil
}

// StatePull returns the raw state of the current (or Workspace) workspace as
// `terraform state pull` prints it. It reads the backend and never writes.
func (t *Terraform) StatePull(ctx context.Context) ([]byte, error) {
	bin, err := t.binary()
	if err != nil {
		return nil, err
	}
	out, err := t.run(ctx, bin, t.args("state", "pull")...)
	if err != nil {
		return nil, fmt.Errorf("terraform state pull failed: %w\n%s", err, out)
	}
	return out, nil
}

// ParseWorkspaceList extracts workspace names from `terraform workspace list`
// output, where the selected workspace is prefixed with "* ".
func ParseWorkspaceList(out []byte) []string {
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (t *Terraform) binary() (string, error) {
	bin := t.Binary
	if bin == "" {
		bin = "terraform"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%q not found on PATH: %w", bin, err)
	}
	return bin, nil
}

func (t *Terraform) args(args ...string) []string {
	if t.Dir == "" {
		return args
	}
	return append([]string{"-chdir=" + t.Dir}, args...)
}

// run executes bin and returns stdout. On failure the returned bytes hold
// stderr, which is where Terraform explains what went wrong.
func (t *Terraform) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if t.Workspace != "" {
		cmd.Env = append(os.Environ(), "TF_WORKSPACE="+t.Workspace)
	}
	out, err := cmd.Output()
	var ee *exec.ExitError
	if err != nil && errors.As(err, &ee) {
		return ee.Stderr, err
	}
	return out, err
}
