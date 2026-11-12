package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/inventory"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func slackServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posts = append(posts, string(b))
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

func TestScanNotifiesOnDrift(t *testing.T) {
	srv, posts := slackServer(t)
	_, err := execute(t, "scan", "--chdir", t.TempDir(), "--plan-json", "../examples/drift-plan.json", "--notify", "slack="+srv.URL)
	var de *DriftError
	if !errors.As(err, &de) || de.Count != 3 {
		t.Fatalf("want DriftError(3), got %v", err)
	}
	if len(*posts) != 1 || !strings.Contains((*posts)[0], "Terraform drift: 3 resource(s)") {
		t.Fatalf("posts = %q", *posts)
	}
}

func TestScanQuietOnCleanRun(t *testing.T) {
	srv, posts := slackServer(t)
	t.Setenv("TEST_TFSTATE_DRIFT_HOOK", srv.URL)
	_, err := execute(t, "scan", "--chdir", t.TempDir(), "--plan-json", "../examples/no-drift-plan.json", "--notify", "slack=env:TEST_TFSTATE_DRIFT_HOOK")
	if err != nil {
		t.Fatal(err)
	}
	if len(*posts) != 0 {
		t.Fatalf("clean run posted %q", *posts)
	}
}

func TestScanNotifyFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := execute(t, "scan", "--chdir", t.TempDir(), "--plan-json", "../examples/drift-plan.json", "--notify", "slack="+srv.URL)
	var de *DriftError
	if err == nil || errors.As(err, &de) || !strings.Contains(err.Error(), "notify slack") {
		t.Fatalf("want a notify error, got %v", err)
	}
}

func TestScanRejectsBadNotifyBeforeRunning(t *testing.T) {
	_, err := execute(t, "scan", "--chdir", t.TempDir(), "--notify", "slack=http://hooks.example.com/x", "--terraform-bin", "terraform-does-not-exist-xyz")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("want webhook validation error, got %v", err)
	}
}

func TestScanTerraformBin(t *testing.T) {
	_, err := execute(t, "scan", "--chdir", t.TempDir(), "--terraform-bin", "tofu-does-not-exist-xyz")
	if err == nil || !strings.Contains(err.Error(), `"tofu-does-not-exist-xyz" not found`) {
		t.Fatalf("got %v", err)
	}
}

// stubSource serves one drifted and one clean workspace.
type stubSource struct{}

func (s stubSource) Workspaces(context.Context) ([]string, error) {
	return []string{"default", "prod"}, nil
}

func (s stubSource) StatePull(_ context.Context, ws string) ([]byte, error) {
	return os.ReadFile("../internal/inventory/testdata/state.json")
}

func (s stubSource) Plan(_ context.Context, ws string) ([]byte, error) {
	if ws == "prod" {
		return os.ReadFile("../examples/drift-plan.json")
	}
	return os.ReadFile("../examples/no-drift-plan.json")
}

func stubTerraform(t *testing.T) *[]string {
	t.Helper()
	var bins []string
	prev := newSource
	newSource = func(dir, bin string) inventory.Source {
		bins = append(bins, bin)
		return stubSource{}
	}
	t.Cleanup(func() { newSource = prev })
	t.Setenv("TF_DATA_DIR", "")
	return &bins
}

func TestWorkspacesJSON(t *testing.T) {
	bins := stubTerraform(t)
	out, err := execute(t, "workspaces", "--chdir", t.TempDir(), "--format", "json", "--terraform-bin", "tofu")
	if err != nil {
		t.Fatal(err)
	}
	var inv inventory.Inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if inv.WorkspaceCount != 2 || inv.Workspaces[0].Status != inventory.StatusUnchecked || inv.Workspaces[0].Backend != "local" {
		t.Fatalf("got %+v", inv)
	}
	if len(*bins) != 1 || (*bins)[0] != "tofu" {
		t.Errorf("terraform bin = %q", *bins)
	}
}

func TestWorkspacesRefreshExitsOnDrift(t *testing.T) {
	stubTerraform(t)
	out, err := execute(t, "workspaces", "--chdir", t.TempDir(), "--refresh")
	var de *DriftError
	if !errors.As(err, &de) || de.Count != 3 {
		t.Fatalf("want DriftError(3), got %v", err)
	}
	if !strings.Contains(out, "1 drifted") || !strings.Contains(out, "drifted (3)") || !strings.Contains(out, "clean") {
		t.Errorf("table:\n%s", out)
	}

	// The same drift ignored in flags is clean.
	if _, err := execute(t, "workspaces", "--chdir", t.TempDir(), "--refresh", "--ignore", "*"); err != nil {
		t.Fatalf("ignored drift should exit 0, got %v", err)
	}
}

func TestWorkspacesFlagValidation(t *testing.T) {
	stubTerraform(t)
	if _, err := execute(t, "workspaces", "--format", "xml"); err == nil {
		t.Error("accepted --format xml")
	}
	if _, err := execute(t, "workspaces", "--stale-after", "soon"); err == nil {
		t.Error("accepted --stale-after soon")
	}
}

func TestParseAge(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"":    0,
		"0":   0,
		"90d": 90 * 24 * time.Hour,
		"36h": 36 * time.Hour,
		"0d":  0,
	} {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"-1d", "xd", "-5h", "ninety"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q) accepted", in)
		}
	}
}
