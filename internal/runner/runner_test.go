package runner

import (
	"context"
	"testing"
)

// fakeProvider is the kind of offline stand-in tests and the scan command can
// use in place of real Terraform.
type fakeProvider struct{ data []byte }

func (f fakeProvider) Plan(context.Context) ([]byte, error) { return f.data, nil }

func TestPlanProviderInterface(t *testing.T) {
	// Compile-time guarantee that the real runner satisfies the interface.
	var _ PlanProvider = (*Terraform)(nil)

	// A fake provider is usable exactly where the real one is.
	var p PlanProvider = fakeProvider{data: []byte(`{"resource_drift":[]}`)}
	out, err := p.Plan(context.Background())
	if err != nil {
		t.Fatalf("fake provider: %v", err)
	}
	if string(out) != `{"resource_drift":[]}` {
		t.Fatalf("unexpected fake output: %s", out)
	}
}

func TestTerraformMissingBinary(t *testing.T) {
	tf := &Terraform{Dir: ".", Binary: "terraform-does-not-exist-xyz"}
	if _, err := tf.Plan(context.Background()); err == nil {
		t.Fatal("expected error when terraform binary is missing")
	}
}
