package emit

import "github.com/koorikla/compositionfactory/internal/blueprint"

// fn is one pipeline function and the package that provides it.
type fn struct{ name, pkg string }

// M1 pins the versions verified against Crossplane v2.4.0.
var defaultFunctions = []fn{
	{"function-go-templating", "xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0"},
	{"function-auto-ready", "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"},
}

// Functions renders functions.yaml, the required third argument to
// `crossplane composition render`.
func Functions(b *blueprint.Blueprint) ([]byte, error) {
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Comment("Required by: crossplane composition render <xr> <composition> functions.yaml")
	d.Comment("No render.crossplane.io/runtime annotation is needed to render; the")
	d.Comment("docker-name annotation below only makes renders reuse one container")
	d.Comment("per function instead of leaking a new one on every run.")
	for i, f := range defaultFunctions {
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
