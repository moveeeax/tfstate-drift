package drift

// Report is the schema-stable result of drift detection. Its JSON encoding is
// what CI consumers should depend on; fields are only ever added, never removed
// or renamed.
type Report struct {
	DriftDetected bool          `json:"drift_detected"`
	ResourceCount int           `json:"resource_count"`
	Modules       []ModuleDrift `json:"modules"`
}

// ModuleDrift groups drifted resources under the module they belong to. The
// root module is reported as "root".
type ModuleDrift struct {
	Module    string          `json:"module"`
	Resources []ResourceDrift `json:"resources"`
}

// ResourceDrift is one drifted resource with its attribute-level changes.
type ResourceDrift struct {
	Address    string       `json:"address"`
	Type       string       `json:"type"`
	Name       string       `json:"name"`
	Action     string       `json:"action"`
	Attributes []AttrChange `json:"attributes"`
}

// AttrChange is a single attribute whose value drifted, addressed by a dotted
// path (e.g. "tags.Env" or "ingress[0].from_port").
type AttrChange struct {
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
