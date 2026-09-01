package blueprint

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// TemplatingStepName and TemplatingFunctionName are the fixed identity of the
// built-in go-templating pipeline step the Composition emitter always writes
// (see internal/emit/composition.go). They live here, not in internal/emit,
// because Validate needs them for collision checks and emit already imports
// this package -- the reverse import would be a cycle.
const (
	TemplatingStepName     = "render-resources"
	TemplatingFunctionName = "function-go-templating"
)

// PositionBefore and PositionAfter are the two legal values of a pipeline
// step's position field: which side of the templating step it lands on. An
// empty position means PositionAfter -- the emitter, not the loader, applies
// that default, so the stored document round-trips exactly as written.
const (
	PositionBefore = "before"
	PositionAfter  = "after"
)

// ParsePipelineInput parses a step's raw input string into the mapping the
// emitter re-renders deterministically. It is the ONE definition of what a
// legal input is: Validate calls it to reject a bad one at the source, and
// internal/emit calls it again at render time so the two layers can never
// disagree about how the string is interpreted.
//
// A function input is a typed Kubernetes-style object, so it must be a YAML
// mapping carrying non-empty apiVersion and kind scalars. An empty or
// whitespace-only string parses to a nil map and is rejected by the
// apiVersion check rather than special-cased: "input present but empty" is
// not a meaningfully different mistake from "input present but untyped".
func ParsePipelineInput(raw string) (map[string]any, error) {
	var v map[string]any
	if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("must be a YAML mapping (a function input is a typed object): %w", err)
	}
	for _, key := range []string{"apiVersion", "kind"} {
		s, _ := v[key].(string)
		if s == "" {
			return nil, fmt.Errorf("must carry a non-empty %s -- a function input is a typed object, "+
				"and the function rejects (or worse, silently ignores) an untyped one", key)
		}
	}
	return v, nil
}

// validatePipeline checks spec.pipeline, reporting the first problem in
// declaration order. Called from Validate; nil Pipeline is legal (the emitter
// then writes its default auto-ready step -- see Spec.Pipeline's doc).
func (b *Blueprint) validatePipeline() error {
	seenName := map[string]int{}
	seenPkg := map[string]struct {
		pkg string
		at  int
	}{}
	for i, s := range b.Spec.Pipeline {
		at := fmt.Sprintf("spec.pipeline[%d]", i)

		// name reaches the Composition as an unquoted `- step: <name>` line,
		// so it gets the same discipline a resource name does: checkScalar,
		// DNS-label shape, and no YAML-keyword shapes.
		if s.Name == "" {
			return fmt.Errorf("%s.name is required", at)
		}
		if err := checkScalar(at+".name", s.Name); err != nil {
			return err
		}
		if !resourceNameRE.MatchString(s.Name) || yamlKeywords[strings.ToLower(s.Name)] {
			return fmt.Errorf("%s.name: %q is not a valid step name "+
				"(must be a DNS label, e.g. auto-ready, and not a YAML keyword like yes/no/on/off)", at, s.Name)
		}
		if s.Name == TemplatingStepName {
			return fmt.Errorf("%s.name: %q collides with the built-in templating step's name; "+
				"pick another -- Crossplane requires pipeline step names to be unique", at, s.Name)
		}
		if prev, dup := seenName[s.Name]; dup {
			return fmt.Errorf("%s.name: duplicate step name %q (already declared at spec.pipeline[%d]); "+
				"Crossplane requires pipeline step names to be unique", at, s.Name, prev)
		}
		seenName[s.Name] = i

		// functionRef becomes both the step's functionRef.name and a
		// Function's metadata.name in functions.yaml -- a Kubernetes object
		// name, so the same DNS-label shape applies.
		if s.FunctionRef == "" {
			return fmt.Errorf("%s.functionRef is required", at)
		}
		if err := checkScalar(at+".functionRef", s.FunctionRef); err != nil {
			return err
		}
		if !resourceNameRE.MatchString(s.FunctionRef) || yamlKeywords[strings.ToLower(s.FunctionRef)] {
			return fmt.Errorf("%s.functionRef: %q is not a valid function name "+
				"(must be a DNS label, e.g. function-auto-ready)", at, s.FunctionRef)
		}
		if s.FunctionRef == TemplatingFunctionName {
			return fmt.Errorf("%s.functionRef: %q is the built-in templating function, which the "+
				"generator declares and pins itself; a second go-templating step is not supported yet",
				at, s.FunctionRef)
		}

		// package reaches functions.yaml verbatim as `spec.package`, exactly
		// the way spec.sources[*].provider reaches the cache -- same checks.
		if s.Package == "" {
			return fmt.Errorf("%s.package is required", at)
		}
		if err := checkScalar(at+".package", s.Package); err != nil {
			return err
		}
		if !providerRefRE.MatchString(s.Package) {
			return fmt.Errorf("%s.package: %q is not a valid package reference "+
				"(e.g. xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1, "+
				"or ...@sha256:<digest>)", at, s.Package)
		}
		// One Function per distinct functionRef in functions.yaml means one
		// package per functionRef here. Two steps may share a function (same
		// function, different inputs), but they must agree on which package
		// provides it -- otherwise functions.yaml would have to pick one
		// silently, which is this project's central defect class.
		if prev, ok := seenPkg[s.FunctionRef]; ok && prev.pkg != s.Package {
			return fmt.Errorf("%s.package: functionRef %q already declared at spec.pipeline[%d] with a "+
				"different package (%q vs %q); one Function is emitted per functionRef, so every step "+
				"naming it must agree on the package", at, s.FunctionRef, prev.at, prev.pkg, s.Package)
		}
		seenPkg[s.FunctionRef] = struct {
			pkg string
			at  int
		}{s.Package, i}

		switch s.Position {
		case "", PositionBefore, PositionAfter:
		default:
			return fmt.Errorf("%s.position: %q is not valid (must be %q or %q, relative to the "+
				"templating step; default %q)", at, s.Position, PositionBefore, PositionAfter, PositionAfter)
		}

		if s.Input != "" {
			v, err := ParsePipelineInput(s.Input)
			if err != nil {
				return fmt.Errorf("%s.input: %w", at, err)
			}
			// The emitter re-encodes the parsed input through a real YAML
			// encoder, so a control character could not break document
			// structure the way it can in a pasted scalar -- but every other
			// user-controlled scalar in this file refuses control characters,
			// and an input whose values need them has no legitimate shape in a
			// function input object. Keeping the rule uniform costs multi-line
			// string values inside inputs; that is deliberate (see checkScalar)
			// and can be revisited if a real function input ever needs one.
			if err := checkInputScalars(at+".input", v); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkInputScalars applies checkScalar to every string in a parsed input
// tree -- map keys and scalar values alike -- walking maps in sorted key
// order so the first error reported is deterministic.
func checkInputScalars(at string, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := checkScalar(at+" key "+k, k); err != nil {
				return err
			}
			if err := checkInputScalars(at+"."+k, t[k]); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range t {
			if err := checkInputScalars(fmt.Sprintf("%s[%d]", at, i), e); err != nil {
				return err
			}
		}
	case string:
		return checkScalar(at, t)
	}
	return nil
}
