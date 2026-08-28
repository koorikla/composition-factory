package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestGetBlueprintReturnsJSON(t *testing.T) {
	rec := do(t, testHandler(t), "GET", "/api/blueprint", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var b blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if b.Spec.XRD.Kind == "" {
		t.Error("blueprint came back empty")
	}
}

// The file on disk is the source of truth: an edit that is not persisted
// would diverge from what `cf gen` reads.
func TestAddParameterPersistsToDisk(t *testing.T) {
	h, path := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"location","parameter":{"type":"string","required":true,"enum":["EU","US"]}}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint on disk no longer loads: %v", err)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["location"]; !ok {
		t.Error("parameter was not persisted to the file")
	}
}

func TestInvalidEditIs400AndLeavesTheFileUntouched(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, _ := os.ReadFile(path)
	rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"not a valid name","parameter":{"type":"string"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a valid name") {
		t.Errorf("error does not name the offending input: %s", rec.Body)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a rejected edit")
	}
}

func TestRenameRewritesReferencesOnDisk(t *testing.T) {
	h, path := testHandlerWithPath(t)
	if rec := do(t, h, "POST", "/api/blueprint/parameters/maxMessageSize/rename",
		`{"to":"maxBytes"}`); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Spec.Resources[0].Fields["maxMessageSize"].From; got != "params.maxBytes" {
		t.Errorf("reference on disk = %q, want params.maxBytes", got)
	}
}

func TestDeleteReferencedParameterIs409(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	rec := do(t, h, "DELETE", "/api/blueprint/parameters/maxMessageSize", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 — the parameter is still referenced, which is a "+
			"conflict with current state rather than a malformed request", rec.Code)
	}
}

func TestMalformedJSONBodyIs400(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	if rec := do(t, h, "POST", "/api/blueprint/parameters", `{"name":`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The whole architecture rests on this: the API must not have its own emitter.
//
// Fix round 2, Critical finding: this test could not fail for what it is
// named. It asserted only that write:false came back with three outputs and
// did not report Written — both true of any handler that returns three
// plausible-looking paths, including one rendering its own bytes with a
// private emitter, which is the single failure this test exists to catch. It
// never sent write:true, never touched the output tree, and compared no
// bytes at all (a stray `_ = path` was the fossil of the missing
// comparison).
//
// It now calls emit.Generate — the one entry point cf gen also goes through
// — directly, with the same three inputs the handler resolves for itself
// (the same blueprint file, the CRDs from the same seeded cache.Store, the
// same OutDir), then drives both modes over HTTP: write:false must report
// exactly the engine's paths and sizes while leaving the output tree
// untouched, and write:true must leave files on disk that are byte-for-byte
// the engine's own output.
func TestGenerateProducesTheSameBytesAsTheEngine(t *testing.T) {
	h, path, store, outDir := testServerParts(t)

	// The engine's own answer for these exact inputs. The CRDs are resolved
	// the way handleGenerate's loadSourceCRDs resolves them — Store.Load for
	// every provider in spec.sources, in order — so "the same CRDs the
	// handler uses" is literal here rather than a re-derivation that could
	// quietly differ.
	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("load the blueprint fixture: %v", err)
	}
	var crds []schema.CRD
	for _, s := range b.Spec.Sources {
		got, err := store.Load(s.Provider)
		if err != nil {
			t.Fatalf("load provider %q from the seeded cache: %v", s.Provider, err)
		}
		crds = append(crds, got...)
	}
	want, err := emit.Generate(b, crds, outDir)
	if err != nil {
		t.Fatalf("emit.Generate: %v", err)
	}
	if len(want) != 3 {
		t.Fatalf("the engine produced %d outputs, want 3 (composition, functions.yaml, xrd) — "+
			"this test's premise is broken, not the server", len(want))
	}

	type outputSummary struct {
		Path  string `json:"path"`
		Bytes int    `json:"bytes"`
	}
	generate := func(body string) ([]outputSummary, bool) {
		t.Helper()
		rec := do(t, h, "POST", "/api/generate", body)
		if rec.Code != 200 {
			t.Fatalf("POST /api/generate %s: status %d: %s", body, rec.Code, rec.Body)
		}
		var got struct {
			Outputs []outputSummary `json:"outputs"`
			Written bool            `json:"written"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("not JSON: %v (%s)", err, rec.Body)
		}
		if len(got.Outputs) != len(want) {
			t.Fatalf("got %d outputs, want %d (composition, functions.yaml, xrd)", len(got.Outputs), len(want))
		}
		return got.Outputs, got.Written
	}

	// write:false — a preview: same paths, same sizes, nothing on disk.
	outputs, written := generate(`{"write":false}`)
	if written {
		t.Error("write:false still reported Written")
	}
	for i, out := range outputs {
		if out.Path != want[i].Path {
			t.Errorf("output %d path = %q, engine says %q", i, out.Path, want[i].Path)
		}
		if out.Bytes != len(want[i].Body) {
			t.Errorf("%s: reported %d bytes, engine produced %d", out.Path, out.Bytes, len(want[i].Body))
		}
		if _, err := os.Stat(out.Path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("write:false put %s on disk (stat err = %v); a preview must not touch the output tree",
				out.Path, err)
		}
	}

	// write:true — the bytes on disk must be the engine's, exactly.
	outputs, written = generate(`{"write":true}`)
	if !written {
		t.Error("write:true did not report Written")
	}
	for i, out := range outputs {
		if out.Path != want[i].Path {
			t.Errorf("output %d path = %q, engine says %q", i, out.Path, want[i].Path)
		}
		onDisk, err := os.ReadFile(out.Path)
		if err != nil {
			t.Fatalf("write:true reported %s but it is not readable: %v", out.Path, err)
		}
		if !bytes.Equal(onDisk, want[i].Body) {
			t.Errorf("%s is NOT what the engine produced (%d bytes written, %d from the engine) — "+
				"the API has grown its own emitter, which is the one thing this architecture forbids\n"+
				"--- emit.Generate ---\n%s\n--- written by the server ---\n%s",
				out.Path, len(onDisk), len(want[i].Body), want[i].Body, onDisk)
		}
		if out.Bytes != len(onDisk) {
			t.Errorf("%s: reported %d bytes, wrote %d", out.Path, out.Bytes, len(onDisk))
		}
	}
}

// TestConcurrentAddsDoNotLoseEdits is fix round 2's lost-update regression.
//
// Every mutating handler does load -> edit -> persist against the file on
// disk. With nothing serializing that sequence, two concurrent requests both
// read the same starting document, each applied its own edit to its own copy,
// and whichever wrote second silently replaced the first: both callers were
// told 200, and one of the two edits simply did not exist afterwards. That is
// the worst shape a bug can take here — the API reports success for work it
// threw away.
//
// Eight concurrent adds of distinct names, released together so they overlap
// rather than queue: every one must report 200 AND be on disk at the end.
// Checking the responses alone would not catch this at all — the pre-fix
// server answered 200 to all eight while dropping most of them.
func TestConcurrentAddsDoNotLoseEdits(t *testing.T) {
	h, path := testHandlerWithPath(t)

	const n = 8
	name := func(i int) string { return fmt.Sprintf("param%d", i) }

	codes := make([]int, n)
	bodies := make([]string, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/blueprint/parameters",
				strings.NewReader(fmt.Sprintf(`{"name":%q,"parameter":{"type":"string"}}`, name(i))))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			<-start // release all eight at once
			h.ServeHTTP(rec, req)
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("add %s: status %d: %s", name(i), code, bodies[i])
		}
	}

	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint on disk no longer loads after %d concurrent edits: %v", n, err)
	}
	var missing []string
	for i := range n {
		if _, ok := reloaded.Spec.XRD.Parameters[name(i)]; !ok {
			missing = append(missing, name(i))
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d of %d concurrent edits were reported as successful but are not on disk: %v — "+
			"load/edit/persist is not atomic, so a later write is overwriting an earlier one",
			len(missing), n, missing)
	}
}

func TestGenerateSurfacesValidationErrorsAsIs(t *testing.T) {
	h, path := testHandlerWithPath(t)
	// Corrupt the blueprint on disk behind the server's back.
	body, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(body, []byte("scope: Namespaced"), []byte("scope: Cluster"), 1), 0o644)
	rec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Cluster") {
		t.Errorf("the engine's own error was not surfaced: %s", rec.Body)
	}
}

// --- Additional coverage beyond the brief's verbatim tests ---
//
// Not from the brief's Step 1 listing. The brief's prose (distinct from its
// verbatim tests) requires persistence to be byte-stable: "two consecutive
// identical edits produce identical bytes." None of the tests above compare
// raw bytes across two independent runs of the same edit, so this pins that
// requirement directly: the same POST against two freshly-built, identical
// starting blueprints must leave byte-for-byte identical files on disk —
// not just semantically-equal YAML that happens to format differently run
// to run.
func TestConsecutiveIdenticalEditsProduceIdenticalBytes(t *testing.T) {
	h1, path1 := testHandlerWithPath(t)
	h2, path2 := testHandlerWithPath(t)

	const editBody = `{"name":"location","parameter":{"type":"string","required":true,"enum":["EU","US"]}}`
	for _, run := range []struct {
		h    http.Handler
		path string
	}{{h1, path1}, {h2, path2}} {
		if rec := do(t, run.h, "POST", "/api/blueprint/parameters", editBody); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}

	got1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read %s: %v", path1, err)
	}
	got2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read %s: %v", path2, err)
	}
	if !bytes.Equal(got1, got2) {
		t.Errorf("two independent runs of the identical edit produced different bytes:\n"+
			"--- run 1 (%s) ---\n%s\n--- run 2 (%s) ---\n%s", path1, got1, path2, got2)
	}
}

// --- Fix round 1 ---
//
// Finding 1 (Important): loadBlueprint must distinguish "the blueprint file
// itself could not be read" (the server's own fixed path/environment is
// wrong — a 500) from "the file was read fine but its content does not
// parse or validate" (a data problem — a 400, the previous behaviour for
// every case). Neither of the two tests below existed before this round.
//
// Finding 2 (Minor): three spec-mandated behaviours the reviewer verified
// live had no committed regression coverage — duplicate add (409), PUT on
// an unknown parameter (404), and rename-to-self as a no-op success (200).
// Added below as straightforward regressions.

// TestBlueprintReadFailureIs500 points Options.Blueprint at a directory
// instead of a file, so blueprint.Load's own os.ReadFile fails with an I/O
// error (confirmed on this platform: "read <dir>: is a directory") rather
// than a parse or validation error. That is the server's fixed blueprint
// path being wrong, not a problem with any document's content, so it must
// be reported as 500 — and the body must still carry the underlying error
// verbatim, the same as every other error path in this API.
func TestBlueprintReadFailureIs500(t *testing.T) {
	dir := t.TempDir() // a directory, not a blueprint file
	h, err := New(Options{
		Index:     testIndex(t),
		Store:     cache.New(t.TempDir()),
		Blueprint: dir,
		OutDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := do(t, h, "GET", "/api/blueprint", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — the blueprint path itself is unreadable, "+
			"which is this server's own fault, not a malformed request or bad document: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), dir) || !strings.Contains(rec.Body.String(), "directory") {
		t.Errorf("error does not carry the underlying read error verbatim: %s", rec.Body)
	}
}

// TestBlueprintParseFailureIsStill400 is the counterpart to the 500 test
// above: a blueprint file that reads fine but is not even syntactically
// valid YAML must still be a 400 (a data problem), not a 500 — the read
// itself succeeded, only the content is bad.
func TestBlueprintParseFailureIsStill400(t *testing.T) {
	h, path := testHandlerWithPath(t)
	if err := os.WriteFile(path, []byte("apiVersion: [\nkind: Blueprint\n"), 0o644); err != nil {
		t.Fatalf("corrupt blueprint: %v", err)
	}

	rec := do(t, h, "GET", "/api/blueprint", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — the file read fine, only its YAML content is broken: %s",
			rec.Code, rec.Body)
	}
}

// TestAddDuplicateParameterIs409 regresses the spec-mandated 409 for adding
// a parameter name that is already declared (maxMessageSize, from the
// fixture blueprint).
func TestAddDuplicateParameterIs409(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"maxMessageSize","parameter":{"type":"integer"}}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a duplicate parameter name: %s", rec.Code, rec.Body)
	}
}

// TestSetUnknownParameterIs404 regresses the spec-mandated 404 for PUT
// against a parameter name that is not declared.
func TestSetUnknownParameterIs404(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	rec := do(t, h, "PUT", "/api/blueprint/parameters/doesNotExist",
		`{"parameter":{"type":"string"}}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for an unknown parameter: %s", rec.Code, rec.Body)
	}
}

// TestRenameToSelfIsNoOpSuccess regresses to==from being a 200 no-op rather
// than a collision error — RenameParameter's own documented behaviour,
// deliberately not special-cased in the HTTP layer (see
// handleRenameParameter's doc comment).
func TestRenameToSelfIsNoOpSuccess(t *testing.T) {
	h, path := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/blueprint/parameters/maxMessageSize/rename", `{"to":"maxMessageSize"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a rename-to-self no-op: %s", rec.Code, rec.Body)
	}
	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("maxMessageSize no longer declared after a rename-to-self")
	}
}
