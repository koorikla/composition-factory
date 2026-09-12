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
	"github.com/koorikla/compositionfactory/internal/schema"
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
	cached, _ := store.List()
	declared := make(map[string]bool)
	if b != nil {
		for _, s := range b.Spec.Sources {
			if s.Provider != "" {
				declared[s.Provider] = true
			}
		}
	}
	for _, p := range cached {
		if !declared[p] {
			cachedRefs = append(cachedRefs, p)
		}
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
// 1. if a cluster client is provided and syncClusterNow is true, syncs live cluster CRDs and installed Crossplane providers into the store
// 2. inspects declared blueprint sources in document order, deduplicating references
// 3. checks store cache presence, warning on os.Stderr for missing providers so startup continues with a partial index
// 4. if no blueprint sources are declared (or b is nil), falls back to all cached providers in store.List()
// 5. if a cluster client is provided, ensures cluster.ProviderLabel is included in refs
func AssembleProviders(store *cache.Store, b *blueprint.Blueprint, cl *cluster.Client, syncClusterNow bool) []string {
	if store == nil {
		return nil
	}

	var refs []string
	seen := make(map[string]bool)

	// If cluster client is provided and syncClusterNow is true, sync live cluster CRDs and installed providers first
	if cl != nil && syncClusterNow {
		provCRDs, _ := cl.FetchCRDsByProvider(context.Background())
		installed, _ := cl.FetchInstalledProviders(context.Background())
		installedCRDNames := make(map[string]bool)

		for _, inst := range installed {
			crds := provCRDs[inst.Package]
			if len(crds) == 0 && inst.Name != "" {
				crds = provCRDs[inst.Name]
			}
			if len(crds) > 0 {
				for _, c := range crds {
					installedCRDNames[c.Plural+"."+c.Group] = true
				}
				if inst.Package != "" {
					_ = store.SaveCRDs(inst.Package, cl.Context(), crds)
				}
				if inst.Name != "" && inst.Name != inst.Package {
					_ = store.SaveCRDs(inst.Name, cl.Context(), crds)
				}
			}
		}

		if clusterCRDs, err := cl.FetchCRDs(context.Background()); err == nil && len(clusterCRDs) > 0 {
			var nonProviderCRDs []schema.CRD
			for _, c := range clusterCRDs {
				if !installedCRDNames[c.Plural+"."+c.Group] {
					nonProviderCRDs = append(nonProviderCRDs, c)
				}
			}
			if len(nonProviderCRDs) > 0 {
				_ = store.SaveCRDs(cluster.ProviderLabel, cl.Context(), nonProviderCRDs)
			} else {
				_ = store.SaveCRDs(cluster.ProviderLabel, cl.Context(), nil)
			}
		}
	}

	if b != nil {
		for _, s := range b.Spec.Sources {
			if s.Provider != "" && !seen[s.Provider] {
				if _, err := store.Load(s.Provider); err != nil {
					// A source missing from the cache no longer kills startup: the
					// server comes up with a partial index and the runtime auto-sync
					// fetches it on demand.
					fmt.Fprintf(os.Stderr, "cf: warning: provider %q is not in the cache — continuing without it; schemas load on demand\n", s.Provider)
					continue
				}
				seen[s.Provider] = true
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

	// If cluster client is provided, ensure cluster.ProviderLabel is included
	if cl != nil {
		if syncClusterNow {
			if !seen[cluster.ProviderLabel] {
				if _, err := store.Load(cluster.ProviderLabel); err == nil {
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
