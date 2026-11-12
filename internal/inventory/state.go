// Package inventory lists the Terraform workspaces of one or more working
// directories with what their state says about them: last write time, serial,
// resource counts by type, providers and, optionally, refresh-only drift.
package inventory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// State is the subset of a Terraform state file (format version 4) the
// inventory reads. It is what `terraform state pull` prints.
type State struct {
	Version          int             `json:"version"`
	TerraformVersion string          `json:"terraform_version"`
	Serial           uint64          `json:"serial"`
	Lineage          string          `json:"lineage"`
	Resources        []StateResource `json:"resources"`
}

// StateResource is one resource block in the state. Every element of
// Instances is a real object (one per count or for_each key).
type StateResource struct {
	Mode      string            `json:"mode"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Provider  string            `json:"provider"`
	Instances []json.RawMessage `json:"instances"`
}

// ParseState decodes `terraform state pull` output. Empty output means the
// workspace has no state yet and yields a zero State.
func ParseState(data []byte) (*State, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return &State{}, nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode state json: %w", err)
	}
	return &s, nil
}

// ManagedByType counts managed resource instances per resource type. Data
// sources are left out: they cost nothing and are not destroyed.
func (s *State) ManagedByType() map[string]int {
	out := map[string]int{}
	for _, r := range s.Resources {
		if r.Mode == "managed" && len(r.Instances) > 0 {
			out[r.Type] += len(r.Instances)
		}
	}
	return out
}

// Providers returns the sorted, unique providers that resources in the state
// (data sources included) were created with, such as "hashicorp/aws".
func (s *State) Providers() []string {
	seen := map[string]bool{}
	for _, r := range s.Resources {
		if p := providerSource(r.Provider); p != "" {
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// providerSource turns a state provider reference such as
// `provider["registry.terraform.io/hashicorp/aws"].west` or
// `module.x.provider["registry.opentofu.org/hashicorp/aws"]` into
// "hashicorp/aws". Sources from other registries keep their hostname.
func providerSource(ref string) string {
	_, rest, ok := strings.Cut(ref, `provider["`)
	if !ok {
		return ""
	}
	src, _, ok := strings.Cut(rest, `"]`)
	if !ok {
		return ""
	}
	for _, host := range []string{"registry.terraform.io/", "registry.opentofu.org/"} {
		if s, ok := strings.CutPrefix(src, host); ok {
			return s
		}
	}
	return src
}
