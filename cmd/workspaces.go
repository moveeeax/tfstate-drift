package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/drift"
	"github.com/moveeeax/tfstate-drift/internal/inventory"
	"github.com/moveeeax/tfstate-drift/internal/render"
	"github.com/spf13/cobra"
)

// newSource is swapped by tests for a Source that serves fixtures.
var newSource = func(dir, bin string) inventory.Source {
	return inventory.TerraformSource{Dir: dir, Binary: bin}
}

func newWorkspacesCmd() *cobra.Command {
	var (
		dirs         []string
		format       string
		refresh      bool
		staleAfter   string
		terraformBin string
		ignore       []string
	)

	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "Inventory workspaces with last write time, resource counts and drift status",
		Long: "List every workspace of the backend configured in each --chdir with when its\n" +
			"state was last written, its serial, resource counts by type and providers,\n" +
			"oldest first. --refresh adds a refresh-only plan per workspace; with it the\n" +
			"command exits with code 2 when any workspace has drifted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "table" && format != "json" && format != "csv" {
				return fmt.Errorf("invalid --format %q: want \"table\", \"json\" or \"csv\"", format)
			}
			stale, err := parseAge(staleAfter)
			if err != nil {
				return fmt.Errorf("invalid --stale-after: %w", err)
			}

			var all []inventory.Workspace
			for _, dir := range dirs {
				rules, err := loadIgnoreRules(dir, ignore)
				if err != nil {
					return err
				}
				ws, err := collectDir(cmd.Context(), dir, terraformBin, inventory.Options{
					Refresh:    refresh,
					StaleAfter: stale,
					Drift:      drift.Options{Ignore: rules},
				})
				if err != nil {
					return fmt.Errorf("%s: %w", dir, err)
				}
				all = append(all, ws...)
			}
			inv := inventory.Summarize(all)

			var out []byte
			switch format {
			case "json":
				out, err = render.WorkspacesJSON(inv)
			case "csv":
				out, err = render.WorkspacesCSV(inv)
			default:
				out = []byte(render.WorkspacesTable(inv))
			}
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(out))

			if inv.DriftedCount > 0 {
				drifted := 0
				for _, ws := range inv.Workspaces {
					drifted += ws.DriftCount
				}
				return &DriftError{Count: drifted}
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&dirs, "chdir", []string{"."}, "initialized Terraform working directory (repeatable, one backend each)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json|csv")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "run a refresh-only plan per workspace and report clean, drifted or unreachable")
	cmd.Flags().StringVar(&staleAfter, "stale-after", "90d", "mark workspaces whose state was last written longer ago as stale (`age` like 90d or 36h; 0 disables)")
	cmd.Flags().StringVar(&terraformBin, "terraform-bin", "terraform", "terraform executable to run, e.g. tofu")
	cmd.Flags().StringArrayVar(&ignore, "ignore", nil, "with --refresh, ignore drift matching `<address glob>[:<attr path glob>]` (repeatable; adds to "+drift.IgnoreFile+" in each --chdir)")
	return cmd
}

// collectDir inventories one working directory. A backend whose timestamps
// cannot be read (no AWS config, say) still yields the rest of the inventory,
// with the reason in last_write_error.
func collectDir(ctx context.Context, dir, bin string, opts inventory.Options) ([]inventory.Workspace, error) {
	backend, err := inventory.ReadBackend(dir)
	if err != nil {
		return nil, err
	}
	times, err := inventory.NewLastWriter(ctx, dir, backend)
	if err != nil {
		times = failingLastWriter{err}
	}
	return inventory.Collect(ctx, dir, backend.Type, newSource(dir, bin), times, opts)
}

type failingLastWriter struct{ err error }

func (f failingLastWriter) LastWrite(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, f.err
}

// parseAge accepts Go durations plus a day suffix ("90d").
func parseAge(s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("%q is negative", s)
	}
	return d, nil
}
