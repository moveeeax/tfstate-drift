package inventory

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/drift"
	"github.com/moveeeax/tfstate-drift/internal/runner"
)

// Workspace statuses.
const (
	StatusUnchecked   = "unchecked"   // state read, no refresh requested
	StatusClean       = "clean"       // refresh-only plan found no drift
	StatusDrifted     = "drifted"     // refresh-only plan found drift
	StatusUnreachable = "unreachable" // state pull or plan failed
)

// Inventory is the schema-stable result of `tfstate-drift workspaces`. Like
// drift.Report, fields are only ever added, never renamed or removed.
type Inventory struct {
	WorkspaceCount   int         `json:"workspace_count"`
	StaleCount       int         `json:"stale_count"`
	DriftedCount     int         `json:"drifted_count"`
	UnreachableCount int         `json:"unreachable_count"`
	Workspaces       []Workspace `json:"workspaces"`
}

// Workspace is one workspace of one working directory. LastWrite and AgeDays
// are null when the backend does not expose a timestamp.
type Workspace struct {
	Dir              string         `json:"dir"`
	Backend          string         `json:"backend"`
	Name             string         `json:"name"`
	LastWrite        *time.Time     `json:"last_write"`
	AgeDays          *int           `json:"age_days"`
	Stale            bool           `json:"stale"`
	Serial           uint64         `json:"serial"`
	Lineage          string         `json:"lineage"`
	TerraformVersion string         `json:"terraform_version"`
	ResourceCount    int            `json:"resource_count"`
	ResourcesByType  map[string]int `json:"resources_by_type"`
	Providers        []string       `json:"providers"`
	Status           string         `json:"status"`
	DriftCount       int            `json:"drift_count"`
	Error            string         `json:"error,omitempty"`
	LastWriteError   string         `json:"last_write_error,omitempty"`
}

// Source runs the Terraform commands the inventory needs against one
// working directory.
type Source interface {
	Workspaces(ctx context.Context) ([]string, error)
	StatePull(ctx context.Context, workspace string) ([]byte, error)
	Plan(ctx context.Context, workspace string) ([]byte, error)
}

// TerraformSource is the real Source: it shells out to Terraform in Dir and
// selects workspaces through TF_WORKSPACE, so the workspace selected in Dir
// is left as it was.
type TerraformSource struct {
	Dir, Binary string
}

func (s TerraformSource) tf(workspace string) *runner.Terraform {
	t := runner.NewTerraform(s.Dir, s.Binary)
	t.Workspace = workspace
	return t
}

func (s TerraformSource) Workspaces(ctx context.Context) ([]string, error) {
	return s.tf("").Workspaces(ctx)
}

func (s TerraformSource) StatePull(ctx context.Context, workspace string) ([]byte, error) {
	return s.tf(workspace).StatePull(ctx)
}

func (s TerraformSource) Plan(ctx context.Context, workspace string) ([]byte, error) {
	return s.tf(workspace).Plan(ctx)
}

// Options tunes Collect.
type Options struct {
	// Refresh runs a refresh-only plan per workspace to set its status.
	Refresh bool
	// StaleAfter marks a workspace stale when its state was last written
	// longer ago than this. Zero disables the check.
	StaleAfter time.Duration
	// Now is the reference time for ages; the zero value means time.Now().
	Now time.Time
	// Drift is passed to drift.Detect for refreshed workspaces.
	Drift drift.Options
}

// Collect inventories every workspace of one working directory. Listing the
// workspaces is the only step whose failure aborts; a workspace whose state
// or plan cannot be read is reported as unreachable. times may be nil.
func Collect(ctx context.Context, dir, backend string, src Source, times LastWriter, opts Options) ([]Workspace, error) {
	names, err := src.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	out := make([]Workspace, 0, len(names))
	for _, name := range names {
		ws := Workspace{
			Dir:             dir,
			Backend:         backend,
			Name:            name,
			ResourcesByType: map[string]int{},
			Providers:       []string{},
			Status:          StatusUnchecked,
		}
		if times != nil {
			t, ok, err := times.LastWrite(ctx, name)
			switch {
			case err != nil:
				ws.LastWriteError = oneLine(err.Error())
			case ok:
				t = t.UTC()
				age := int(now.Sub(t).Hours() / 24)
				ws.LastWrite, ws.AgeDays = &t, &age
				ws.Stale = opts.StaleAfter > 0 && now.Sub(t) > opts.StaleAfter
			}
		}

		data, err := src.StatePull(ctx, name)
		var st *State
		if err == nil {
			st, err = ParseState(data)
		}
		if err != nil {
			ws.Status, ws.Error = StatusUnreachable, oneLine(err.Error())
			out = append(out, ws)
			continue
		}
		ws.Serial, ws.Lineage, ws.TerraformVersion = st.Serial, st.Lineage, st.TerraformVersion
		ws.ResourcesByType = st.ManagedByType()
		for _, n := range ws.ResourcesByType {
			ws.ResourceCount += n
		}
		ws.Providers = st.Providers()

		if opts.Refresh {
			refresh(ctx, src, &ws, opts.Drift)
		}
		out = append(out, ws)
	}
	return out, nil
}

func refresh(ctx context.Context, src Source, ws *Workspace, opts drift.Options) {
	data, err := src.Plan(ctx, ws.Name)
	var plan *drift.Plan
	if err == nil {
		plan, err = drift.Parse(data)
	}
	if err != nil {
		ws.Status, ws.Error = StatusUnreachable, oneLine(err.Error())
		return
	}
	report := drift.Detect(plan, opts)
	ws.DriftCount = report.ResourceCount
	ws.Status = StatusClean
	if report.DriftDetected {
		ws.Status = StatusDrifted
	}
}

// Summarize sorts the workspaces by staleness, oldest state first, and counts
// them. Workspaces without a known last write follow, then ties sort by
// directory and name.
func Summarize(workspaces []Workspace) *Inventory {
	inv := &Inventory{Workspaces: workspaces}
	if inv.Workspaces == nil {
		inv.Workspaces = []Workspace{}
	}
	sort.SliceStable(inv.Workspaces, func(i, j int) bool {
		a, b := inv.Workspaces[i], inv.Workspaces[j]
		switch {
		case a.LastWrite != nil && b.LastWrite != nil && !a.LastWrite.Equal(*b.LastWrite):
			return a.LastWrite.Before(*b.LastWrite)
		case (a.LastWrite == nil) != (b.LastWrite == nil):
			return a.LastWrite != nil
		case a.Dir != b.Dir:
			return a.Dir < b.Dir
		default:
			return a.Name < b.Name
		}
	})
	for _, ws := range inv.Workspaces {
		inv.WorkspaceCount++
		if ws.Stale {
			inv.StaleCount++
		}
		switch ws.Status {
		case StatusDrifted:
			inv.DriftedCount++
		case StatusUnreachable:
			inv.UnreachableCount++
		}
	}
	return inv
}

// oneLine squeezes an error onto one line of at most 300 characters for
// tables and CSV: Terraform's diagnostics run to many lines and may carry
// color codes.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(ansiEscape.ReplaceAllString(s, "")), " ")
	if r := []rune(s); len(r) > maxErrorLen {
		s = string(r[:maxErrorLen-1]) + "…"
	}
	return s
}

const maxErrorLen = 300

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
