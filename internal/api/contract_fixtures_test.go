// This file is a cross-language contract test: it checks that the JSON
// shapes this API serves have not silently drifted from the frontend's own
// fixtures for them.
//
// Before this test existed, that check was manual: someone had to notice
// that a Go json tag and a frontend fixture disagreed. This makes the same
// check build-breaking instead — DisallowUnknownFields turns "the fixture
// has a key Go doesn't know about" into a decode error, and the key-set
// comparison after re-marshaling turns "Go has a key the fixture doesn't"
// into a test failure, so a field renamed on only one side of the API
// contract fails CI rather than shipping as a silent mismatch the canvas
// discovers at runtime.
//
// Fix round 2: the check used to cover kinds.json and queue.fields.json
// only — two of the five fixtures, and the two whose Go types (index.Kind,
// index.Field) are the least likely to move on their own. The three that
// were missing are exactly the ones this milestone actually authored:
// queue.kind.json and generate.json have no named Go type at all (their
// handlers write map literals), and blueprint.json mirrors the document
// every editing route returns. All five are covered now, and the key-set
// comparison recurses, so nesting — a Kind inside the kind response, a
// parameter map inside the blueprint — is compared at every level rather
// than only at the top.
//
// web/ (the frontend, and therefore these fixtures) lives on the m3-canvas
// branch, not here on m2-schema-api — this branch predates the frontend
// entirely. So on this branch, fixturesDir does not exist and every test in
// this file skips with an explanatory message; that is expected, not a
// failure. The test activates on its own once this branch merges with
// m3-canvas and web/src/api/fixtures/*.json actually exist on disk.
package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/index"
)

// fixturesDir is where the frontend keeps its canned API fixtures, relative
// to this package (internal/api -> internal -> repo root -> web/...).
const fixturesDir = "../../web/src/api/fixtures"

// kindResponse mirrors GET /api/kinds/{apiVersion}/{kind}'s body, and
// generateResponse mirrors POST /api/generate's. Both handlers write a
// map[string]any literal rather than a struct, so there is no production
// type to point this test at; declaring the shape here is what gives those
// two routes the same drift protection the others get from their real types.
// If a handler's map keys and one of these change apart from each other, the
// fixture comparison below is what notices.
type kindResponse struct {
	Kind     index.Kind    `json:"kind"`
	Envelope []index.Field `json:"envelope"`
}

type generateResponse struct {
	Outputs []generateOutput `json:"outputs"`
	Written bool             `json:"written"`
}

func TestContractFixtureKindsRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "kinds.json"), &[]index.Kind{})
}

func TestContractFixtureQueueFieldsRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "queue.fields.json"), &[]index.Field{})
}

func TestContractFixtureQueueKindRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "queue.kind.json"), &kindResponse{})
}

func TestContractFixtureBlueprintRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "blueprint.json"), &blueprint.Blueprint{})
}

func TestContractFixtureGenerateRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "generate.json"), &generateResponse{})
}

// checkFixtureKeySetRoundTrips reads the JSON at path, decodes it into into
// (a pointer to whatever Go shape that route serves) with
// DisallowUnknownFields so an extra key on the fixture's side fails loudly,
// then re-marshals into and checks that the key set at every level of
// nesting survived the round trip unchanged — catching the opposite drift, a
// Go field the fixture does not have (e.g. one missing omitempty, or one
// renamed only in Go).
//
// It is shape-agnostic: a fixture may be a JSON array (kinds.json) or a JSON
// object (blueprint.json), and either may nest the other to any depth.
func checkFixtureKeySetRoundTrips(t *testing.T, path string, into any) {
	t.Helper()

	if _, err := os.Stat(fixturesDir); err != nil {
		t.Skipf("web/ is not present on this branch (%s: %v) — this contract test activates once "+
			"m2-schema-api merges with m3-canvas's frontend fixtures", fixturesDir, err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("%s: %v\n(a Go json struct tag has drifted from this frontend fixture — "+
			"the fixture has a key the Go type does not)", path, err)
	}

	reEncoded, err := json.Marshal(into)
	if err != nil {
		t.Fatalf("re-marshal %T decoded from %s: %v", into, path, err)
	}

	want := keySkeleton(t, path, raw)
	got := keySkeleton(t, path+" (re-encoded)", reEncoded)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s: key set changed across the Go round-trip (-fixture +go):\n%s", path, diff)
	}
}

// keySkeleton reduces raw JSON to its structure alone: every object becomes
// a map from its keys to their own skeletons, every array a list of element
// skeletons, and every scalar one shared placeholder. Comparing two
// skeletons therefore compares key sets at every level of nesting and
// nothing else — the fixture's values are illustrative data, not part of the
// contract, so they must not make this test fail.
func keySkeleton(t *testing.T, label string, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: not valid JSON: %v", label, err)
	}
	return skeletonOf(v)
}

// scalarPlaceholder stands in for every non-object, non-array value, so
// "region" vs "tags" or 3 vs 4 never registers as a difference — only a key
// appearing or disappearing does.
const scalarPlaceholder = "<scalar>"

func skeletonOf(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, elem := range t {
			out[k] = skeletonOf(elem)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, elem := range t {
			out[i] = skeletonOf(elem)
		}
		return out
	default:
		return scalarPlaceholder
	}
}
