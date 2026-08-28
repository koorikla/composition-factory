// This file implements the /api/providers routes: listing the providers the
// server is currently serving kinds from, and (next in this file's history:
// POST) adding one at runtime.
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
	"net/http"

	"github.com/koorikla/compositionfactory/internal/index"
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
