package manifest

// Span represents a byte slice range [Start, End) inside a text body.
type Span struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Raw   string `json:"raw,omitempty"`
}

// FieldForm represents the authoring classification of a resource field.
type FieldForm string

const (
	FormLiteral       FieldForm = "literal"
	FormParameterWire FieldForm = "parameter_wire"
	FormGuardedWire   FieldForm = "guarded_wire"
	FormStatusWire    FieldForm = "status_wire"
	FormRaw           FieldForm = "raw"
	FormOpaque        FieldForm = "opaque"
)

// ParsedField models a single recognized field in a composed resource template.
type ParsedField struct {
	Key    string    `json:"key"`
	Indent string    `json:"indent"`
	Form   FieldForm `json:"form"`
	Value  string    `json:"value"`
	Guard  string    `json:"guard,omitempty"`
	Span   Span      `json:"span"`
}

// ParsedResource models a composed resource document inside the template.
type ParsedResource struct {
	Name        string                  `json:"name"`
	APIVersion  string                  `json:"apiVersion"`
	Kind        string                  `json:"kind"`
	Span        Span                    `json:"span"`
	Fields      map[string]*ParsedField `json:"fields"`
	OpaqueSpans []Span                  `json:"opaqueSpans,omitempty"`
}

// CompositionManifest models the parsed Crossplane Composition document.
type CompositionManifest struct {
	Raw          []byte            `json:"-"`
	Name         string            `json:"name"`
	Group        string            `json:"group"`
	XRDKind      string            `json:"xrdKind"`
	TemplateSpan Span              `json:"templateSpan"`
	PreludeSpan  Span              `json:"preludeSpan"`
	Defines      []Span            `json:"defines,omitempty"`
	Resources    []*ParsedResource `json:"resources"`
	OpaqueSpans  []Span            `json:"opaqueSpans,omitempty"`
}
