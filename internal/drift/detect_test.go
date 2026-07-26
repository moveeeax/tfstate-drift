package drift

import (
	"os"
	"testing"
)

func loadFixture(t *testing.T, path string) *Plan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	plan, err := Parse(data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return plan
}

func TestDetectGroupsByModule(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"))

	if !report.DriftDetected {
		t.Fatal("expected drift to be detected")
	}
	if report.ResourceCount != 3 {
		t.Fatalf("resource count = %d, want 3", report.ResourceCount)
	}
	if len(report.Modules) != 2 {
		t.Fatalf("modules = %d, want 2", len(report.Modules))
	}
	// root sorts before named modules.
	if report.Modules[0].Module != "root" {
		t.Fatalf("first module = %q, want root", report.Modules[0].Module)
	}
	if report.Modules[1].Module != "module.network" {
		t.Fatalf("second module = %q, want module.network", report.Modules[1].Module)
	}
	// module.network has two resources, sorted by address.
	net := report.Modules[1].Resources
	if len(net) != 2 {
		t.Fatalf("module.network resources = %d, want 2", len(net))
	}
	if net[0].Address != "module.network.aws_eip.nat" {
		t.Fatalf("first network resource = %q", net[0].Address)
	}
}

func TestDetectAttributeDiff(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"))
	web := report.Modules[0].Resources[0]
	if web.Address != "aws_instance.web" {
		t.Fatalf("root resource = %q", web.Address)
	}
	if web.Action != "update" {
		t.Fatalf("action = %q, want update", web.Action)
	}

	got := map[string][2]any{}
	for _, a := range web.Attributes {
		got[a.Path] = [2]any{a.Before, a.After}
	}
	// unchanged attributes (id, tags.Owner) must not appear.
	if _, ok := got["id"]; ok {
		t.Error("unchanged attribute id should not be reported")
	}
	if _, ok := got["tags.Owner"]; ok {
		t.Error("unchanged attribute tags.Owner should not be reported")
	}
	// changed attributes must appear.
	for _, path := range []string{"instance_type", "monitoring", "tags.Env"} {
		if _, ok := got[path]; !ok {
			t.Errorf("expected changed attribute %q to be reported", path)
		}
	}
}

func TestDetectDeleteAction(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"))
	eip := report.Modules[1].Resources[0]
	if eip.Action != "delete" {
		t.Fatalf("eip action = %q, want delete", eip.Action)
	}
	// after was null, so every before attribute drifted to nil.
	if len(eip.Attributes) == 0 {
		t.Fatal("expected delete to report removed attributes")
	}
}

func TestDetectNoDrift(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/no-drift-plan.json"))
	if report.DriftDetected {
		t.Fatal("expected no drift")
	}
	if report.ResourceCount != 0 || len(report.Modules) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

func TestDetectNilPlan(t *testing.T) {
	report := Detect(nil)
	if report.DriftDetected || report.Modules == nil {
		t.Fatalf("nil plan should yield empty non-nil report, got %+v", report)
	}
}

func TestSummarizeActions(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want string
	}{
		"update":  {[]string{"update"}, "update"},
		"delete":  {[]string{"delete"}, "delete"},
		"replace": {[]string{"delete", "create"}, "replace"},
		"empty":   {nil, ""},
	}
	for name, c := range cases {
		if got := summarizeActions(c.in); got != c.want {
			t.Errorf("%s: summarizeActions = %q, want %q", name, got, c.want)
		}
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("expected error for empty document")
	}
	if _, err := Parse([]byte("{ not json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}
