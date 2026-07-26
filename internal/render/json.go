// Package render turns a drift.Report into the two output formats the CLI
// supports: a human-readable colorized tree and a schema-stable JSON document.
package render

import (
	"encoding/json"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

// JSON renders the report as indented, schema-stable JSON with a trailing
// newline. This is the format CI pipelines should parse.
func JSON(report *drift.Report) ([]byte, error) {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
