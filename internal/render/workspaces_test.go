package render

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/inventory"
)

func fixtureInventory() *inventory.Inventory {
	old := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	age := 256
	return inventory.Summarize([]inventory.Workspace{
		{
			Dir: "infra", Backend: "s3", Name: "demo", LastWrite: &old, AgeDays: &age, Stale: true,
			Serial: 7, Lineage: "l-1", TerraformVersion: "1.9.8", ResourceCount: 3,
			ResourcesByType: map[string]int{"aws_instance": 2, "aws_eip": 1},
			Providers:       []string{"hashicorp/aws"}, Status: inventory.StatusDrifted, DriftCount: 2,
		},
		{
			Dir: "infra", Backend: "s3", Name: "=cmd", ResourcesByType: map[string]int{}, Providers: []string{},
			Status: inventory.StatusUnreachable, Error: "AccessDenied", LastWriteError: "forbidden",
		},
	})
}

func TestWorkspacesTable(t *testing.T) {
	out := WorkspacesTable(fixtureInventory())
	for _, want := range []string{
		"tfstate-drift: 2 workspace(s), 1 stale, 1 drifted, 1 unreachable",
		"WORKSPACE  BACKEND  LAST WRITE  AGE         SERIAL  RESOURCES  PROVIDERS      STATUS",
		"demo       s3       2026-03-01  256d stale  7       3          hashicorp/aws  drifted (2)",
		"=cmd       s3       -           -           0       0          -              unreachable",
		"demo\n      2  aws_instance\n      1  aws_eip\n",
		"=cmd\n  error: AccessDenied\n  last write unknown: forbidden\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "infra:") {
		t.Error("single directory should not prefix names")
	}
}

func TestWorkspacesTableMultiDir(t *testing.T) {
	inv := fixtureInventory()
	inv.Workspaces[1].Dir = "other"
	if out := WorkspacesTable(inv); !strings.Contains(out, "infra:demo") || !strings.Contains(out, "other:=cmd") {
		t.Errorf("multiple directories should prefix names:\n%s", out)
	}
}

func TestWorkspacesTableEmpty(t *testing.T) {
	out := WorkspacesTable(inventory.Summarize(nil))
	if out != "tfstate-drift: 0 workspace(s), 0 stale, 0 drifted, 0 unreachable\n\n" {
		t.Errorf("got %q", out)
	}
}

func TestWorkspacesCSV(t *testing.T) {
	out, err := WorkspacesCSV(fixtureInventory())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil {
		t.Fatalf("invalid csv: %v", err)
	}
	if len(rows) != 3 || strings.Join(rows[0], ",") != strings.Join(csvHeader, ",") {
		t.Fatalf("header/rows: %q", rows)
	}
	demo := strings.Join(rows[1], ",")
	if demo != "infra,s3,demo,2026-03-01T12:00:00Z,256,true,7,l-1,1.9.8,3,aws_instance=2;aws_eip=1,hashicorp/aws,drifted,2," {
		t.Errorf("demo row = %s", demo)
	}
	if rows[2][2] != "'=cmd" {
		t.Errorf("formula-looking cell not neutralized: %q", rows[2][2])
	}
	if rows[2][3] != "" || rows[2][4] != "" || rows[2][14] != "AccessDenied" {
		t.Errorf("unreachable row = %q", rows[2])
	}
}

func TestWorkspacesJSONStableSchema(t *testing.T) {
	out, err := WorkspacesJSON(fixtureInventory())
	if err != nil {
		t.Fatal(err)
	}
	var decoded inventory.Inventory
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if decoded.WorkspaceCount != 2 || decoded.Workspaces[0].Name != "demo" {
		t.Fatalf("decoded: %+v", decoded)
	}
	for _, key := range []string{
		`"workspace_count"`, `"stale_count"`, `"drifted_count"`, `"unreachable_count"`, `"workspaces"`,
		`"dir"`, `"backend"`, `"name"`, `"last_write": "2026-03-01T12:00:00Z"`, `"age_days": 256`, `"stale"`,
		`"serial"`, `"lineage"`, `"terraform_version"`, `"resource_count"`, `"resources_by_type"`,
		`"providers"`, `"status"`, `"drift_count"`, `"last_write": null`, `"age_days": null`,
	} {
		if !strings.Contains(string(out), key) {
			t.Errorf("json missing %s", key)
		}
	}
}
