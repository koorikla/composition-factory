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
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// providerEntry is one provider in GET /api/providers' response — and the
// "provider" half of POST's. Kinds is a count, not the kinds themselves; the
// canvas fetches those from /api/kinds, which stays the one source for kind
// listings.
type providerEntry struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
	Kinds  int    `json:"kinds"`
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
func (srv *server) handleListProviders(w http.ResponseWriter, _ *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	entries, err := srv.providerEntriesLocked()
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
// never disagree on its shape. srv.mu must be held by the caller.
func (srv *server) providerEntriesLocked() ([]providerEntry, error) {
	counts := kindCountsByProvider(srv.Index)
	entries := make([]providerEntry, 0, len(srv.Providers))
	for _, ref := range srv.Providers {
		digest, err := srv.Store.LoadDigest(ref)
		if err != nil {
			return nil, err
		}
		entries = append(entries, providerEntry{Ref: ref, Digest: digest, Kinds: counts[ref]})
	}
	return entries, nil
}

// kindCountsByProvider counts idx's kinds per provider ref, one pass for the
// whole response rather than one index scan per provider.
func kindCountsByProvider(idx *index.Index) map[string]int {
	counts := make(map[string]int)
	for _, k := range idx.All() {
		counts[k.Provider]++
	}
	return counts
}

// addProviderRequest is the POST /api/providers body.
type addProviderRequest struct {
	Ref string `json:"ref"`
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

	for _, ref := range srv.Providers {
		if ref == req.Ref {
			writeJSONError(w, http.StatusConflict, fmt.Sprintf("provider %q is already cached", req.Ref))
			return
		}
	}

	fetch := srv.fetch
	if fetch == nil {
		fetch = func(ref string) (*xpkg.Package, error) {
			return xpkg.Fetch(r.Context(), ref)
		}
	}
	pkg, err := fetch(req.Ref)
	if err != nil {
		// Verbatim: the fetch error names the registry's own reason (DNS,
		// auth, a missing tag), and paraphrasing it would throw that away.
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	crds, err := schema.ParseCRDs(pkg.Docs)
	if err != nil {
		// The pull succeeded but the package's own content is bad — still
		// the upstream's fault, wrapped with the ref exactly the way
		// ProviderAddCmd reports the same failure.
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s: %v", req.Ref, err))
		return
	}

	l, err := cache.ReadLock(srv.Lock)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	l.Set(req.Ref, pkg.Digest)
	if err := l.Write(srv.Lock); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := srv.Store.Save(pkg, crds); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Rebuild over the existing providers' CACHED schemas plus the CRDs just
	// fetched — the same single-load discipline cmd/cf/serve.go's startup
	// enforces: the index and the store must describe the same bytes, so the
	// existing refs are re-read from the store they were saved to, and the
	// new ref uses the exact CRDs Save just persisted.
	byProvider := make(map[string][]schema.CRD, len(srv.Providers)+2)
	for _, ref := range srv.Providers {
		existing, err := srv.Store.Load(ref)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		byProvider[ref] = existing
	}
	byProvider[req.Ref] = crds
	// The vendored native kinds are indexed under their own label, exactly
	// as cmd/cf/serve.go's startup build does — without this line the first
	// provider add would rebuild an index with every native kind silently
	// gone from /api/kinds. They are not part of srv.Providers (that list is
	// xpkg refs with digests to pin and cache entries to count; native kinds
	// have neither — their pin is the vendored Kubernetes version).
	native, err := k8s.Kinds()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byProvider[blueprint.NativeProvider] = native

	idx, err := index.Build(byProvider)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The atomic step the whole handler exists for: index and provider set
	// swap together, under the same mu the duplicate check ran under.
	srv.Index = idx
	srv.Providers = append(srv.Providers, req.Ref)

	added := []index.Kind{}
	for _, k := range idx.All() {
		if k.Provider == req.Ref {
			added = append(added, k)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": providerEntry{Ref: req.Ref, Digest: pkg.Digest, Kinds: len(added)},
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
// GET serves. 404 is a ref the server does not hold; 409 is a ref the
// blueprint still references — from spec.sources or any resource's provider —
// with the message naming every referencer, the same refuse-and-name
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

	held := false
	for _, p := range srv.Providers {
		if p == ref {
			held = true
			break
		}
	}
	if !held {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("provider not found: %q", ref))
		return
	}

	// The referencer check reads the blueprint from disk, like every handler
	// that consults it — the file is the source of truth, and a copy held
	// since some earlier request could miss a source or resource added since.
	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	if msg := providerReferencers(b, ref); msg != "" {
		writeJSONError(w, http.StatusConflict, msg)
		return
	}

	// Rebuild over the remaining providers' cached schemas — the same
	// single-load discipline handleAddProvider follows: the index and the
	// store must describe the same bytes, so every surviving ref is re-read
	// from the store it was saved to.
	remaining := make([]string, 0, len(srv.Providers)-1)
	byProvider := make(map[string][]schema.CRD, len(srv.Providers)-1)
	for _, p := range srv.Providers {
		if p == ref {
			continue
		}
		crds, err := srv.Store.Load(p)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		byProvider[p] = crds
		remaining = append(remaining, p)
	}
	idx, err := index.Build(byProvider)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := srv.Store.Delete(ref); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	l, err := cache.ReadLock(srv.Lock)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if l.Remove(ref) { // a ref never pinned (or pinned elsewhere) is fine; don't rewrite for a no-op
		if err := l.Write(srv.Lock); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// The atomic step: index and provider set swap together, under the same
	// mu every reader snapshots them under.
	srv.Index = idx
	srv.Providers = remaining

	entries, err := srv.providerEntriesLocked()
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

	var parts []string
	if inSources {
		parts = append(parts, "the blueprint's sources")
	}
	if len(resources) > 0 {
		parts = append(parts, "resources "+strings.Join(resources, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("delete provider %q: still referenced by %s", ref, strings.Join(parts, " and by "))
}
