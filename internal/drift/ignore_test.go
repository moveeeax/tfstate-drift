package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnoreRule(t *testing.T) {
	cases := map[string]IgnoreRule{
		"aws_autoscaling_group.*:desired_capacity": {Address: "aws_autoscaling_group.*", Attr: "desired_capacity"},
		"module.network.*":                         {Address: "module.network.*"},
		` aws_s3_bucket.b["a:b"]:tags.x `:          {Address: `aws_s3_bucket.b["a:b"]`, Attr: "tags.x"},
		"*:tags.aws:cloudformation:stack-name":     {Address: "*", Attr: "tags.aws:cloudformation:stack-name"},
	}
	for in, want := range cases {
		got, err := ParseIgnoreRule(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", ":tags", "aws_instance.web:"} {
		if _, err := ParseIgnoreRule(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"aws_instance.web", "aws_instance.web", true},
		{"aws_instance.*", "aws_instance.web", true},
		{"aws_instance.*", "module.a.aws_instance.web", false},
		{"*aws_instance.*", "module.a.aws_instance.web", true},
		{"aws_instance.web[0]", "aws_instance.web[0]", true},
		{"aws_instance.web[0]", "aws_instance.web0", false},
		{`aws_instance.web["?"]`, `aws_instance.web["a"]`, true},
		{"tags.*", "tags.LastModified", true},
		{"*", "", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestReadIgnoreRules(t *testing.T) {
	in := "# noisy by design\n\naws_autoscaling_group.*:desired_capacity\n  *:tags.LastModified  \n"
	rules, err := ReadIgnoreRules(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[1] != (IgnoreRule{Address: "*", Attr: "tags.LastModified"}) {
		t.Fatalf("got %+v", rules)
	}
	if _, err := ReadIgnoreRules(strings.NewReader("ok\n:bad\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want a line-numbered error, got %v", err)
	}
}

func TestLoadIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	rules, err := LoadIgnoreFile(dir)
	if err != nil || rules != nil {
		t.Fatalf("missing file: got %v, %v", rules, err)
	}
	if err := os.WriteFile(filepath.Join(dir, IgnoreFile), []byte("aws_eip.*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, err = LoadIgnoreFile(dir)
	if err != nil || len(rules) != 1 || rules[0].Address != "aws_eip.*" {
		t.Fatalf("got %v, %v", rules, err)
	}
}

func TestDetectIgnoreAttributes(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"), Options{Ignore: []IgnoreRule{
		{Address: "aws_instance.web", Attr: "tags"},
		{Address: "aws_instance.web", Attr: "monitoring"},
	}})
	if report.ResourceCount != 3 || report.IgnoredCount != 0 {
		t.Fatalf("counts = %d/%d, want 3/0", report.ResourceCount, report.IgnoredCount)
	}
	web := report.Modules[0].Resources[0]
	if len(web.Attributes) != 1 || web.Attributes[0].Path != "instance_type" {
		t.Fatalf("want only instance_type left, got %+v", web.Attributes)
	}
}

func TestDetectIgnoreDropsEmptiedResource(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"), Options{Ignore: []IgnoreRule{
		{Address: "*aws_security_group.*", Attr: "ingress"},
		{Address: "module.network.aws_eip.nat"},
	}})
	if report.ResourceCount != 1 || report.IgnoredCount != 2 {
		t.Fatalf("counts = %d/%d, want 1/2", report.ResourceCount, report.IgnoredCount)
	}
	if len(report.Modules) != 1 || report.Modules[0].Module != "root" {
		t.Fatalf("emptied module should disappear, got %+v", report.Modules)
	}
}

func TestDetectIgnoreEverything(t *testing.T) {
	report := Detect(loadFixture(t, "../../examples/drift-plan.json"), Options{Ignore: []IgnoreRule{{Address: "*"}}})
	if report.DriftDetected || report.ResourceCount != 0 || report.IgnoredCount != 3 {
		t.Fatalf("got %+v", report)
	}
}

func TestAncestors(t *testing.T) {
	got := strings.Join(ancestors("a.b[0].c"), " ")
	if got != "a a.b a.b[0] a.b[0].c" {
		t.Fatalf("ancestors = %q", got)
	}
}
