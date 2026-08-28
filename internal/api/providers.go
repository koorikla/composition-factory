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

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
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
// The provider set and the index are snapshotted under srv.mu as one
// consistent pair — POST /api/providers swaps both together under the same
// lock, so this handler can never observe a ref whose kinds are not yet (or
// no longer) in the index it counts against. The digests are then read from
// the store outside the lock: cache entries are written before a ref ever
// enters srv.Providers, and nothing deletes them.
func (srv *server) handleListProviders(w http.ResponseWriter, _ *http.Request) {
	srv.mu.Lock()
	refs := append([]string(nil), srv.Providers...)
	idx := srv.Index
	srv.mu.Unlock()

	counts := kindCountsByProvider(idx)
	entries := make([]providerEntry, 0, len(refs))
	for _, ref := range refs {
		digest, err := srv.Store.LoadDigest(ref)
		if err != nil {
			// The server's own cache no longer holds a provider it was
			// started with (or added) — its fixed environment is broken, not
			// the caller's request. The store's error already names the exact
			// `cf provider add` command that repairs it; surface it verbatim.
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entries = append(entries, providerEntry{Ref: ref, Digest: digest, Kinds: counts[ref]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
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
	byProvider := make(map[string][]schema.CRD, len(srv.Providers)+1)
	for _, ref := range srv.Providers {
		existing, err := srv.Store.Load(ref)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		byProvider[ref] = existing
	}
	byProvider[req.Ref] = crds

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
