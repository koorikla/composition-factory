package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// defaultPipeline is the effective spec.pipeline for a blueprint that
// declares none: the function-auto-ready step every real composition wants,
// pinned to the version verified against Crossplane v2.4.0. It is expressed
// as an ordinary PipelineStep so the default case and the declared case run
// through exactly one emission path -- byte-identical output to what this
// package emitted before spec.pipeline existed.
//
// A blueprint that declares ANY pipeline step replaces this default in full
// (see blueprint.Spec.Pipeline): the common case then declares auto-ready
// explicitly, typically from xpkg.crossplane.io/crossplane-contrib.
var defaultPipeline = []blueprint.PipelineStep{{
	Name:        "auto-ready",
	FunctionRef: "function-auto-ready",
	Package:     "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
}}

// effectivePipeline resolves a blueprint's declared steps, falling back to
// defaultPipeline. When spec.environment is non-empty, the function-environment-configs
// step is auto-injected ahead of the templating step (if not already present).
// Both Composition and Functions go through this one resolver so the pipeline they describe can never disagree.
func effectivePipeline(b *blueprint.Blueprint) []blueprint.PipelineStep {
	steps := b.Spec.Pipeline
	if len(steps) == 0 {
		steps = defaultPipeline
	}
	if len(b.Spec.Environment) > 0 {
		hasEnvStep := false
		for _, s := range steps {
			if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName {
				hasEnvStep = true
				break
			}
		}
		if !hasEnvStep {
			envStep := blueprint.PipelineStep{
				Name:        "environment-configs",
				FunctionRef: blueprint.EnvironmentConfigsFunctionName,
				Package:     blueprint.EnvironmentConfigsFunctionPackage,
				Position:    blueprint.PositionBefore,
			}
			steps = append([]blueprint.PipelineStep{envStep}, steps...)
		}
	}
	return steps
}

// splitPipeline partitions steps around the templating step, preserving
// declaration order within each side. An empty position means after -- the
// loader deliberately does not default it (round-trip exactness), so the
// default is applied here, at the one place that consumes it.
func splitPipeline(steps []blueprint.PipelineStep) (before, after []blueprint.PipelineStep) {
	for _, s := range steps {
		if s.Position == blueprint.PositionBefore {
			before = append(before, s)
		} else {
			after = append(after, s)
		}
	}
	return before, after
}

// writePipelineStep emits one declared step at the same indentation the
// templating step uses. The step's input, when present, is parsed
// (blueprint.ParsePipelineInput -- the same definition Validate checked it
// against) and re-encoded through sigs.k8s.io/yaml, NOT pasted as the string
// the user wrote. That encoder round-trips through JSON, which sorts every
// mapping's keys and quotes exactly the scalars that need quoting, so the
// emitted block is deterministic and structurally identical to the declared
// input regardless of the user's own key order or spacing. Each encoded line
// is then written through Doc.Line, which supplies the uniform indent -- a
// whole YAML document indented uniformly under a mapping key keeps its
// meaning, block scalars included.
func writePipelineStep(d *Doc, s blueprint.PipelineStep) error {
	d.Line(1, "- step: %s", s.Name)
	d.Line(2, "functionRef:")
	d.Line(3, "name: %s", s.FunctionRef)
	if s.Input == "" {
		return nil
	}
	v, err := blueprint.ParsePipelineInput(s.Input)
	if err != nil {
		// Validate rejects this before Generate ever calls here; kept for
		// direct Composition callers.
		return fmt.Errorf("pipeline step %q: input %w", s.Name, err)
	}
	body, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("pipeline step %q: re-encode input: %w", s.Name, err)
	}
	d.Line(2, "input:")
	for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		d.Line(3, "%s", line)
	}
	return nil
}

// ValidatePipelineInputs validates every declared pipeline step's input against
// the resolved Input CRD from crds. If a step's function CRD is found, unknown
// field paths fail with nearest-match suggestions. If a step's function schema
// is not cached, the step is accepted with an explicit warning.
func ValidatePipelineInputs(b *blueprint.Blueprint, crds []schema.CRD) (warnings []string, err error) {
	if b == nil {
		return nil, nil
	}
	for _, step := range b.Spec.Pipeline {
		if step.Input == "" {
			if step.Package != "" && !isFunctionCached(step.Package, crds) {
				warnings = append(warnings, fmt.Sprintf("pipeline step %q: function package %q is not cached; run cf function add %s to cache its schema",
					step.Name, step.Package, step.Package))
			}
			continue
		}
		v, err := blueprint.ParsePipelineInput(step.Input)
		if err != nil {
			return nil, fmt.Errorf("pipeline step %q input: %w", step.Name, err)
		}
		apiVersion, _ := v["apiVersion"].(string)
		kind, _ := v["kind"].(string)
		_, ver, found := findCRDForRendered(crds, apiVersion, kind)
		if !found {
			warnings = append(warnings, fmt.Sprintf("pipeline step %q: function %s %s (package %q) is not cached; input schema validation skipped (run: cf function add %s)",
				step.Name, apiVersion, kind, step.Package, step.Package))
			continue
		}

		if err := validateInputMap(step.Name, kind, "", v, ver.Properties); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

func isFunctionCached(pkgRef string, crds []schema.CRD) bool {
	for _, c := range crds {
		if c.IsFunctionInput() || c.Function {
			return true
		}
	}
	return false
}

func validateInputMap(stepName, kind, prefix string, inMap map[string]any, schemaProps map[string]any) error {
	var candidates []string
	for k := range schemaProps {
		if k != "apiVersion" && k != "kind" && k != "status" && k != "metadata" {
			candidates = append(candidates, k)
		}
	}

	keys := make([]string, 0, len(inMap))
	for k := range inMap {
		keys = append(keys, k)
	}

	for _, k := range keys {
		if k == "apiVersion" || k == "kind" || k == "metadata" || k == "status" {
			continue
		}
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		propRaw, exists := schemaProps[k]
		if !exists {
			if s := closestPath(k, candidates); s != "" {
				sugPath := s
				if prefix != "" {
					sugPath = prefix + "." + s
				}
				return fmt.Errorf("pipeline step %q input: field %q is not in %s schema; did you mean %q?",
					stepName, path, kind, sugPath)
			}
			return fmt.Errorf("pipeline step %q input: field %q is not in %s schema",
				stepName, path, kind)
		}

		propSchema, _ := propRaw.(map[string]any)
		if propSchema == nil {
			continue
		}

		val := inMap[k]
		if err := validateInputValue(stepName, kind, path, val, propSchema); err != nil {
			return err
		}
	}
	return nil
}

func validateInputValue(stepName, kind, path string, val any, propSchema map[string]any) error {
	if val == nil {
		return nil
	}

	typeName, _ := propSchema["type"].(string)
	if typeName == "" {
		if propSchema["properties"] != nil {
			typeName = "object"
		} else if propSchema["items"] != nil {
			typeName = "array"
		} else if propSchema["additionalProperties"] != nil {
			typeName = "map"
		}
	}

	switch typeName {
	case "string":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected string, got %T",
				stepName, path, val)
		}
		if enumRaw, ok := propSchema["enum"].([]any); ok && len(enumRaw) > 0 {
			matched := false
			var allowed []string
			for _, e := range enumRaw {
				str := fmt.Sprint(e)
				allowed = append(allowed, fmt.Sprintf("%q", str))
				if str == s {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("pipeline step %q input: field %q: invalid value %q: supported values: %s",
					stepName, path, s, strings.Join(allowed, ", "))
			}
		}
	case "integer":
		switch val := val.(type) {
		case int, int64, int32, uint, uint32, uint64:
			// ok
		case float64:
			if val != float64(int64(val)) {
				return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected integer, got number %v",
					stepName, path, val)
			}
		default:
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected integer, got %T",
				stepName, path, val)
		}
	case "number":
		switch val.(type) {
		case float64, float32, int, int64, int32:
			// ok
		default:
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected number, got %T",
				stepName, path, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected boolean, got %T",
				stepName, path, val)
		}
	case "array":
		slice, ok := val.([]any)
		if !ok {
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected array, got %T",
				stepName, path, val)
		}
		itemsSchema, _ := propSchema["items"].(map[string]any)
		if itemsSchema != nil {
			for i, item := range slice {
				elemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := validateInputValue(stepName, kind, elemPath, item, itemsSchema); err != nil {
					return err
				}
			}
		}
	case "object", "map":
		objMap, ok := val.(map[string]any)
		if !ok {
			return fmt.Errorf("pipeline step %q input: field %q: invalid type: expected object, got %T",
				stepName, path, val)
		}
		if props, ok := propSchema["properties"].(map[string]any); ok && len(props) > 0 {
			if err := validateInputMap(stepName, kind, path, objMap, props); err != nil {
				return err
			}
		} else if addProps, ok := propSchema["additionalProperties"].(map[string]any); ok {
			for k, v := range objMap {
				elemPath := fmt.Sprintf("%s[%s]", path, k)
				if err := validateInputValue(stepName, kind, elemPath, v, addProps); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
