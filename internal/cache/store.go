// Package cache persists extracted provider schemas on disk and pins the
// resolved image digests so generation is reproducible.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
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

// slug turns an image reference into a filesystem-safe, collision-free
// directory name: <readable>-<12 hex chars of sha256(ref)>.
//
// The hash covers the FULL reference, so it alone guarantees no two distinct
// refs ever collide. The readable prefix (the ref's last path segment, tag
// or digest suffix stripped, sanitised to [A-Za-z0-9._-]) exists only so the
// cache directory is browsable with `ls`; it is not relied on for
// uniqueness. That distinction matters because a naive scheme that maps `/`,
// `:` and `@` all to the same separator is lossy: "registry:5000/repo" and
// "registry/5000/repo" would both flatten to "registry_5000_repo" and one
// provider's cached CRDs would silently overwrite another's.
func slug(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	hash := hex.EncodeToString(sum[:])[:12]

	last := ref
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i+1:]
	}
	if i := strings.Index(last, "@"); i >= 0 {
		last = last[:i]
	} else if i := strings.LastIndex(last, ":"); i >= 0 {
		last = last[:i]
	}
	last = sanitizeSlugSegment(last)
	if last == "" {
		last = "ref"
	}
	return last + "-" + hash
}

// sanitizeSlugSegment replaces every rune outside [A-Za-z0-9._-] with "_".
func sanitizeSlugSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// Entry is what Store persists for one provider: the CRDs it extracted plus
// the ref and digest they came from, so a cache entry can later be checked
// against the digest pinned in .cf.lock (see LoadDigest). Without this, Save
// and Lock.Set falling out of sync would let Load silently serve schemas
// that no longer match the pin — quietly breaking the reproducibility
// guarantee the lockfile exists to provide.
type Entry struct {
	Ref    string       `json:"ref"`
	Digest string       `json:"digest"`
	CRDs   []schema.CRD `json:"crds"`
}

// Save writes the parsed CRDs for pkg, along with pkg.Ref and pkg.Digest, into the cache.
func (s *Store) Save(pkg *xpkg.Package, crds []schema.CRD) error {
	return s.SaveCRDs(pkg.Ref, pkg.Digest, crds)
}

// SaveCRDs writes parsed CRDs for an arbitrary provider ref and digest into the cache.
func (s *Store) SaveCRDs(ref, digest string, crds []schema.CRD) error {
	dir := filepath.Join(s.Root, slug(ref))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	entry := Entry{Ref: ref, Digest: digest, CRDs: crds}
	body, err := json.MarshalIndent(entry, "", " ")
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "crds.json"), body, 0o644)
}

// loadEntry reads and decodes the cache entry for ref.
func (s *Store) loadEntry(ref string) (*Entry, error) {
	path := filepath.Join(s.Root, slug(ref), "crds.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider %q is not in the cache; run: cf provider add %s", ref, ref)
	}
	var entry Entry
	if err := json.Unmarshal(body, &entry); err != nil {
		return nil, fmt.Errorf("decode cached entry for %q: %w", ref, err)
	}
	return &entry, nil
}

// Load returns the cached CRDs for ref.
func (s *Store) Load(ref string) ([]schema.CRD, error) {
	entry, err := s.loadEntry(ref)
	if err != nil {
		return nil, err
	}
	return entry.CRDs, nil
}

// LoadDigest returns the digest recorded for ref when it was saved, so a
// caller can cross-check a cache entry against the pin in .cf.lock.
func (s *Store) LoadDigest(ref string) (string, error) {
	entry, err := s.loadEntry(ref)
	if err != nil {
		return "", err
	}
	return entry.Digest, nil
}

// Delete removes the cached entry for ref. A ref with no cache entry is a
// no-op, not an error: the caller's intent — "this provider must not be in
// the cache" — is already satisfied, and slug's full-ref hashing means the
// removed directory can never belong to a different provider.
func (s *Store) Delete(ref string) error {
	if err := os.RemoveAll(filepath.Join(s.Root, slug(ref))); err != nil {
		return fmt.Errorf("delete cached entry for %q: %w", ref, err)
	}
	return nil
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

// Remove deletes the entry for ref, reporting whether one was present — so a
// caller can skip rewriting the lockfile when nothing changed.
func (l *Lock) Remove(ref string) bool {
	for i := range l.Providers {
		if l.Providers[i].Ref == ref {
			l.Providers = append(l.Providers[:i], l.Providers[i+1:]...)
			return true
		}
	}
	return false
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
