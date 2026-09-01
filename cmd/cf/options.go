package main

import (
	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	k8s "github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// buildAPIOptions loads the blueprint and every provider schema it names,
// builds the index over that one load, and assembles the api.Options both
// front doors — `cf serve` (HTTP) and `cf mcp` (stdio) — construct their
// server from. Extracted from ServeCmd.run when `cf mcp` arrived, so the two
// commands cannot drift apart in how they wire the engine.
//
// INVARIANT, carried from Task 6's review (this is a requirement, not a
// style preference): Options.Index and Options.Store MUST be built from
// the SAME CRD load, over exactly the providers named in the blueprint's
// spec.sources.
//
// internal/api's own tests deliberately diverge the two -- testIndex and
// the Store testHandlerWithPath seeds cover different CRD shapes for the
// same provider ref (see internal/api/server_test.go's
// testGenerateFixtureCRDs comment) -- and that is fine there, because it
// is the only way to exercise /api/kinds and /api/generate against
// independently-crafted fixtures without one test file's needs
// contorting the other's. It is NOT fine here: a production server whose
// index (what /api/kinds shows the canvas) disagrees with its store
// (what /api/generate actually reads when rendering) could advertise a
// field the browser lets someone add and then fail, or silently render
// against a different schema than the one just browsed. So: load each
// source provider's CRDs from the store exactly once into byProvider,
// build the index from that same map, and hand api.New that same store
// instance -- never a second, independently-loaded map or store for
// either side.
func buildAPIOptions(blueprintPath, cacheDir, outDir, lockPath string) (api.Options, error) {
	b, err := blueprint.Load(blueprintPath)
	if err != nil {
		return api.Options{}, err
	}

	store := cache.New(cacheDir)

	// refs doubles as Options.Providers: the exact provider set the index is
	// built over, in blueprint-source order, deduplicated the same way the
	// byProvider map inherently is -- so GET /api/providers lists precisely
	// what /api/kinds serves from, never a second, independently-derived set.
	byProvider := make(map[string][]schema.CRD, len(b.Spec.Sources))
	refs := make([]string, 0, len(b.Spec.Sources))
	for _, s := range b.Spec.Sources {
		if _, ok := byProvider[s.Provider]; ok {
			continue // a duplicate source entry names the same load
		}
		crds, err := store.Load(s.Provider)
		if err != nil {
			// cache.Store.Load's own error already names the exact command
			// to run ("provider %q is not in the cache; run: cf provider
			// add %s") -- returning it unwrapped keeps that message intact
			// rather than presenting an empty index with no explanation.
			return api.Options{}, err
		}
		byProvider[s.Provider] = crds
		refs = append(refs, s.Provider)
	}

	// The vendored native Kubernetes kinds are always in the index, under
	// their own provider label — no source entry names them (they are
	// compiled into the binary, pinned to one Kubernetes version) and no
	// blueprint opts into them. They deliberately do NOT join refs:
	// GET /api/providers lists xpkg packages with digests and cache entries,
	// and native kinds have neither — /api/kinds is where they surface,
	// wearing provider "k8s".
	native, err := k8s.Kinds()
	if err != nil {
		return api.Options{}, err
	}
	byProvider[blueprint.NativeProvider] = native

	idx, err := index.Build(byProvider)
	if err != nil {
		return api.Options{}, err
	}

	return api.Options{
		Index:     idx,
		Store:     store,
		Blueprint: blueprintPath,
		OutDir:    outDir,
		Lock:      lockPath,
		Providers: refs,
	}, nil
}
