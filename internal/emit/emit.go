package emit

import (
	"path/filepath"
	"sort"

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
//
// It validates b itself rather than trusting the caller to have done so.
// blueprint.Load validates, but Load is only the CLI's path to a Blueprint:
// the HTTP and MCP front doors build one in memory from a request body and
// never touch it. Leaving validation to the caller therefore made every
// invariant Validate enforces optional in exactly the two front doors that
// have not been written yet -- and the failure is quiet. A Blueprint with
// Scope: "" reached resolveKind, which compares against a bool and so
// silently selected the LEGACY cluster-scoped variant (whose fields the API
// server prunes), while the XRD emitted a null `scope:`. Both artifacts
// parse. Validating here makes "the one entry point" mean the one place the
// rules are enforced too, not just the one place the files are assembled.
func Generate(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
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
	pcs, err := ProviderConfigs(b, crds)
	if err != nil {
		return nil, err
	}

	// Sorted by path so callers can diff two runs positionally. providerconfigs
	// entries are variable in count (zero-to-many families), so they are
	// collected and path-sorted separately, then spliced in between
	// functions.yaml and xrds/ -- "providerconfigs" sorts after "functions.yaml"
	// and before "xrds" byte-wise, so this preserves the whole-list invariant
	// with the fixed three entries alone.
	families := make([]string, 0, len(pcs))
	for fam := range pcs {
		families = append(families, fam)
	}
	sort.Strings(families)

	out := make([]Output, 0, 3+len(families))
	out = append(out,
		Output{Path: filepath.Join(outDir, "compositions", name), Body: comp},
		Output{Path: filepath.Join(outDir, "functions.yaml"), Body: fns},
	)
	for _, fam := range families {
		out = append(out, Output{Path: filepath.Join(outDir, "providerconfigs", fam+".yaml"), Body: pcs[fam]})
	}
	out = append(out, Output{Path: filepath.Join(outDir, "xrds", name), Body: xrd})
	return out, nil
}
