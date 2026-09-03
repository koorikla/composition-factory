package blueprint

import (
	"fmt"
	"regexp"
	"strings"
)

// FromRef is a parsed Field.From value. Exactly one of the two forms is set:
//
//   - Param != "":  params.<name> — the value of an XRD parameter.
//   - Resource != "": resources.<name>.status.<path> — a scalar observed on
//     another composed resource's status, with StatusPath holding the
//     dot-split path below status (never empty when Resource is set).
//
// The grammar decisions the type encodes, since the code cannot show why:
//
//   - The path is anchored at `status`, not at the whole resource. A wire's
//     job is to carry an OBSERVED value (spec §6: "Reserve status.atProvider
//     for values with no triad — and never emit one unguarded"); admitting
//     spec paths would invite wiring one resource's desired state from
//     another's desired state, which dependsOn/params already cover without
//     the observation delay.
//   - `atProvider` is NOT part of the grammar. The path below status is
//     whatever the source CRD's own status schema declares, because nothing
//     guarantees an upjet envelope (provider-kubernetes-style resources
//     carry their own shapes) — the same never-hard-code-the-envelope rule
//     as spec §5.
//   - Each path segment must be a Go-template identifier, because the
//     emitted template dereferences it as `.segment` and text/template only
//     admits identifier-shaped keys after a dot. This also (deliberately)
//     excludes array-crossing paths: Leaves addresses array elements as
//     conditions[0].type, and [0] is not an identifier — a status wire
//     cannot cross an array in M-scope.
type FromRef struct {
	Param        string
	Env          string
	Resource     string
	StatusPath   []string
	MetadataPath string
}

// IsMetadataName reports whether this ref points to another resource's metadata.name.
func (r FromRef) IsMetadataName() bool {
	return r.Resource != "" && r.MetadataPath == "name"
}

// statusSegmentRE is the Go text/template field-access grammar: after a dot,
// only an identifier can follow, so only identifier-shaped status keys are
// addressable by the emitted template.
var statusSegmentRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ParseFrom parses a Field.From value into its reference form. It performs
// only grammar checks; whether the named parameter, env key, or resource exists is
// Validate's job (it has the document), and whether the status path exists
// in the source CRD's schema is the emit layer's (it has the CRDs).
func ParseFrom(s string) (FromRef, error) {
	if param, ok := strings.CutPrefix(s, "params."); ok {
		return FromRef{Param: param}, nil
	}
	if envKey, ok := EnvRef(s); ok {
		if envKey == "" {
			return FromRef{}, fmt.Errorf("from: %q is missing an environment key name", s)
		}
		if !paramNameRE.MatchString(envKey) {
			return FromRef{}, fmt.Errorf("from: %q is not a valid environment key name (must be camelCase, e.g. env.region)", s)
		}
		return FromRef{Env: envKey}, nil
	}
	if rest, ok := strings.CutPrefix(s, "resources."); ok {
		if name, ok := strings.CutSuffix(rest, ".metadata.name"); ok {
			if !resourceNameRE.MatchString(name) {
				return FromRef{}, fmt.Errorf("from: %q is not a valid resource name in a "+
					"resources.<name>.metadata.name reference (must be a DNS label, e.g. main-queue)", name)
			}
			return FromRef{Resource: name, MetadataPath: "name"}, nil
		}
		// A resource name is a DNS label and never contains a dot, so the
		// FIRST ".status." unambiguously splits name from path — even when
		// the status path itself begins with a key literally named "status".
		name, path, found := strings.Cut(rest, ".status.")
		if !found || path == "" {
			return FromRef{}, fmt.Errorf("from: %q must be resources.<name>.status.<path> "+
				"(e.g. resources.main-queue.status.atProvider.url) or resources.<name>.metadata.name", s)
		}
		if !resourceNameRE.MatchString(name) {
			return FromRef{}, fmt.Errorf("from: %q is not a valid resource name in a "+
				"resources.<name>.status.<path> reference (must be a DNS label, e.g. main-queue)", name)
		}
		segs := strings.Split(path, ".")
		for _, seg := range segs {
			if !statusSegmentRE.MatchString(seg) {
				return FromRef{}, fmt.Errorf("from: status path segment %q in %q is not a "+
					"template identifier ([a-zA-Z_][a-zA-Z0-9_]*); the emitted template "+
					"dereferences each segment as .segment, so dashed, empty or array-indexed "+
					"segments (conditions[0]) cannot be addressed", seg, s)
			}
		}
		return FromRef{Resource: name, StatusPath: segs}, nil
	}
	return FromRef{}, fmt.Errorf("from must start with params.<name>, "+
		"params.<name>.<member>, env.<key>, resources.<name>.status.<path> or resources.<name>.metadata.name (got %q)", s)
}
