package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeProvider is the kind of offline stand-in tests and the scan command can
// use in place of real Terraform.
type fakeProvider struct{ data []byte }

func (f fakeProvider) Plan(context.Context) ([]byte, error) { return f.data, nil }

func TestPlanProviderInterface(t *testing.T) {
	// Compile-time guarantee that the real runner satisfies the interface.
	var _ PlanProvider = (*Terraform)(nil)

	// A fake provider is usable exactly where the real one is.
	var p PlanProvider = fakeProvider{data: []byte(`{"resource_drift":[]}`)}
	out, err := p.Plan(context.Background())
	if err != nil {
		t.Fatalf("fake provider: %v", err)
	}
	if string(out) != `{"resource_drift":[]}` {
		t.Fatalf("unexpected fake output: %s", out)
	}
}

func TestTerraformMissingBinary(t *testing.T) {
	tf := &Terraform{Dir: ".", Binary: "terraform-does-not-exist-xyz"}
	if _, err := tf.Plan(context.Background()); err == nil {
		t.Fatal("expected error when terraform binary is missing")
	}
}

func TestParseWorkspaceList(t *testing.T) {
	out := []byte("  default\n* prod\n  staging\n\n")
	got := ParseWorkspaceList(out)
	want := []string{"default", "prod", "staging"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// fakeTerraform writes a shell script that echoes its arguments and
// TF_WORKSPACE, or fails with a message on stderr when asked to.
func fakeTerraform(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in needs a POSIX shell")
	}
	bin := filepath.Join(t.TempDir(), "terraform")
	script := "#!/bin/sh\n" +
		"case \"$*\" in *fail*) echo \"backend unreachable\" >&2; exit 1;; esac\n" +
		"echo \"args=$* ws=$TF_WORKSPACE\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestStatePullUsesWorkspaceEnv(t *testing.T) {
	tf := &Terraform{Dir: "infra", Binary: fakeTerraform(t), Workspace: "prod"}
	out, err := tf.StatePull(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "args=-chdir=infra state pull ws=prod" {
		t.Fatalf("got %q", got)
	}
}

func TestRunReportsStderr(t *testing.T) {
	tf := &Terraform{Dir: "fail", Binary: fakeTerraform(t)}
	_, err := tf.Workspaces(context.Background())
	if err == nil || !strings.Contains(err.Error(), "backend unreachable") {
		t.Fatalf("error should carry terraform stderr, got %v", err)
	}
}

func TestNewTerraformDefaultsBinary(t *testing.T) {
	if got := NewTerraform(".", "").Binary; got != "terraform" {
		t.Errorf("default binary = %q", got)
	}
	if got := NewTerraform(".", "tofu").Binary; got != "tofu" {
		t.Errorf("binary = %q, want tofu", got)
	}
}
