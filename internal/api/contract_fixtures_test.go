// This file is a cross-language contract test: it checks that
// internal/index's Kind and Field JSON shapes have not silently drifted
// from the frontend's own fixtures for them.
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
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/index"
)

// fixturesDir is where the frontend keeps its canned API fixtures, relative
// to this package (internal/api -> internal -> repo root -> web/...).
const fixturesDir = "../../web/src/api/fixtures"

func TestContractFixtureKindsRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "kinds.json"), &[]index.Kind{})
}

func TestContractFixtureQueueFieldsRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "queue.fields.json"), &[]index.Field{})
}

// checkFixtureKeySetRoundTrips reads the JSON array at path, decodes it into
// into (a pointer to a slice of index.Kind or index.Field) with
// DisallowUnknownFields so an extra key on the fixture's side fails loudly,
// then re-marshals into and checks that every object's key set survived the
// round trip unchanged — catching the opposite drift, a Go field the
// fixture does not have (e.g. one missing omitempty, or one renamed only in
// Go).
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

	wantObjs := decodeKeySets(t, path, raw)
	gotObjs := decodeKeySets(t, path+" (re-encoded)", reEncoded)
	if len(wantObjs) != len(gotObjs) {
		t.Fatalf("%s: %d objects in fixture, %d after decode+re-encode", path, len(wantObjs), len(gotObjs))
	}
	for i := range wantObjs {
		if diff := cmp.Diff(wantObjs[i], gotObjs[i]); diff != "" {
			t.Errorf("%s[%d]: key set changed across the Go round-trip (-fixture +go):\n%s", path, i, diff)
		}
	}
}

// decodeKeySets parses raw as a JSON array of objects and returns each
// object's sorted key set, for a content-independent, order-independent
// comparison of what was present vs. what came back out.
func decodeKeySets(t *testing.T, label string, raw []byte) [][]string {
	t.Helper()
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		t.Fatalf("%s: expected a JSON array of objects: %v", label, err)
	}
	out := make([][]string, len(objs))
	for i, obj := range objs {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out[i] = keys
	}
	return out
}
