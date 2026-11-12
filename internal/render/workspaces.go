package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/inventory"
)

// WorkspacesJSON renders the inventory as indented, schema-stable JSON with a
// trailing newline.
func WorkspacesJSON(inv *inventory.Inventory) ([]byte, error) {
	out, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// csvHeader is the CSV column contract; columns are only ever appended.
var csvHeader = []string{
	"dir", "backend", "workspace", "last_write", "age_days", "stale", "serial",
	"lineage", "terraform_version", "resource_count", "resources_by_type",
	"providers", "status", "drift_count", "error",
}

// WorkspacesCSV renders one row per workspace. resources_by_type holds
// "type=count" pairs and providers holds sources, both separated by ";".
// Cells that a spreadsheet would run as a formula are prefixed with "'".
func WorkspacesCSV(inv *inventory.Inventory) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, ws := range inv.Workspaces {
		lastWrite, age := "", ""
		if ws.LastWrite != nil {
			lastWrite = ws.LastWrite.Format(time.RFC3339)
			age = strconv.Itoa(*ws.AgeDays)
		}
		errText := ws.Error
		if errText == "" {
			errText = ws.LastWriteError
		}
		row := []string{
			ws.Dir, ws.Backend, ws.Name, lastWrite, age, strconv.FormatBool(ws.Stale),
			strconv.FormatUint(ws.Serial, 10), ws.Lineage, ws.TerraformVersion,
			strconv.Itoa(ws.ResourceCount), typeCounts(ws.ResourcesByType, "=", ";"),
			strings.Join(ws.Providers, ";"), ws.Status, strconv.Itoa(ws.DriftCount), errText,
		}
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// csvSafe neutralizes cells starting with a character spreadsheets treat as
// the start of a formula (OWASP "CSV Injection").
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// WorkspacesTable renders the inventory for a terminal: a summary line, one
// row per workspace and the resource breakdown by type.
func WorkspacesTable(inv *inventory.Inventory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tfstate-drift: %d workspace(s), %d stale, %d drifted, %d unreachable\n\n",
		inv.WorkspaceCount, inv.StaleCount, inv.DriftedCount, inv.UnreachableCount)
	if inv.WorkspaceCount == 0 {
		return b.String()
	}

	multiDir := false
	for _, ws := range inv.Workspaces {
		if ws.Dir != inv.Workspaces[0].Dir {
			multiDir = true
			break
		}
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKSPACE\tBACKEND\tLAST WRITE\tAGE\tSERIAL\tRESOURCES\tPROVIDERS\tSTATUS")
	for _, ws := range inv.Workspaces {
		lastWrite, age := "-", "-"
		if ws.LastWrite != nil {
			lastWrite = ws.LastWrite.Format("2006-01-02")
			age = fmt.Sprintf("%dd", *ws.AgeDays)
			if ws.Stale {
				age += " stale"
			}
		}
		status := ws.Status
		if ws.Status == inventory.StatusDrifted {
			status = fmt.Sprintf("drifted (%d)", ws.DriftCount)
		}
		providers := strings.Join(ws.Providers, ",")
		if providers == "" {
			providers = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			workspaceLabel(ws, multiDir), ws.Backend, lastWrite, age, ws.Serial, ws.ResourceCount, providers, status)
	}
	tw.Flush()

	for _, ws := range inv.Workspaces {
		if ws.Error == "" && ws.LastWriteError == "" && ws.ResourceCount == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", workspaceLabel(ws, multiDir))
		if ws.Error != "" {
			fmt.Fprintf(&b, "  error: %s\n", ws.Error)
		}
		if ws.LastWriteError != "" {
			fmt.Fprintf(&b, "  last write unknown: %s\n", ws.LastWriteError)
		}
		for _, tc := range sortedTypes(ws.ResourcesByType) {
			fmt.Fprintf(&b, "  %5d  %s\n", tc.n, tc.typ)
		}
	}
	return b.String()
}

// workspaceLabel prefixes the directory once more than one is inventoried.
func workspaceLabel(ws inventory.Workspace, withDir bool) string {
	if withDir {
		return ws.Dir + ":" + ws.Name
	}
	return ws.Name
}

type typeCount struct {
	typ string
	n   int
}

// sortedTypes orders resource types by count, largest first, then by name.
func sortedTypes(m map[string]int) []typeCount {
	out := make([]typeCount, 0, len(m))
	for t, n := range m {
		out = append(out, typeCount{t, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].typ < out[j].typ
	})
	return out
}

func typeCounts(m map[string]int, kv, sep string) string {
	parts := make([]string, 0, len(m))
	for _, tc := range sortedTypes(m) {
		parts = append(parts, tc.typ+kv+strconv.Itoa(tc.n))
	}
	return strings.Join(parts, sep)
}
