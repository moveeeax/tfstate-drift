package render

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

func fixtureReport(t *testing.T, path string) *drift.Report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	plan, err := drift.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return drift.Detect(plan)
}

func TestTreePlainOutput(t *testing.T) {
	report := fixtureReport(t, "../../examples/drift-plan.json")
	out := Tree(report, false)

	// No ANSI escapes when color is disabled.
	if strings.Contains(out, "\x1b[") {
		t.Fatal("plain tree output must not contain ANSI escapes")
	}
	for _, want := range []string{
		"tfstate-drift: 3 resource(s) drifted",
		"root",
		"aws_instance.web (update)",
		"~ instance_type: \"t3.micro\" → \"t3.small\"",
		"module.network",
		"aws_eip.nat (delete)",
		"aws_security_group.web (update)",
		"~ ingress[0].from_port: 22 → 2222",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q\n---\n%s", want, out)
		}
	}
}

func TestTreeNoDrift(t *testing.T) {
	report := fixtureReport(t, "../../examples/no-drift-plan.json")
	out := Tree(report, false)
	if !strings.Contains(out, "No drift detected") {
		t.Fatalf("expected no-drift message, got %q", out)
	}
}

func TestTreeColorAddsEscapes(t *testing.T) {
	report := fixtureReport(t, "../../examples/drift-plan.json")
	out := Tree(report, true)
	if !strings.Contains(out, "\x1b[") {
		t.Skip("terminal profile stripped colors in this environment")
	}
}

func TestJSONStableSchema(t *testing.T) {
	report := fixtureReport(t, "../../examples/drift-plan.json")
	out, err := JSON(report)
	if err != nil {
		t.Fatalf("json: %v", err)
	}

	var decoded drift.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}
	if !decoded.DriftDetected || decoded.ResourceCount != 3 {
		t.Fatalf("decoded report mismatch: %+v", decoded)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Error("json output should end with a newline")
	}
	// Field names are the CI contract: assert they are present.
	for _, key := range []string{"\"drift_detected\"", "\"resource_count\"", "\"modules\"", "\"attributes\"", "\"path\"", "\"before\"", "\"after\""} {
		if !strings.Contains(string(out), key) {
			t.Errorf("json output missing stable key %s", key)
		}
	}
}
