package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// functionList is the deterministic function set behind functions.yaml and
// the Configuration meta's dependsOn: the built-in templating function first,
// then one entry per DISTINCT functionRef in the effective pipeline, each
// carrying its package exactly as the blueprint declared it.
func functionList(b *blueprint.Blueprint) ([]fn, error) {
	primaryFn := templatingFunction
	if b.Engine() == blueprint.EngineKCL {
		primaryFn = fn{
			blueprint.KCLFunctionName,
			blueprint.KCLFunctionPackage,
		}
	} else if b.Engine() == blueprint.EnginePython {
		primaryFn = fn{
			blueprint.PythonFunctionName,
			blueprint.PythonFunctionPackage,
		}
	}
	fns := []fn{primaryFn}
	declared := map[string]string{primaryFn.name: primaryFn.pkg}
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
	return fns, nil
}

// splitRef splits an OCI ref into the bare package path and a dependsOn
// version constraint. A tag becomes an exact "=<tag>" pin; a digest ref is
// kept verbatim on the package with no constraint (a digest is not semver,
// and inventing one would lie); an untagged ref floats.
func splitRef(ref string) (pkg, version string) {
	if strings.Contains(ref, "@") {
		return ref, ""
	}
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon], "=" + ref[colon+1:]
	}
	return ref, ""
}

// ConfigurationMeta renders crossplane.yaml, the meta document of a
// Crossplane Configuration package: one Provider dependency per declared
// source, one Function dependency per functions.yaml entry, and — when the
// caller supplies it — the blueprint source embedded verbatim under a
// block-scalar annotation, the same recovery story as the cf gen headers.
func ConfigurationMeta(b *blueprint.Blueprint, source []byte) ([]byte, error) {
	fns, err := functionList(b)
	if err != nil {
		return nil, err
	}

	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Line(0, "apiVersion: meta.pkg.crossplane.io/v1")
	d.Line(0, "kind: Configuration")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s", b.Metadata.Name)
	if len(source) > 0 {
		d.Line(1, "annotations:")
		d.Line(2, "factory.crossplane.io/blueprint: |")
		text := strings.TrimRight(string(source), "\n")
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				d.Line(0, "") // no trailing spaces on blank source lines
				continue
			}
			d.Line(3, "%s", line)
		}
	}
	d.Line(0, "spec:")
	d.Line(1, "crossplane:")
	d.Line(2, "version: %s", quoteYAML(">=v2.0.0"))
	d.Line(1, "dependsOn:")
	for _, src := range b.Spec.Sources {
		if src.Provider == "" {
			continue // a crds: source is a scanned manifest, not an installable package
		}
		pkg, version := splitRef(src.Provider)
		d.Line(1, "- apiVersion: pkg.crossplane.io/v1")
		d.Line(2, "kind: Provider")
		d.Line(2, "package: %s", pkg)
		if version != "" {
			d.Line(2, "version: %s", quoteYAML(version))
		}
	}
	for _, f := range fns {
		pkg, version := splitRef(f.pkg)
		d.Line(1, "- apiVersion: pkg.crossplane.io/v1")
		d.Line(2, "kind: Function")
		d.Line(2, "package: %s", pkg)
		if version != "" {
			d.Line(2, "version: %s", quoteYAML(version))
		}
	}
	return d.Bytes(), nil
}
