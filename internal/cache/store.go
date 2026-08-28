// Package cache persists extracted provider schemas on disk and pins the
// resolved image digests so generation is reproducible.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// Store is a directory of cached provider schemas.
type Store struct{ Root string }

// New returns a Store rooted at root. Use DefaultRoot for the usual location.
func New(root string) *Store { return &Store{Root: root} }

// DefaultRoot is ~/.cache/compositionfactory, or ./.cf-cache if HOME is unset.
func DefaultRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ".cf-cache"
	}
	return filepath.Join(dir, "compositionfactory")
}

// slug turns an image reference into a filesystem-safe directory name.
func slug(ref string) string {
	r := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return r.Replace(ref)
}

// Save writes the parsed CRDs for pkg into the cache.
func (s *Store) Save(pkg *xpkg.Package, crds []schema.CRD) error {
	dir := filepath.Join(s.Root, slug(pkg.Ref))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	body, err := json.MarshalIndent(crds, "", " ")
	if err != nil {
		return fmt.Errorf("encode CRDs: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "crds.json"), body, 0o644)
}

// Load returns the cached CRDs for ref.
func (s *Store) Load(ref string) ([]schema.CRD, error) {
	path := filepath.Join(s.Root, slug(ref), "crds.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider %q is not in the cache; run: cf provider add %s", ref, ref)
	}
	var crds []schema.CRD
	if err := json.Unmarshal(body, &crds); err != nil {
		return nil, fmt.Errorf("decode cached CRDs for %q: %w", ref, err)
	}
	return crds, nil
}

// LockEntry pins one provider reference to a resolved digest.
type LockEntry struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

// Lock is the contents of .cf.lock.
type Lock struct {
	Providers []LockEntry `json:"providers"`
}

// Set adds or replaces the entry for ref and keeps the list sorted.
func (l *Lock) Set(ref, digest string) {
	for i := range l.Providers {
		if l.Providers[i].Ref == ref {
			l.Providers[i].Digest = digest
			return
		}
	}
	l.Providers = append(l.Providers, LockEntry{Ref: ref, Digest: digest})
	sort.Slice(l.Providers, func(i, j int) bool { return l.Providers[i].Ref < l.Providers[j].Ref })
}

// ReadLock reads path. A missing file is an empty lock, not an error.
func ReadLock(path string) (*Lock, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var l Lock
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &l, nil
}

// Write saves the lock with a trailing newline, sorted and stable.
func (l *Lock) Write(path string) error {
	sort.Slice(l.Providers, func(i, j int) bool { return l.Providers[i].Ref < l.Providers[j].Ref })
	body, err := json.MarshalIndent(l, "", " ")
	if err != nil {
		return fmt.Errorf("encode lock: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
