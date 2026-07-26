package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/moveeeax/tfstate-drift/internal/drift"
)

// palette holds the styles used by the tree renderer. When color is disabled
// every style is the identity style, so the output is plain, deterministic text
// (which is what the golden tests assert against).
type palette struct {
	title  lipgloss.Style
	module lipgloss.Style
	res    lipgloss.Style
	action lipgloss.Style
	before lipgloss.Style
	after  lipgloss.Style
	ok     lipgloss.Style
}

func newPalette(color bool) palette {
	if !color {
		id := lipgloss.NewStyle()
		return palette{id, id, id, id, id, id, id}
	}
	return palette{
		title:  lipgloss.NewStyle().Bold(true),
		module: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		res:    lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		action: lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
		before: lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		after:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		ok:     lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
	}
}

// Tree renders the report as a colorized module/resource/attribute tree.
func Tree(report *drift.Report, color bool) string {
	p := newPalette(color)
	var b strings.Builder

	if !report.DriftDetected {
		fmt.Fprintln(&b, p.ok.Render("No drift detected. State matches configuration."))
		return b.String()
	}

	fmt.Fprintln(&b, p.title.Render(fmt.Sprintf("tfstate-drift: %d resource(s) drifted", report.ResourceCount)))

	for _, m := range report.Modules {
		fmt.Fprintf(&b, "\n%s\n", p.module.Render(m.Module))
		for i, r := range m.Resources {
			last := i == len(m.Resources)-1
			branch, cont := "├─", "│  "
			if last {
				branch, cont = "└─", "   "
			}
			fmt.Fprintf(&b, "%s %s %s\n",
				branch,
				p.res.Render(r.Address),
				p.action.Render("("+r.Action+")"))
			for _, a := range r.Attributes {
				fmt.Fprintf(&b, "%s ~ %s: %s → %s\n",
					cont,
					a.Path,
					p.before.Render(formatValue(a.Before)),
					p.after.Render(formatValue(a.After)))
			}
		}
	}
	return b.String()
}

// formatValue renders a JSON value compactly for the tree: strings stay quoted,
// scalars render bare, and nested structures collapse to compact JSON.
func formatValue(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(out)
}
