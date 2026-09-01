package emit

import (
	"fmt"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// fn is one pipeline function and the package that provides it.
type fn struct{ name, pkg string }

// templatingFunction is the built-in go-templating function every generated
// Composition's templating step needs, pinned to the version verified against
// Crossplane v2.4.0. It is always declared, and always first; the rest of
// functions.yaml follows from the blueprint's (effective) pipeline.
var templatingFunction = fn{
	blueprint.TemplatingFunctionName,
	"xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0",
}

// Functions renders functions.yaml, the required third argument to
// `crossplane composition render`: the templating function plus one Function
// per DISTINCT functionRef in the blueprint's effective pipeline (two steps
// may share a function), each carrying its package exactly as the blueprint
// declared it -- tag, digest pin, or neither, verbatim.
func Functions(b *blueprint.Blueprint) ([]byte, error) {
	fns := []fn{templatingFunction}
	declared := map[string]string{templatingFunction.name: templatingFunction.pkg}
	for _, s := range effectivePipeline(b) {
		if pkg, ok := declared[s.FunctionRef]; ok {
			if pkg != s.Package {
				// Validate rejects both ways this can happen (a functionRef
				// declared twice with different packages, and a step naming
				// the built-in templating function); kept for direct callers.
				return nil, fmt.Errorf("pipeline step %q: functionRef %q already declared with package %q",
					s.Name, s.FunctionRef, pkg)
			}
			continue
		}
		declared[s.FunctionRef] = s.Package
		fns = append(fns, fn{s.FunctionRef, s.Package})
	}

	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Comment("Required by: crossplane composition render <xr> <composition> functions.yaml")
	d.Comment("No render.crossplane.io/runtime annotation is needed to render; the")
	d.Comment("docker-name annotation below only makes renders reuse one container")
	d.Comment("per function instead of leaking a new one on every run.")
	for i, f := range fns {
		if i > 0 {
			d.Line(0, "---")
		}
		d.Line(0, "apiVersion: pkg.crossplane.io/v1")
		d.Line(0, "kind: Function")
		d.Line(0, "metadata:")
		d.Line(1, "name: %s", f.name)
		d.Line(1, "annotations:")
		d.Line(2, "render.crossplane.io/runtime-docker-name: cf-%s", f.name)
		d.Line(0, "spec:")
		d.Line(1, "package: %s", f.pkg)
	}
	return d.Bytes(), nil
}
