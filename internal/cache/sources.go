package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// LoadSources resolves every schema source a blueprint declares: provider
// packages from the store, crds: manifests from disk (relative to
// blueprintDir), each scanned manifest object-rooted per
// schema.ParseCRDManifest. This is THE sources loop — cf gen, cf package
// and the HTTP server all load through it, so a source form supported in
// one front door is supported in all of them.
func LoadSources(store *Store, b *blueprint.Blueprint, blueprintDir string) ([]schema.CRD, error) {
	var crds []schema.CRD
	var lock *Lock
	if blueprintDir != "" {
		lock, _ = ReadLock(filepath.Join(blueprintDir, ".cf.lock"))
	}

	for _, s := range b.Spec.Sources {
		if s.CRDs != "" {
			path := s.CRDs
			if !filepath.IsAbs(path) {
				path = filepath.Join(blueprintDir, path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("crds source %q: %w", s.CRDs, err)
			}
			scanned, err := schema.ParseCRDManifest(data)
			if err != nil {
				return nil, fmt.Errorf("crds source %q: %w", s.CRDs, err)
			}
			crds = append(crds, scanned...)
			continue
		}
		ref := s.Provider
		if lock != nil {
			if entry, ok := lock.FindProvider(s.Provider); ok {
				ref = entry.Ref
			}
		}
		got, err := store.Load(ref)
		if err != nil {
			if ref != s.Provider {
				got, err = store.Load(s.Provider)
			}
			if err != nil {
				return nil, err
			}
		}
		crds = append(crds, got...)
	}

	if b != nil && store != nil {
		for _, step := range b.Spec.Pipeline {
			pkgRef := step.Package
			if pkgRef == "" && lock != nil {
				if entry, ok := lock.FindFunction(step.FunctionRef); ok {
					pkgRef = entry.Ref
				}
			}
			if pkgRef != "" {
				if got, err := store.Load(pkgRef); err == nil {
					crds = append(crds, got...)
				}
			}
		}
	}

	if lock != nil && store != nil {
		for _, f := range lock.Functions {
			if got, err := store.Load(f.Ref); err == nil {
				crds = append(crds, got...)
			}
		}
	}

	return crds, nil
}
