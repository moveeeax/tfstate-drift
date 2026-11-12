package cmd

import (
	"fmt"
	"os"

	"github.com/moveeeax/tfstate-drift/internal/drift"
	"github.com/moveeeax/tfstate-drift/internal/notify"
	"github.com/moveeeax/tfstate-drift/internal/render"
	"github.com/moveeeax/tfstate-drift/internal/runner"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	var (
		chdir         string
		format        string
		planJSON      string
		noColor       bool
		showSensitive bool
		ignore        []string
		notifySpecs   []string
		terraformBin  string
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Detect and report Terraform state drift",
		Long: "Run a refresh-only plan (or read a pre-generated plan JSON) and report\n" +
			"drifted resources grouped by module, with attribute-level diffs.\n" +
			"Exits with code 2 when drift is detected.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "tree" && format != "json" {
				return fmt.Errorf("invalid --format %q: want \"tree\" or \"json\"", format)
			}

			rules, err := loadIgnoreRules(chdir, ignore)
			if err != nil {
				return err
			}
			sinks, err := parseSinks(notifySpecs)
			if err != nil {
				return err
			}

			data, err := loadPlan(cmd, planJSON, runner.NewTerraform(chdir, terraformBin))
			if err != nil {
				return err
			}

			plan, err := drift.Parse(data)
			if err != nil {
				return err
			}
			report := drift.Detect(plan, drift.Options{
				ShowSensitive: showSensitive,
				Ignore:        rules,
			})

			switch format {
			case "json":
				out, err := render.JSON(report)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), string(out))
			default:
				color := !noColor && os.Getenv("NO_COLOR") == ""
				fmt.Fprint(cmd.OutOrStdout(), render.Tree(report, color))
			}

			if err := notify.Send(cmd.Context(), sinks, report); err != nil {
				return err
			}

			if report.DriftDetected {
				return &DriftError{Count: report.ResourceCount}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&chdir, "chdir", ".", "Terraform working directory")
	cmd.Flags().StringVar(&format, "format", "tree", "output format: tree|json")
	cmd.Flags().StringVar(&planJSON, "plan-json", "", "read a pre-generated `terraform show -json` file instead of running terraform")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors in tree output")
	cmd.Flags().BoolVar(&showSensitive, "show-sensitive", false, "print values Terraform marks sensitive instead of masking them")
	cmd.Flags().StringArrayVar(&ignore, "ignore", nil, "ignore drift matching `<address glob>[:<attr path glob>]` (repeatable; adds to "+drift.IgnoreFile+" in --chdir)")
	cmd.Flags().StringArrayVar(&notifySpecs, "notify", nil, "send the report to `slack=<webhook url>` or slack=env:NAME when drift is found (repeatable)")
	cmd.Flags().StringVar(&terraformBin, "terraform-bin", "terraform", "terraform executable to run, e.g. tofu")
	return cmd
}

// parseSinks builds the notification sinks from the --notify flags.
func parseSinks(specs []string) ([]notify.Sink, error) {
	sinks := make([]notify.Sink, 0, len(specs))
	for _, spec := range specs {
		s, err := notify.ParseSpec(spec)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, s)
	}
	return sinks, nil
}

// loadIgnoreRules merges the ignore file in the working directory with the
// --ignore flags.
func loadIgnoreRules(chdir string, flags []string) ([]drift.IgnoreRule, error) {
	rules, err := drift.LoadIgnoreFile(chdir)
	if err != nil {
		return nil, err
	}
	for _, f := range flags {
		rule, err := drift.ParseIgnoreRule(f)
		if err != nil {
			return nil, fmt.Errorf("--ignore: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// loadPlan reads plan JSON from a file when --plan-json is set, otherwise runs
// a refresh-only plan through tf.
func loadPlan(cmd *cobra.Command, planJSON string, tf runner.PlanProvider) ([]byte, error) {
	if planJSON != "" {
		return os.ReadFile(planJSON)
	}
	return tf.Plan(cmd.Context())
}
