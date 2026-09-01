// This file implements GET /api/catalogue: the provider/function discovery
// catalogue — a static, CI-refreshed index of installable crossplane-contrib
// packages (see the top-level catalogue package, scripts/build-catalogue and
// docs/catalogue.md for how it's built and why it has to be static: neither
// xpkg.upbound.io nor ghcr.io expose an anonymous _catalog listing, so "what
// providers exist" can't be answered per request the way this file's
// sibling providers.go answers "what is this server currently serving").
package api

import (
	"net/http"
	"strings"

	"github.com/koorikla/compositionfactory/catalogue"
)

// catalogueEntries and catalogueErr are resolved once, at package init, by
// parsing the embedded catalogue/providers.json — every request filters an
// already-parsed, already-validated (scripts/build-catalogue's own
// writeCatalogue validates before ever writing the file) slice, rather than
// re-decoding JSON per request. A non-nil catalogueErr — unreachable in
// practice, since catalogue.TestEmbeddedCatalogueIsValid gates exactly this
// at CI time — fails only the specific requests that need it with a 500,
// rather than a broken embed crashing server startup for routes that never
// touch the catalogue at all.
var catalogueEntries, catalogueErr = catalogue.Load()

// handleCatalogue serves GET /api/catalogue?q=: {"providers":[...]},
// optionally filtered to entries whose name or description contains q as a
// case-insensitive substring (the same style GET /api/kinds' q filter
// uses — see index.Search).
//
// This handler needs no server state — the catalogue is the same static
// list for every request, unlike every other route in this package — so
// unlike them it is a plain function, not a *server method; New registers
// it directly.
func handleCatalogue(w http.ResponseWriter, r *http.Request) {
	if catalogueErr != nil {
		writeJSONError(w, http.StatusInternalServerError, catalogueErr.Error())
		return
	}

	q := strings.ToLower(r.URL.Query().Get("q"))
	entries := make([]catalogue.Provider, 0, len(catalogueEntries))
	for _, e := range catalogueEntries {
		if q == "" || strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(strings.ToLower(e.Description), q) {
			entries = append(entries, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
}
