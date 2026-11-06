package drift

// SensitiveMask replaces the before/after value of an attribute that Terraform
// marked sensitive, so secrets never reach CI logs or artifacts.
const SensitiveMask = "(sensitive)"

// sensitivePaths walks a before_sensitive/after_sensitive document and records
// every flattened path marked true. The document mirrors the shape of the
// attribute value: true marks a whole subtree, false or a missing key means not
// sensitive. A bare true at the top level marks the entire resource as "".
func sensitivePaths(prefix string, v any, out map[string]struct{}) {
	switch t := v.(type) {
	case bool:
		if t {
			out[prefix] = struct{}{}
		}
	case map[string]any:
		for k, val := range t {
			sensitivePaths(join(prefix, k), val, out)
		}
	case []any:
		for i, val := range t {
			sensitivePaths(indexPath(prefix, i), val, out)
		}
	}
}

// isSensitive reports whether path, or any of its ancestors, is marked.
func isSensitive(path string, marks map[string]struct{}) bool {
	if len(marks) == 0 {
		return false
	}
	if _, ok := marks[""]; ok {
		return true
	}
	for _, p := range ancestors(path) {
		if _, ok := marks[p]; ok {
			return true
		}
	}
	return false
}

// maskSensitive flags every change whose path is marked sensitive on either
// side and, unless show is set, replaces its non-null values with
// SensitiveMask. Null stays null: it says the value appeared or disappeared,
// not what it was.
func maskSensitive(changes []AttrChange, c Change, show bool) {
	marks := map[string]struct{}{}
	sensitivePaths("", c.BeforeSensitive, marks)
	sensitivePaths("", c.AfterSensitive, marks)
	for i := range changes {
		if !isSensitive(changes[i].Path, marks) {
			continue
		}
		changes[i].Sensitive = true
		if show {
			continue
		}
		if changes[i].Before != nil {
			changes[i].Before = SensitiveMask
		}
		if changes[i].After != nil {
			changes[i].After = SensitiveMask
		}
	}
}
