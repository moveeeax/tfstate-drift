// Package drift parses Terraform refresh-only plan JSON and turns the
// resource_drift section into a grouped, attribute-level drift report.
package drift

import (
	"encoding/json"
	"fmt"
)

// Plan is the subset of the `terraform show -json` plan document that we care
// about. A refresh-only plan populates resource_drift with every resource whose
// real-world state no longer matches what Terraform recorded.
type Plan struct {
	FormatVersion    string         `json:"format_version"`
	TerraformVersion string         `json:"terraform_version"`
	ResourceDrift    []ResourcePlan `json:"resource_drift"`
}

// ResourcePlan is a single entry in the resource_drift array.
type ResourcePlan struct {
	Address       string `json:"address"`
	ModuleAddress string `json:"module_address"`
	Mode          string `json:"mode"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	ProviderName  string `json:"provider_name"`
	Change        Change `json:"change"`
}

// Change holds the before/after attribute maps and the planned actions.
type Change struct {
	Actions []string       `json:"actions"`
	Before  map[string]any `json:"before"`
	After   map[string]any `json:"after"`
}

// Parse decodes a `terraform show -json` plan document. It is deliberately
// strict about JSON validity but tolerant of extra fields, so newer Terraform
// versions keep working.
func Parse(data []byte) (*Plan, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty plan document")
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode plan json: %w", err)
	}
	return &p, nil
}
