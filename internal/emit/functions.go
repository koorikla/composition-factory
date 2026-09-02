package emit

import (
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
	rcName := ""
	if b.TemplateSource() == blueprint.TemplateSourceFileSystem {
		rcName = blueprint.TemplatingFunctionName
	}
	return functionsDoc(b, rcName)
}

// functionsDoc renders functions.yaml; a non-empty runtimeConfigName pins a
// DeploymentRuntimeConfig onto the templating function (the FileSystem
// export's ConfigMap mounts — see fsexport.go).
func functionsDoc(b *blueprint.Blueprint, runtimeConfigName string) ([]byte, error) {
	fns, err := functionList(b)
	if err != nil {
		return nil, err
	}

	d := NewDoc()
	header(d, blueprintSource(b))
	d.Comment("Required by: crossplane composition render <xr> <composition> functions.yaml")
	d.Comment("No render.crossplane.io/runtime annotation is needed to render.")
	for i, f := range fns {
		if i > 0 {
			d.Line(0, "---")
		}
		d.Line(0, "apiVersion: pkg.crossplane.io/v1")
		d.Line(0, "kind: Function")
		d.Line(0, "metadata:")
		d.Line(1, "name: %s", f.name)
		d.Line(0, "spec:")
		d.Line(1, "package: %s", f.pkg)
		if runtimeConfigName != "" && f.name == blueprint.TemplatingFunctionName {
			d.Line(1, "runtimeConfigRef:")
			d.Line(2, "name: %s", runtimeConfigName)
		}
	}
	return d.Bytes(), nil
}
