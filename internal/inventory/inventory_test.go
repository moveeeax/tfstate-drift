package inventory

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

// fakeSource serves per-workspace state and plan bytes; a missing entry is an
// error, as if the backend were unreachable.
type fakeSource struct {
	names   []string
	listErr error
	states  map[string][]byte
	plans   map[string][]byte
	planned []string
}

func (f *fakeSource) Workspaces(context.Context) ([]string, error) { return f.names, f.listErr }

func (f *fakeSource) StatePull(_ context.Context, ws string) ([]byte, error) {
	if b, ok := f.states[ws]; ok {
		return b, nil
	}
	return nil, errors.New("terraform state pull failed: exit status 1\n\x1b[31mError: \x1b[0mAccessDenied\n  details")
}

func (f *fakeSource) Plan(_ context.Context, ws string) ([]byte, error) {
	f.planned = append(f.planned, ws)
	if b, ok := f.plans[ws]; ok {
		return b, nil
	}
	return nil, errors.New("terraform plan failed: exit status 1")
}

type fakeTimes map[string]time.Time

func (f fakeTimes) LastWrite(_ context.Context, ws string) (time.Time, bool, error) {
	if ws == "broken" {
		return time.Time{}, false, errors.New("head object: forbidden")
	}
	t, ok := f[ws]
	return t, ok, nil
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var now = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

func newFixtureSource(t *testing.T) *fakeSource {
	state := read(t, "testdata/state.json")
	return &fakeSource{
		names: []string{"default", "prod", "old", "gone", "broken"},
		states: map[string][]byte{
			"default": {}, "prod": state, "old": state, "broken": state,
		},
		plans: map[string][]byte{
			"prod": read(t, "../../examples/drift-plan.json"),
			"old":  read(t, "../../examples/no-drift-plan.json"),
		},
	}
}

func byName(ws []Workspace) map[string]Workspace {
	m := map[string]Workspace{}
	for _, w := range ws {
		m[w.Name] = w
	}
	return m
}

func TestCollectWithoutRefresh(t *testing.T) {
	src := newFixtureSource(t)
	times := fakeTimes{
		"prod": now.Add(-10 * 24 * time.Hour),
		"old":  now.Add(-200 * 24 * time.Hour),
	}
	ws, err := Collect(context.Background(), "infra", "s3", src, times, Options{StaleAfter: 90 * 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.planned) != 0 {
		t.Fatalf("planned %v without --refresh", src.planned)
	}
	got := byName(ws)

	prod := got["prod"]
	if prod.Status != StatusUnchecked || prod.Serial != 42 || prod.ResourceCount != 4 || prod.Stale {
		t.Errorf("prod: %+v", prod)
	}
	if prod.Dir != "infra" || prod.Backend != "s3" || *prod.AgeDays != 10 {
		t.Errorf("prod dir/backend/age: %+v", prod)
	}
	if old := got["old"]; !old.Stale || *old.AgeDays != 200 {
		t.Errorf("old should be stale: %+v", old)
	}
	if def := got["default"]; def.Status != StatusUnchecked || def.LastWrite != nil || def.ResourceCount != 0 || def.Providers == nil {
		t.Errorf("empty default workspace: %+v", def)
	}
	gone := got["gone"]
	if gone.Status != StatusUnreachable || gone.Error != "terraform state pull failed: exit status 1 Error: AccessDenied details" {
		t.Errorf("gone: status=%s error=%q", gone.Status, gone.Error)
	}
	if b := got["broken"]; b.LastWriteError != "head object: forbidden" || b.Status != StatusUnchecked {
		t.Errorf("broken: %+v", b)
	}
}

func TestCollectRefresh(t *testing.T) {
	src := newFixtureSource(t)
	ws, err := Collect(context.Background(), ".", "local", src, nil, Options{Refresh: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	got := byName(ws)
	if p := got["prod"]; p.Status != StatusDrifted || p.DriftCount != 3 {
		t.Errorf("prod: %s %d", p.Status, p.DriftCount)
	}
	if o := got["old"]; o.Status != StatusClean {
		t.Errorf("old: %s", o.Status)
	}
	if b := got["broken"]; b.Status != StatusUnreachable || !strings.Contains(b.Error, "plan failed") {
		t.Errorf("broken: %+v", b)
	}
	// No plan for a workspace whose state could not be read.
	for _, p := range src.planned {
		if p == "gone" {
			t.Error("planned an unreachable workspace")
		}
	}
}

func TestCollectRefreshAppliesIgnoreRules(t *testing.T) {
	src := newFixtureSource(t)
	opts := Options{Refresh: true, Drift: drift.Options{Ignore: []drift.IgnoreRule{{Address: "*"}}}}
	ws, err := Collect(context.Background(), ".", "local", src, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if p := byName(ws)["prod"]; p.Status != StatusClean || p.DriftCount != 0 {
		t.Errorf("ignored drift should be clean: %+v", p)
	}
}

func TestCollectListError(t *testing.T) {
	src := &fakeSource{listErr: errors.New("backend not initialized")}
	if _, err := Collect(context.Background(), ".", "local", src, nil, Options{}); err == nil {
		t.Fatal("expected the workspace list error")
	}
}

func TestSummarizeSortsByStaleness(t *testing.T) {
	at := func(days int) *time.Time {
		t := now.Add(-time.Duration(days) * 24 * time.Hour)
		return &t
	}
	inv := Summarize([]Workspace{
		{Dir: "b", Name: "unknown2"},
		{Dir: "a", Name: "fresh", LastWrite: at(1), Status: StatusDrifted},
		{Dir: "a", Name: "unknown1", Status: StatusUnreachable},
		{Dir: "a", Name: "ancient", LastWrite: at(400), Stale: true},
		{Dir: "a", Name: "older", LastWrite: at(100), Stale: true, Status: StatusClean},
	})
	var order []string
	for _, w := range inv.Workspaces {
		order = append(order, w.Name)
	}
	if got := strings.Join(order, ","); got != "ancient,older,fresh,unknown1,unknown2" {
		t.Errorf("order = %s", got)
	}
	if inv.WorkspaceCount != 5 || inv.StaleCount != 2 || inv.DriftedCount != 1 || inv.UnreachableCount != 1 {
		t.Errorf("counts: %+v", inv)
	}
	if empty := Summarize(nil); empty.Workspaces == nil {
		t.Error("workspaces should encode as [] not null")
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("a\n\n  b\x1b[1mc\x1b[0m "); got != "a bc" {
		t.Errorf("got %q", got)
	}
	long := oneLine(strings.Repeat("x", 1000))
	if r := []rune(long); len(r) != maxErrorLen || !strings.HasSuffix(long, "…") {
		t.Errorf("not truncated: %d", len(r))
	}
}
