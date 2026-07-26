package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/moveeeax/tfstate-drift/cmd"
)

func main() {
	err := cmd.NewRootCmd().Execute()
	if err == nil {
		return
	}

	// Drift is an expected outcome, not an operational error: exit 2 quietly so
	// CI can gate on it without a noisy stack of error text.
	var de *cmd.DriftError
	if errors.As(err, &de) {
		os.Exit(2)
	}

	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
