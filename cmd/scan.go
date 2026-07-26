package cmd

import (
	"fmt"
	"os"

	"github.com/moveeeax/tfstate-drift/internal/drift"
	"github.com/moveeeax/tfstate-drift/internal/render"
	"github.com/moveeeax/tfstate-drift/internal/runner"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	var (
		chdir    string
		format   string
		planJSON string
		noColor  bool
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

			data, err := loadPlan(cmd, planJSON, chdir)
			if err != nil {
				return err
			}

			plan, err := drift.Parse(data)
			if err != nil {
				return err
			}
			report := drift.Detect(plan)

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
	return cmd
}

// loadPlan reads plan JSON from a file when --plan-json is set, otherwise runs
// a refresh-only Terraform plan in the working directory.
func loadPlan(cmd *cobra.Command, planJSON, chdir string) ([]byte, error) {
	if planJSON != "" {
		return os.ReadFile(planJSON)
	}
	return runner.NewTerraform(chdir).Plan(cmd.Context())
}
