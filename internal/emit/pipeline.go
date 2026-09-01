package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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
// defaultPipeline. Both Composition and Functions go through this one
// resolver so the pipeline they describe can never disagree.
func effectivePipeline(b *blueprint.Blueprint) []blueprint.PipelineStep {
	if len(b.Spec.Pipeline) > 0 {
		return b.Spec.Pipeline
	}
	return defaultPipeline
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
