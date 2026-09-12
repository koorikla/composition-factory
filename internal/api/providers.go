// This file implements the /api/providers routes: listing the providers the
// server is currently serving kinds from, and adding one at runtime.
//
// The list is served from the server's own provider set — srv.Providers, the
// refs Options was built with plus any added over POST — never re-derived
// from the index's kinds. Deriving it from the index would silently drop any
// provider with zero managed kinds (a family package carries only
// ProviderConfig types; see ProviderAddCmd's note in cmd/cf/provider.go),
// and "I added it and it vanished from the list" is exactly the silent
// wrongness this project exists to avoid.
package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// providerEntry is one provider in GET /api/providers' response — and the
// "provider" half of POST's. Kinds is a count, not the kinds themselves; the
// canvas fetches those from /api/kinds, which stays the one source for kind
// listings.
//
// Error is set only on a source the blueprint declares that the server could
// not load (CF-152): the fetch failed and the ref never entered srv.Providers,
// so it has no digest and no kinds, but it is still part of the document and
// still the reason generate answers 400. Listing it — with the reason — is
// what lets the canvas offer the remove/replace that repairs the document;
// before, the only way out was hand-editing the YAML.
type providerEntry struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
	Kinds  int    `json:"kinds"`
	Error  string `json:"error,omitempty"`
}

// handleListProviders serves GET /api/providers:
// {"providers":[{"ref":...,"digest":...,"kinds":N}]}, in the server's own
// provider order (blueprint-source order, then POST order).
//
// srv.mu is held for the whole response, digest reads included. It used to be
// released after snapshotting the ref list and the index, with the digests
// read from the store afterwards — safe back then because cache entries were
// written before a ref ever entered srv.Providers and nothing deleted them.
// DELETE /api/providers/{ref} broke that second premise: it evicts a cache
// entry, so a list that read digests unlocked could snapshot a ref, lose the
// race to a concurrent DELETE, and then 500 on LoadDigest for a provider it
// had every reason to believe was cached. Under the same lock the DELETE
// swaps under, the list always describes a provider set whose cache entries
// all still exist.
func (srv *server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	// A declared source the server has not tried yet is neither held nor
	// known to have failed; attempt it (memoized, so a failed ref is not
	// re-fetched on every list) so the list can name the reason.
	_ = srv.ensureBlueprintSourcesLoadedLocked(r.Context(), b)

	entries, err := srv.providerEntriesLocked(b)
	if err != nil {
		// The server's own cache no longer holds a provider it was
		// started with (or added) — its fixed environment is broken, not
		// the caller's request. The store's error already names the exact
		// `cf provider add` command that repairs it; surface it verbatim.
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
}

// providerEntriesLocked builds the {"providers":[...]} entry list for the
// server's current provider set — the one envelope both GET /api/providers
// and DELETE /api/providers/{ref} serve, built in one place so the two can
// never disagree on its shape — followed by every source b declares that the
// server does not hold, each carrying the fetch failure as its Error (see
// providerEntry). srv.mu must be held by the caller.
func (srv *server) providerEntriesLocked(b *blueprint.Blueprint) ([]providerEntry, error) {
	counts := kindCountsByProvider(srv.Index)
	entries := make([]providerEntry, 0, len(srv.Providers))
	held := make(map[string]bool, len(srv.Providers))
	for _, ref := range srv.Providers {
		digest, err := srv.Store.LoadDigest(ref)
		if err != nil {
			return nil, err
		}
		held[ref] = true
		entries = append(entries, providerEntry{Ref: ref, Digest: digest, Kinds: counts[ref]})
	}
	if b == nil {
		return entries, nil
	}
	seen := make(map[string]bool)
	for _, s := range b.Spec.Sources {
		ref := s.Provider
		if ref == "" || ref == blueprint.NativeProvider || held[ref] || seen[ref] {
			continue
		}
		seen[ref] = true
		reason := "source is declared but not loaded"
		if err := srv.failedSources[ref]; err != nil {
			reason = err.Error()
		}
		entries = append(entries, providerEntry{Ref: ref, Error: reason})
	}
	return entries, nil
}

// kindCountsByProvider returns the indexed kind count per provider ref.
func kindCountsByProvider(idx *index.Index) map[string]int {
	if idx == nil {
		return nil
	}
	return idx.CountsByProvider()
}

// addProviderRequest is the POST /api/providers body. Replaces names a
// declared source the new ref takes the place of (CF-152): its spec.sources
// entry becomes Ref, every resource pinned to it re-points to Ref, and if
// the server held it, it is evicted the way DELETE evicts. It is how a
// source that failed to load is repaired without hand-editing the document.
type addProviderRequest struct {
	Ref      string `json:"ref"`
	Replaces string `json:"replaces,omitempty"`
}

// handleAddProvider serves POST /api/providers: {"ref":"ghcr.io/..."} ->
// fetch the package, cache its CRDs, pin its digest into the lockfile, and
// rebuild the server's index so /api/kinds reflects the new provider on the
// very next request. 200 carries the new provider's entry plus the kinds it
// added; 400 is a caller-fault request (bad body, missing/unparseable ref),
// 409 the exact ref the server already serves, 502 a pull failure with the
// fetch error's text verbatim (it names the registry's own reason), 500 the
// server's own environment failing (lockfile, cache, index rebuild).
//
// srv.mu is held from the duplicate check through the index swap — the same
// whole-sequence lost-update discipline as blueprint PUT (see server.mu).
// Two concurrent adds otherwise both rebuild from the same starting index
// and the second swap silently discards the first's kinds; two adds of the
// same ref would both pass the duplicate check and pull twice. Holding the
// lock across the fetch also serializes provider adds against blueprint
// edits and generation for the duration of a pull; for this loopback,
// single-user server that is the acceptable cost of making the check-fetch-
// pin-swap sequence one atomic step, not a gap-riddled pipeline.
//
// On-disk ordering inside the critical section mirrors ProviderAddCmd (see
// cmd/cf/provider.go): lock first, then cache. A failure between the two
// leaves a pin with no cached entry, which Load reports loudly with its own
// "run: cf provider add <ref>" message — a visible, recoverable state,
// unlike cached schemas nothing pins. A failure after both (the index
// rebuild itself) leaves that same recoverable on-disk state and swaps
// nothing in memory: the server keeps serving exactly what it served
// before, and a retry of the same POST re-runs the whole sequence.
func (srv *server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var req addProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Ref == "" {
		writeJSONError(w, http.StatusBadRequest, "ref is required")
		return
	}
	if err := xpkg.ValidateRef(req.Ref); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}

	isProvider := false
	for _, ref := range srv.Providers {
		if ref == req.Ref {
			isProvider = true
			break
		}
	}

	hasSource := false
	for _, s := range b.Spec.Sources {
		if s.Provider == req.Ref {
			hasSource = true
			break
		}
	}

	replaces := req.Replaces
	if replaces == req.Ref {
		replaces = ""
	}
	if replaces != "" && !declaresProvider(b, replaces) {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("source not declared: %q", replaces))
		return
	}

	// Only return 409 Conflict if already in srv.Providers AND already declared
	// in spec.sources — and nothing is being replaced: swapping a declared
	// source for one the server already serves is still a real change.
	if isProvider && hasSource && replaces == "" {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("provider %q is already cached", req.Ref))
		return
	}

	pkg, crds, err := srv.Store.FetchAndSave(r.Context(), "", req.Ref, srv.fetch)
	if err != nil {
		if cache.IsLockError(err) {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	managed := 0
	inputs := 0
	for _, crd := range crds {
		if crd.IsManaged() {
			managed++
		}
		if crd.IsFunctionInput() || crd.Function {
			inputs++
		}
	}
	if inputs > 0 && managed == 0 {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("package %q is a function package, not a provider (use 'cf function add %s')", req.Ref, req.Ref))
		return
	}

	if err := srv.Store.PinLock(srv.Lock, req.Ref, pkg.Digest); err != nil {
		if cache.IsLockError(err) {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pkgDigest := pkg.Digest
	origProviders := append([]string(nil), srv.Providers...)
	if !isProvider {
		srv.Providers = append(srv.Providers, req.Ref)
	}

	if !hasSource && replaces == "" {
		b.Spec.Sources = append(b.Spec.Sources, blueprint.Source{Provider: req.Ref})
	}
	if replaces != "" {
		replaceProvider(b, replaces, req.Ref)
		delete(srv.failedSources, replaces)
		remaining := make([]string, 0, len(srv.Providers))
		for _, p := range srv.Providers {
			if p != replaces {
				remaining = append(remaining, p)
			}
		}
		srv.Providers = remaining
	}

	if !hasSource || replaces != "" {
		if err := writeBlueprintFile(srv.Blueprint, b); err != nil {
			srv.Providers = origProviders
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := srv.rebuildIndexLocked(b); err != nil {
		srv.Providers = origProviders
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The replaced ref, if the server held it, is evicted the way DELETE
	// evicts — cache entry and lock pin — after the swap has landed. One
	// that never loaded left nothing on disk to evict.
	if replaces != "" && contains(origProviders, replaces) {
		if err := srv.evictProviderLocked(replaces); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	added := []index.Kind{}
	for _, k := range srv.Index.All() {
		if k.Provider == req.Ref {
			added = append(added, k)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": providerEntry{Ref: req.Ref, Digest: pkgDigest, Kinds: len(added)},
		"kinds":    added,
	})
}

// pathProviderRef extracts and unescapes the {ref} path wildcard. An xpkg
// ref contains slashes, so the client sends it URL-path-escaped
// (encodeURIComponent: "ghcr.io%2Fx%2Fprovider-aws-sqs%3Av2.7.0") and
// ServeMux keeps the escaped form inside the one segment; PathValue comes
// back already unescaped. The explicit PathUnescape below is the same
// defense-in-depth pathAPIVersion applies to {apiVersion} — see its doc
// comment in kinds.go for the full reasoning; it is not what makes the happy
// path work.
func pathProviderRef(r *http.Request) (string, error) {
	raw := r.PathValue("ref")
	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid ref: %q", raw)
	}
	return unescaped, nil
}

// handleDeleteProvider serves DELETE /api/providers/{ref}: remove a provider
// the server currently serves — evict its cached schemas and its lockfile
// pin, and rebuild the index over the remaining providers — answering 200
// with the remaining providers list, the same {"providers":[...]} envelope
// GET serves. A ref the server does not hold but the blueprint declares is
// a source that failed to load (CF-152): it is removed from spec.sources and
// answered 200 the same way, since that document edit is the whole repair.
// 404 is a ref neither held nor declared; 409 is a ref a resource still
// pins, with the message naming every referencer, the same refuse-and-name
// discipline DeleteParameter applies to a still-referenced parameter
// (internal/blueprint/edit.go): the user fixes every reference in one
// round-trip instead of discovering a broken blueprint at the next generate.
//
// srv.mu is held for the whole sequence — the same whole-sequence discipline
// as handleAddProvider, and for the same reason: the referencer check, the
// rebuild and the swap must see one consistent (blueprint, index, provider
// set) or a concurrent add/delete loses its update.
//
// Ordering inside the critical section mirrors handleAddProvider's reasoning
// in reverse. The replacement index is built FIRST, before anything on disk
// is touched, so a rebuild failure changes nothing at all. Then cache
// eviction, then the lock pin: a failure between the two leaves a pin with no
// cached entry — the same loud, recoverable state a failed add can leave
// (Load names the exact `cf provider add <ref>` that repairs it), rather
// than the silent reverse (a cached entry nothing pins). The in-memory swap
// happens last, only after every on-disk step succeeded.
func (srv *server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	ref, err := pathProviderRef(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	// The referencer check reads the blueprint from disk, like every handler
	// that consults it — the file is the source of truth, and a copy held
	// since some earlier request could miss a source or resource added since.
	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}

	held := contains(srv.Providers, ref)
	if !held && !declaresProvider(b, ref) {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("provider not found: %q", ref))
		return
	}
	if msg := providerReferencers(b, ref); msg != "" {
		writeJSONError(w, http.StatusConflict, msg)
		return
	}

	if !held {
		// A declared source the server never loaded (CF-152): there is no
		// cache entry, pin or index share to evict — removing it is purely
		// a document edit, plus forgetting the fetch failure it left behind.
		b.Spec.Sources = withoutProvider(b.Spec.Sources, ref)
		if err := writeBlueprintFile(srv.Blueprint, b); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		delete(srv.failedSources, ref)
		entries, err := srv.providerEntriesLocked(b)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
		return
	}

	// Remove from spec.sources if present
	if declaresProvider(b, ref) {
		b.Spec.Sources = withoutProvider(b.Spec.Sources, ref)
		if err := writeBlueprintFile(srv.Blueprint, b); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	remaining := make([]string, 0, len(srv.Providers)-1)
	for _, p := range srv.Providers {
		if p != ref {
			remaining = append(remaining, p)
		}
	}
	oldProviders := srv.Providers
	srv.Providers = remaining
	if err := srv.rebuildIndexLocked(); err != nil {
		srv.Providers = oldProviders
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := srv.evictProviderLocked(ref); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entries, err := srv.providerEntriesLocked(b)
	if err != nil {
		// The delete itself has landed; this is the server's environment
		// failing to describe the survivors, same classification as GET's.
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
}

// providerReferencers returns the 409 message for deleting ref while the
// blueprint still references it, or "" when nothing does. Referencers are
// the blueprint's spec.sources entries and every resource whose provider
// names ref; resources are listed by name, in resource order, mirroring
// DeleteParameter's still-referenced-by message shape.
func providerReferencers(b *blueprint.Blueprint, ref string) string {
	inSources := false
	for _, s := range b.Spec.Sources {
		if s.Provider == ref {
			inSources = true
			break
		}
	}
	var resources []string
	for _, res := range b.Spec.Resources {
		if res.Provider == ref {
			resources = append(resources, fmt.Sprintf("%q", res.Name))
		}
	}
	if len(resources) == 0 {
		return ""
	}
	var parts []string
	if inSources {
		parts = append(parts, "the blueprint's sources")
	}
	parts = append(parts, "resources "+strings.Join(resources, ", "))
	return fmt.Sprintf("delete provider %q: still referenced by %s", ref, strings.Join(parts, " and by "))
}

// evictProviderLocked removes ref's cached schemas and its lockfile pin —
// the on-disk half of a delete, after the in-memory swap has landed. A ref
// never pinned (or pinned elsewhere) is fine; the lock is not rewritten for
// a no-op. srv.mu must be held by the caller.
func (srv *server) evictProviderLocked(ref string) error {
	if err := srv.Store.Delete(ref); err != nil {
		return err
	}
	l, err := cache.ReadLock(srv.Lock)
	if err != nil {
		return err
	}
	if l.Remove(ref) {
		if err := l.Write(srv.Lock); err != nil {
			return err
		}
	}
	return nil
}

// withoutProvider returns sources minus every entry naming ref.
func withoutProvider(sources []blueprint.Source, ref string) []blueprint.Source {
	out := make([]blueprint.Source, 0, len(sources))
	for _, s := range sources {
		if s.Provider != ref {
			out = append(out, s)
		}
	}
	return out
}

func contains(refs []string, ref string) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}

// declaresProvider reports whether b.Spec.Sources names ref.
func declaresProvider(b *blueprint.Blueprint, ref string) bool {
	for _, s := range b.Spec.Sources {
		if s.Provider == ref {
			return true
		}
	}
	return false
}

// replaceProvider swaps old for new in b: the spec.sources entry naming old
// becomes new, in place (dropped instead when new is already declared, so
// the document never lists a source twice), and every resource pinned to
// old re-points to new — the pins are what make a lone sources edit fail
// validation ("provider is not declared in spec.sources").
func replaceProvider(b *blueprint.Blueprint, old, new string) {
	already := declaresProvider(b, new)
	out := make([]blueprint.Source, 0, len(b.Spec.Sources))
	for _, s := range b.Spec.Sources {
		if s.Provider != old {
			out = append(out, s)
			continue
		}
		if !already {
			out = append(out, blueprint.Source{Provider: new})
			already = true
		}
	}
	b.Spec.Sources = out
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Provider == old {
			b.Spec.Resources[i].Provider = new
		}
	}
}
