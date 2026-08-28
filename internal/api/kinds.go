// This file implements the /api/kinds routes: search over the in-memory
// index, and lazy per-kind field fetch (envelope + forProvider fields) so a
// caller never has to load every provider's full schema up front.
package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/koorikla/compositionfactory/internal/index"
)

// handleKinds serves GET /api/kinds?q=&limit=. With no q, index.Search("",
// limit) still does the right thing: strings.Contains(s, "") is true for
// every s, so an empty query matches every Kind and this collapses to "all
// kinds, optionally capped at limit" without a separate branch for the
// no-query case.
func (srv *server) handleKinds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, err := parseIntParam(q, "limit")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	kinds := srv.Index.Search(q.Get("q"), limit)
	if kinds == nil {
		kinds = []index.Kind{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"kinds": kinds})
}

// handleKind serves GET /api/kinds/{apiVersion}/{kind}: the identity/summary
// Kind plus its envelope (spec fields outside forProvider/initProvider —
// status, writeConnectionSecretToRef and friends, whichever of those the
// CRD's own schema actually has).
//
// The envelope is always computed via index.Fields(crd.Envelope(),
// FieldQuery{}) — never a hand-written field list. A hand-written list is
// exactly what went wrong before: without this route, an earlier version of
// the frontend guessed the legacy v1 envelope shape (deletionPolicy,
// publishConnectionDetailsTo, writeConnectionSecretToRef.namespace), fields
// the API server silently prunes on v2 namespaced resources. Envelope()
// reads the real CRD, so a namespaced Queue and a cluster-scoped Queue each
// get the envelope their own schema actually has.
func (srv *server) handleKind(w http.ResponseWriter, r *http.Request) {
	apiVersion, err := pathAPIVersion(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	kindName := r.PathValue("kind")

	// LookupKind, not a separate Lookup+scan-All(): both halves of the
	// response must describe the same provider's resource, which only
	// holds if they are resolved as one consistent choice — see
	// index.LookupKind's doc comment for the collision this avoids.
	kind, crd, ok := srv.Index.LookupKind(apiVersion, kindName)
	if !ok {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("kind not found: %s %s", apiVersion, kindName))
		return
	}

	// A CRD version with no schema block at all (upjet ships some
	// non-storage versions this way) makes Envelope's underlying
	// specProperties lookup fail. That is a legitimate "nothing to report"
	// case, not a server error — index.Build treats an equivalent
	// ForProvider failure as zero fields for the same reason, and Envelope
	// mirrors that here so a schema-light CRD still returns 200 with an
	// empty envelope rather than a 500.
	nodes, err := crd.Envelope()
	if err != nil {
		nodes = nil
	}
	envelope := index.Fields(nodes, index.FieldQuery{})
	if envelope == nil {
		envelope = []index.Field{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "envelope": envelope})
}

// handleKindFields serves GET
// /api/kinds/{apiVersion}/{kind}/fields?required_only=&max_depth=&prefix=&q=&limit=:
// the forProvider fields for one kind, filtered by index.FieldQuery.
func (srv *server) handleKindFields(w http.ResponseWriter, r *http.Request) {
	apiVersion, err := pathAPIVersion(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	kindName := r.PathValue("kind")

	crd, ok := srv.Index.Lookup(apiVersion, kindName)
	if !ok {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("kind not found: %s %s", apiVersion, kindName))
		return
	}

	fq, err := parseFieldQuery(r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// See handleKind: a CRD version with no schema block makes ForProvider
	// fail; that is zero fields, not a server error (index.Build treats it
	// identically when building Kind.Fields/Kind.Required).
	nodes, err := crd.ForProvider()
	if err != nil {
		nodes = nil
	}

	// total must count the filtered set BEFORE limit truncates it, so a
	// caller can tell limit cut the response short (total > len(fields)).
	// index.Fields applies Limit as its own last filter step internally, so
	// computing fields := index.Fields(nodes, fq) and then total :=
	// len(fields) would make total tautologically equal len(fields) —
	// exactly the bug this comment is here to not reintroduce. Run the same
	// query with Limit zeroed to get the unlimited filtered set once, then
	// slice it ourselves: one filtering pass, not two.
	fqUnlimited := fq
	fqUnlimited.Limit = 0
	fields := index.Fields(nodes, fqUnlimited)
	total := len(fields)
	if fq.Limit > 0 && len(fields) > fq.Limit {
		fields = fields[:fq.Limit]
	}
	if fields == nil {
		fields = []index.Field{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"fields": fields, "total": total})
}

// pathAPIVersion extracts and unescapes the {apiVersion} path wildcard.
//
// Go's ServeMux (1.22+) already matches "{apiVersion}/{kind}" against the
// request's escaped path form, so a literal "%2F" inside the apiVersion
// segment is treated as part of that segment rather than a separator, and
// r.PathValue("apiVersion") comes back already unescaped — verified
// directly: PathValue on a route registered for
// "/api/kinds/{apiVersion}/{kind}" returns "sqs.aws.m.upbound.io/v1beta1"
// (with a real slash) for a request path containing
// "sqs.aws.m.upbound.io%2Fv1beta1", with no extra decoding step. A request
// whose path fails to unescape at all (an invalid "%" sequence) never
// reaches this handler in the first place — net/http's own request-line
// parser rejects it with a plain 400 before the mux runs.
//
// The explicit url.PathUnescape call below is therefore defense in depth,
// not the thing making the happy path work: it guards against the
// (currently unreachable in practice) case of PathValue ever returning a
// still-escaped segment, turning that into the same 400-naming-the-parameter
// shape as every other bad input here, rather than an accidental panic or
// mismatch further down.
func pathAPIVersion(r *http.Request) (string, error) {
	raw := r.PathValue("apiVersion")
	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid apiVersion: %q", raw)
	}
	return unescaped, nil
}

// parseFieldQuery builds an index.FieldQuery from query parameters,
// rejecting any that fail to parse rather than silently coercing them to a
// default. A dropped max_depth or limit would return a different field set
// than the caller asked for with no indication anything was ignored — the
// same silent-wrongness class this project exists to avoid.
func parseFieldQuery(q url.Values) (index.FieldQuery, error) {
	var fq index.FieldQuery
	var err error

	if fq.RequiredOnly, err = parseBoolParam(q, "required_only"); err != nil {
		return index.FieldQuery{}, err
	}
	if fq.MaxDepth, err = parseIntParam(q, "max_depth"); err != nil {
		return index.FieldQuery{}, err
	}
	if fq.Limit, err = parseIntParam(q, "limit"); err != nil {
		return index.FieldQuery{}, err
	}
	fq.Prefix = q.Get("prefix")
	fq.Search = q.Get("q")
	return fq, nil
}

// parseIntParam parses query parameter name as an int, returning 0 (the
// zero value every FieldQuery int field treats as "unlimited") when the
// parameter is absent. A negative integer parses successfully and is passed
// through as-is — FieldQuery.Limit and index.Search's limit both document a
// negative value as "fine, means unlimited" — but a value that is not an
// integer at all (limit=-x, max_depth=abc) is rejected outright rather than
// silently becoming 0.
func parseIntParam(q url.Values, name string) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %q", name, raw)
	}
	return v, nil
}

// parseBoolParam parses query parameter name as a bool (accepting the same
// spellings as strconv.ParseBool: 1/t/T/TRUE/true/True and
// 0/f/F/FALSE/false/False), returning false when the parameter is absent.
func parseBoolParam(q url.Values, name string) (bool, error) {
	raw := q.Get(name)
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %q", name, raw)
	}
	return v, nil
}
