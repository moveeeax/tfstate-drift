package drift

import "testing"

func TestDiffAttributesScalar(t *testing.T) {
	before := map[string]any{"instance_type": "t3.micro", "count": 1.0}
	after := map[string]any{"instance_type": "t3.small", "count": 1.0}
	changes := diffAttributes(before, after)
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1 (%+v)", len(changes), changes)
	}
	if changes[0].Path != "instance_type" {
		t.Fatalf("path = %q", changes[0].Path)
	}
}

func TestDiffAttributesNestedAndSorted(t *testing.T) {
	before := map[string]any{
		"tags":    map[string]any{"Env": "dev", "Team": "core"},
		"ingress": []any{map[string]any{"from_port": 22.0}},
	}
	after := map[string]any{
		"tags":    map[string]any{"Env": "prod", "Team": "core"},
		"ingress": []any{map[string]any{"from_port": 2222.0}},
	}
	changes := diffAttributes(before, after)
	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2 (%+v)", len(changes), changes)
	}
	// sorted by path: ingress[0].from_port before tags.Env
	if changes[0].Path != "ingress[0].from_port" {
		t.Fatalf("first path = %q", changes[0].Path)
	}
	if changes[1].Path != "tags.Env" {
		t.Fatalf("second path = %q", changes[1].Path)
	}
}

func TestDiffAttributesAddedRemoved(t *testing.T) {
	before := map[string]any{"a": "1"}
	after := map[string]any{"b": "2"}
	changes := diffAttributes(before, after)
	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(changes))
	}
	byPath := map[string]AttrChange{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	if byPath["a"].After != nil {
		t.Errorf("removed attr a should have nil After")
	}
	if byPath["b"].Before != nil {
		t.Errorf("added attr b should have nil Before")
	}
}

func TestDiffAttributesEqual(t *testing.T) {
	m := map[string]any{"x": "same", "nested": map[string]any{"y": 1.0}}
	if changes := diffAttributes(m, m); len(changes) != 0 {
		t.Fatalf("identical maps should not diff, got %+v", changes)
	}
}
