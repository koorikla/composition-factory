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
		got, err := store.Load(s.Provider)
		if err != nil {
			return nil, err
		}
		crds = append(crds, got...)
	}
	return crds, nil
}
