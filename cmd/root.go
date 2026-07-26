// Package cmd wires the tfstate-drift CLI together with cobra.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags "-X .../cmd.version=...".
var version = "dev"

// DriftError signals that drift was detected. main translates it into a
// non-zero exit code without printing an extra error line.
type DriftError struct {
	Count int
}

func (e *DriftError) Error() string {
	return fmt.Sprintf("drift detected: %d resource(s)", e.Count)
}

// NewRootCmd builds the root command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tfstate-drift",
		Short:         "Visualize Terraform state drift as a readable tree or JSON diff",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	return root
}
