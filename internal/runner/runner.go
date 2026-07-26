// Package runner isolates every cloud/network side effect behind an interface.
// The drift-detection and rendering logic never touch Terraform directly; they
// operate on plan JSON bytes, which is what makes them unit-testable offline.
package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// Binary is the terraform executable; defaults to "terraform".
	Binary string
}

// NewTerraform builds a Terraform runner for the given working directory.
func NewTerraform(dir string) *Terraform {
	return &Terraform{Dir: dir, Binary: "terraform"}
}

// Plan produces a refresh-only plan and returns `terraform show -json` output.
// It writes the binary plan to a temp file so the drift section carries full
// attribute-level before/after values (the -json log stream does not).
func (t *Terraform) Plan(ctx context.Context) ([]byte, error) {
	bin := t.Binary
	if bin == "" {
		bin = "terraform"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%q not found on PATH: %w", bin, err)
	}

	tmp, err := os.MkdirTemp("", "tfstate-drift-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	planFile := filepath.Join(tmp, "refresh.tfplan")

	planArgs := []string{}
	if t.Dir != "" {
		planArgs = append(planArgs, "-chdir="+t.Dir)
	}
	planArgs = append(planArgs, "plan", "-refresh-only", "-input=false", "-out="+planFile)
	if out, err := t.run(ctx, bin, planArgs...); err != nil {
		return nil, fmt.Errorf("terraform plan failed: %w\n%s", err, out)
	}

	showArgs := []string{}
	if t.Dir != "" {
		showArgs = append(showArgs, "-chdir="+t.Dir)
	}
	showArgs = append(showArgs, "show", "-json", planFile)
	out, err := t.run(ctx, bin, showArgs...)
	if err != nil {
		return nil, fmt.Errorf("terraform show failed: %w\n%s", err, out)
	}
	return out, nil
}

func (t *Terraform) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	return cmd.Output()
}
