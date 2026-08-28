package emit

import (
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Output is one generated file.
type Output struct {
	Path string
	Body []byte
}

// Generate renders every artifact for b into outDir. This is the ONLY entry
// point: the CLI, the HTTP server and the MCP server all call it, so a
// UI-authored artifact is always reproducible from the CLI.
func Generate(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error) {
	name := b.Spec.XRD.Plural + "." + b.Spec.XRD.Group + ".yaml"

	xrd, err := XRD(b)
	if err != nil {
		return nil, err
	}
	comp, err := Composition(b, crds)
	if err != nil {
		return nil, err
	}
	fns, err := Functions(b)
	if err != nil {
		return nil, err
	}
	// Sorted by path so callers can diff two runs positionally.
	return []Output{
		{Path: filepath.Join(outDir, "compositions", name), Body: comp},
		{Path: filepath.Join(outDir, "functions.yaml"), Body: fns},
		{Path: filepath.Join(outDir, "xrds", name), Body: xrd},
	}, nil
}
