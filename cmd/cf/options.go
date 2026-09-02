package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/cluster"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	k8s "github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// buildAPIOptions loads the blueprint and every provider schema it names,
// builds the index over that one load, and assembles the api.Options both
// front doors — `cf serve` (HTTP) and `cf mcp` (stdio) — construct their
// server from. Extracted from ServeCmd.run when `cf mcp` arrived, so the two
// commands cannot drift apart in how they wire the engine.
func buildAPIOptions(blueprintPath, cacheDir, outDir, lockPath string, cl *cluster.Client, syncClusterNow bool) (api.Options, error) {
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
		// crds: sources are scanned manifest files; they index under their
		// own path label so the palette groups them per file. They skip
		// refs: /api/providers lists xpkg packages with digests, and a
		// manifest file has neither.
		if s.CRDs != "" {
			if _, ok := byProvider[s.CRDs]; ok {
				continue
			}
			path := s.CRDs
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(blueprintPath), path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return api.Options{}, fmt.Errorf("crds source %q: %w", s.CRDs, err)
			}
			scanned, err := schema.ParseCRDManifest(data)
			if err != nil {
				return api.Options{}, fmt.Errorf("crds source %q: %w", s.CRDs, err)
			}
			byProvider[s.CRDs] = scanned
			continue
		}
		if _, ok := byProvider[s.Provider]; ok {
			continue // a duplicate source entry names the same load
		}
		crds, err := store.Load(s.Provider)
		if err != nil {
			// A source missing from the cache no longer kills startup: the
			// server comes up with a partial index and the runtime auto-sync
			// (syncBlueprintSourcesLocked) fetches it on the next import,
			// example load or provider add — cold starts need no pre-warmed
			// cache and no network. The warning keeps Load's own message,
			// which names the exact command to run.
			fmt.Fprintf(os.Stderr, "cf: warning: %v — continuing without it; schemas load on demand\n", err)
			continue
		}
		byProvider[s.Provider] = crds
		refs = append(refs, s.Provider)
	}

	// If cluster client is provided, load or sync live cluster CRDs
	if cl != nil {
		if syncClusterNow {
			if clusterCRDs, err := cl.FetchCRDs(context.Background()); err == nil && len(clusterCRDs) > 0 {
				_ = store.SaveCRDs(cluster.ProviderLabel, cl.Context(), clusterCRDs)
				byProvider[cluster.ProviderLabel] = clusterCRDs
				refs = append(refs, cluster.ProviderLabel)
			}
		} else if clusterCRDs, err := store.Load(cluster.ProviderLabel); err == nil && len(clusterCRDs) > 0 {
			byProvider[cluster.ProviderLabel] = clusterCRDs
			refs = append(refs, cluster.ProviderLabel)
		}
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
		Index:         idx,
		Store:         store,
		Blueprint:     blueprintPath,
		OutDir:        outDir,
		Lock:          lockPath,
		Providers:     refs,
		Version:       version,
		ClusterClient: cl,
	}, nil
}
