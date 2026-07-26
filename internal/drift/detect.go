package drift

import (
	"sort"
	"strings"
)

// Detect turns a parsed refresh-only plan into a grouped drift report.
// Resources whose only action is "no-op" are skipped: Terraform occasionally
// lists a resource in resource_drift even when the net change is nothing.
func Detect(plan *Plan) *Report {
	report := &Report{Modules: []ModuleDrift{}}
	if plan == nil {
		return report
	}

	byModule := map[string][]ResourceDrift{}
	for _, rp := range plan.ResourceDrift {
		action := summarizeActions(rp.Change.Actions)
		if action == "no-op" || action == "" {
			continue
		}
		mod := rp.ModuleAddress
		if mod == "" {
			mod = "root"
		}
		byModule[mod] = append(byModule[mod], ResourceDrift{
			Address:    rp.Address,
			Type:       rp.Type,
			Name:       rp.Name,
			Action:     action,
			Attributes: diffAttributes(rp.Change.Before, rp.Change.After),
		})
		report.ResourceCount++
	}

	mods := make([]string, 0, len(byModule))
	for m := range byModule {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return moduleLess(mods[i], mods[j]) })

	for _, m := range mods {
		res := byModule[m]
		sort.Slice(res, func(i, j int) bool { return res[i].Address < res[j].Address })
		report.Modules = append(report.Modules, ModuleDrift{Module: m, Resources: res})
	}

	report.DriftDetected = report.ResourceCount > 0
	return report
}

// summarizeActions collapses the Terraform actions array into a single verb.
// ["update"] -> "update", ["delete"] -> "delete", ["create"] -> "create",
// ["delete","create"] -> "replace".
func summarizeActions(actions []string) string {
	switch {
	case len(actions) == 0:
		return ""
	case len(actions) == 1:
		return actions[0]
	default:
		joined := strings.Join(actions, ",")
		if joined == "delete,create" || joined == "create,delete" {
			return "replace"
		}
		return joined
	}
}

// moduleLess keeps "root" first, then sorts remaining modules alphabetically.
func moduleLess(a, b string) bool {
	if a == "root" {
		return b != "root"
	}
	if b == "root" {
		return false
	}
	return a < b
}
