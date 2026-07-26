package drift

import (
	"fmt"
	"reflect"
	"sort"
)

// diffAttributes flattens the before/after attribute maps to dotted paths and
// returns every path whose value changed. Both maps come straight from the
// Terraform plan JSON, so values are arbitrary nested JSON (maps, slices,
// scalars). The result is sorted by path for stable output.
func diffAttributes(before, after map[string]any) []AttrChange {
	flatBefore := map[string]any{}
	flatAfter := map[string]any{}
	flatten("", before, flatBefore)
	flatten("", after, flatAfter)

	seen := map[string]struct{}{}
	for k := range flatBefore {
		seen[k] = struct{}{}
	}
	for k := range flatAfter {
		seen[k] = struct{}{}
	}

	var changes []AttrChange
	for path := range seen {
		b, okB := flatBefore[path]
		a, okA := flatAfter[path]
		if okB && okA && reflect.DeepEqual(b, a) {
			continue
		}
		changes = append(changes, AttrChange{Path: path, Before: b, After: a})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

// flatten walks an arbitrary JSON value and records leaf values keyed by a
// dotted/indexed path. Maps expand to "prefix.key", slices to "prefix[i]".
func flatten(prefix string, v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			// A whole-resource create/delete has an empty/nil top-level map;
			// there is nothing meaningful to record there. Nested empty maps do
			// carry signal, so keep their sentinel.
			if prefix != "" {
				out[prefix] = map[string]any{}
			}
			return
		}
		for k, val := range t {
			flatten(join(prefix, k), val, out)
		}
	case []any:
		if len(t) == 0 {
			if prefix != "" {
				out[prefix] = []any{}
			}
			return
		}
		for i, val := range t {
			flatten(fmt.Sprintf("%s[%d]", prefix, i), val, out)
		}
	default:
		if v == nil && prefix == "" {
			return
		}
		out[prefix] = v
	}
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
