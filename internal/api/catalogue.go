// This file implements GET /api/catalogue: the provider/function discovery
// catalogue — a static, CI-refreshed index of installable crossplane-contrib
// packages (see the top-level catalogue package, scripts/build-catalogue and
// docs/catalogue.md for how it's built and why it has to be static: neither
// xpkg.upbound.io nor ghcr.io expose an anonymous _catalog listing, so "what
// providers exist" can't be answered per request the way this file's
// sibling providers.go answers "what is this server currently serving").
package api

import (
	"encoding/json"
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
//
// Unfiltered responses are additionally pre-marshaled, pre-hashed (ETag)
// and pre-compressed (gzip) to eliminate repeat runtime overhead entirely.
var (
	catalogueEntries, catalogueErr = catalogue.Load()
	unfilteredCatalogueMap         = map[string]any{"providers": catalogueEntries}
	unfilteredCatalogueJSON        []byte
	unfilteredCatalogueETag        string
	unfilteredCatalogueGzip        []byte
)

func init() {
	if catalogueErr == nil {
		if data, err := json.Marshal(unfilteredCatalogueMap); err == nil {
			unfilteredCatalogueJSON = data
			unfilteredCatalogueETag = etagFor(data)
			if gz, ok := gzipBytes(data); ok {
				unfilteredCatalogueGzip = gz
			}
		}
	}
}

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
	typ := strings.ToLower(r.URL.Query().Get("type")) // "function", "provider", or ""
	if q == "" && typ == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", unfilteredCatalogueETag)
		if acceptsGzip(r) && len(unfilteredCatalogueGzip) > 0 {
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(unfilteredCatalogueGzip)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(unfilteredCatalogueJSON)
		return
	}

	entries := make([]catalogue.Provider, 0, len(catalogueEntries))
	for _, e := range catalogueEntries {
		isFn := strings.HasPrefix(e.Name, "function-")
		if typ == "function" && !isFn {
			continue
		}
		if typ == "provider" && isFn {
			continue
		}
		if q == "" || catalogue.Matches(e, q) {
			entries = append(entries, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": entries})
}
