package inventory

import (
	"maps"
	"os"
	"slices"
	"testing"
)

func loadState(t *testing.T) *State {
	t.Helper()
	data, err := os.ReadFile("testdata/state.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseState(data)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestParseState(t *testing.T) {
	st := loadState(t)
	if st.Serial != 42 || st.Lineage != "3f1c2a9e-0000-4000-8000-000000000001" || st.TerraformVersion != "1.9.8" {
		t.Fatalf("unexpected header: %+v", st)
	}
}

func TestParseStateEmpty(t *testing.T) {
	for _, in := range []string{"", "\n", "  "} {
		st, err := ParseState([]byte(in))
		if err != nil || st.Serial != 0 || len(st.Resources) != 0 {
			t.Errorf("ParseState(%q) = %+v, %v", in, st, err)
		}
	}
	if _, err := ParseState([]byte("{not json")); err == nil {
		t.Error("expected an error on invalid json")
	}
}

func TestManagedByType(t *testing.T) {
	got := loadState(t).ManagedByType()
	want := map[string]int{"aws_instance": 2, "aws_eip": 1, "random_id": 1}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestProviders(t *testing.T) {
	got := loadState(t).Providers()
	want := []string{"example.com/acme/acme", "hashicorp/aws", "hashicorp/random"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestProviderSource(t *testing.T) {
	for in, want := range map[string]string{
		`provider["registry.terraform.io/hashicorp/aws"]`:         "hashicorp/aws",
		`provider["registry.terraform.io/hashicorp/aws"].west`:    "hashicorp/aws",
		`module.a.module.b.provider["registry.opentofu.org/x/y"]`: "x/y",
		`provider["terraform.io/builtin/terraform"]`:              "terraform.io/builtin/terraform",
		`provider.aws`:           "",
		`provider["unterminated`: "",
	} {
		if got := providerSource(in); got != want {
			t.Errorf("providerSource(%q) = %q, want %q", in, got, want)
		}
	}
}
