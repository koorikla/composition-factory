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
	refs := AssembleProviders(store, b, cl, syncClusterNow)

	idx, err := api.BuildIndex(store, refs, b, filepath.Dir(blueprintPath))
	if err != nil {
		return api.Options{}, err
	}

	var cachedRefs []string
	if b == nil || len(b.Spec.Sources) == 0 {
		cached, _ := store.List()
		cachedRefs = cached
	}

	return api.Options{
		Index:           idx,
		Store:           store,
		Blueprint:       blueprintPath,
		OutDir:          outDir,
		Lock:            lockPath,
		Providers:       refs,
		CachedProviders: cachedRefs,
		Version:         version,
		ClusterClient:   cl,
	}, nil
}

// AssembleProviders collects the provider set to index and serve:
// 1. inspects declared blueprint sources in document order, deduplicating references
// 2. checks store cache presence, warning on os.Stderr for missing providers so startup continues with a partial index
// 3. if no blueprint sources are declared (or b is nil), falls back to all cached providers in store.List()
// 4. if a cluster client is provided, syncs or loads live cluster CRDs under cluster.ProviderLabel
func AssembleProviders(store *cache.Store, b *blueprint.Blueprint, cl *cluster.Client, syncClusterNow bool) []string {
	if store == nil {
		return nil
	}

	var refs []string
	seen := make(map[string]bool)

	if b != nil {
		for _, s := range b.Spec.Sources {
			if s.Provider != "" && !seen[s.Provider] {
				seen[s.Provider] = true
				if _, err := store.Load(s.Provider); err != nil {
					// A source missing from the cache no longer kills startup: the
					// server comes up with a partial index and the runtime auto-sync
					// fetches it on demand.
					fmt.Fprintf(os.Stderr, "cf: warning: provider %q is not in the cache — continuing without it; schemas load on demand\n", s.Provider)
					continue
				}
				refs = append(refs, s.Provider)
			}
		}
	}

	// If no blueprint sources found, discover all cached providers
	if b == nil || len(b.Spec.Sources) == 0 {
		cached, _ := store.List()
		for _, p := range cached {
			if !seen[p] {
				seen[p] = true
				refs = append(refs, p)
			}
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

	return refs
}
