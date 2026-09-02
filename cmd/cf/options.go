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
	// built over, in blueprint-source order, deduplicated.
	refs := make([]string, 0, len(b.Spec.Sources))
	seen := make(map[string]bool, len(b.Spec.Sources))
	for _, s := range b.Spec.Sources {
		if s.Provider != "" && !seen[s.Provider] {
			seen[s.Provider] = true
			if _, err := store.Load(s.Provider); err != nil {
				// A source missing from the cache no longer kills startup: the
				// server comes up with a partial index and the runtime auto-sync
				// fetches it on demand.
				fmt.Fprintf(os.Stderr, "cf: warning: %v — continuing without it; schemas load on demand\n", err)
				continue
			}
			refs = append(refs, s.Provider)
		}
	}

	// If cluster client is provided, load or sync live cluster CRDs
	if cl != nil {
		if syncClusterNow {
			if clusterCRDs, err := cl.FetchCRDs(context.Background()); err == nil && len(clusterCRDs) > 0 {
				_ = store.SaveCRDs(cluster.ProviderLabel, cl.Context(), clusterCRDs)
				if !seen[cluster.ProviderLabel] {
					seen[cluster.ProviderLabel] = true
					refs = append(refs, cluster.ProviderLabel)
				}
			}
		} else if clusterCRDs, err := store.Load(cluster.ProviderLabel); err == nil && len(clusterCRDs) > 0 {
			if !seen[cluster.ProviderLabel] {
				seen[cluster.ProviderLabel] = true
				refs = append(refs, cluster.ProviderLabel)
			}
		}
	}

	idx, err := api.BuildIndex(store, refs, b, filepath.Dir(blueprintPath))
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
