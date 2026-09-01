// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

// NativeProvider is the provider label for native Kubernetes kinds — the
// value a resource's provider field carries to compose a Deployment or
// Service directly, and the provider label those kinds wear in the index
// and /api/kinds. It is a label, not a package reference: native kinds are
// vendored into cf itself (internal/schema/k8s, pinned per Kubernetes
// version) and are always available, so nothing ever fetches, caches or
// digest-pins a source named "k8s" — which is why Validate refuses it in
// spec.sources.
const NativeProvider = "k8s"

// Blueprint is the root document.
type Blueprint struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name string `json:"name"`
}

type Spec struct {
	Sources   []Source   `json:"sources"`
	XRD       XRD        `json:"xrd"`
	Resources []Resource `json:"resources"`
}

// Source is one schema source. M1 supports provider packages only.
type Source struct {
	Provider string `json:"provider"`
}

// XRD describes the composite API to generate.
type XRD struct {
	Group      string               `json:"group"`
	Kind       string               `json:"kind"`
	Plural     string               `json:"plural"`
	Version    string               `json:"version"`
	Scope      string               `json:"scope"`
	Parameters map[string]Parameter `json:"parameters"`
}

// Parameter is one spec field of the composite API. It is single-source: this
// declaration produces both the XRD schema and the template default.
type Parameter struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
}

// Resource is one composed resource.
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	Fields   map[string]Field `json:"fields"`
}

// Field sets one path on a composed resource. Exactly one of From, Value or Raw
// must be set.
type Field struct {
	From  string `json:"from"`
	Value string `json:"value"`
	Raw   string `json:"raw"`
}
