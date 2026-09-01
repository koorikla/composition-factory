package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
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
		Body  string `json:"body"`
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

	// write:false — a preview: same paths, same sizes, nothing on disk, and
	// (additive contract change) the response now carries each output's full
	// rendered body so the canvas can render it without a write.
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
		if out.Body == "" {
			t.Errorf("%s: body is empty — write:false must still return the rendered content", out.Path)
		}
		if out.Bytes != len(out.Body) {
			t.Errorf("%s: bytes = %d, but len(body) = %d", out.Path, out.Bytes, len(out.Body))
		}
		// The load-bearing assertion: the body is byte-for-byte what
		// emit.Generate itself produced for this output, not a
		// re-derivation the handler computed on its own.
		if !bytes.Equal([]byte(out.Body), want[i].Body) {
			t.Errorf("%s: body does NOT match emit.Generate's own output byte-for-byte\n"+
				"--- emit.Generate ---\n%s\n--- response body ---\n%s",
				out.Path, want[i].Body, out.Body)
		}
	}

	// write:true — the bytes on disk must be the engine's, exactly, and the
	// response body must match too (a write does not change what a preview
	// would have shown).
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
		if out.Body == "" {
			t.Errorf("%s: body is empty — write:true must return the rendered content too", out.Path)
		}
		if out.Bytes != len(out.Body) {
			t.Errorf("%s: bytes = %d, but len(body) = %d", out.Path, out.Bytes, len(out.Body))
		}
		if !bytes.Equal([]byte(out.Body), want[i].Body) {
			t.Errorf("%s: body does NOT match emit.Generate's own output byte-for-byte\n"+
				"--- emit.Generate ---\n%s\n--- response body ---\n%s",
				out.Path, want[i].Body, out.Body)
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

// TestPartialParameterUpdateIsRefusedRatherThanSilentlyDestructive is fix
// round 2's PUT regression.
//
// PUT is replace-only, but blueprint.Parameter has no omitempty and JSON
// decoding cannot tell an absent key from a zero value, so
// `{"parameter":{"type":"string"}}` against a required parameter with an enum
// used to return 200 and quietly persist Required:false, Enum:nil — a
// destructive edit dressed as a partial one, with nothing in the request or
// the response hinting at the loss.
//
// Replace-only stays (a merge/patch would be a second, divergent edit model
// on the same route); what changes is that the destructive case has to be
// asked for explicitly. The three cases below are the whole contract: a body
// that would drop a live value is refused and names what it would have
// dropped; a complete body is accepted; and clearing a value still works when
// the caller says so with a zero value.
func TestPartialParameterUpdateIsRefusedRatherThanSilentlyDestructive(t *testing.T) {
	h, path := testHandlerWithPath(t)

	// A parameter with something to lose.
	if rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"location","parameter":{"type":"string","required":true,"enum":["EU","US"],"description":"where it runs"}}`,
	); rec.Code != http.StatusOK {
		t.Fatalf("seed: status %d: %s", rec.Code, rec.Body)
	}

	// A partial body: 400, naming every key it would have discarded.
	rec := do(t, h, "PUT", "/api/blueprint/parameters/location", `{"parameter":{"type":"string"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — this body would have dropped required, enum and description "+
			"without saying so: %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"required", "enum", "description"} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("the refusal does not name the omitted %q key, so the caller cannot tell what to "+
				"send: %s", key, rec.Body)
		}
	}
	if strings.Contains(rec.Body.String(), "default") {
		t.Errorf("the refusal names \"default\", which is not set on this parameter and so would not "+
			"have been discarded: %s", rec.Body)
	}

	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p := reloaded.Spec.XRD.Parameters["location"]; !p.Required || len(p.Enum) != 2 {
		t.Errorf("the refused PUT still changed the file: required=%v enum=%v", p.Required, p.Enum)
	}

	// A complete body: 200, and the change lands.
	rec = do(t, h, "PUT", "/api/blueprint/parameters/location",
		`{"parameter":{"type":"string","required":true,"enum":["EU","US","APAC"],"default":"EU","description":"where it runs"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a body carrying every key: %s", rec.Code, rec.Body)
	}
	reloaded, err = blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p := reloaded.Spec.XRD.Parameters["location"]; len(p.Enum) != 3 || p.Default != "EU" {
		t.Errorf("the accepted PUT did not land: enum=%v default=%q", p.Enum, p.Default)
	}

	// Clearing is still possible — it just has to be explicit.
	rec = do(t, h, "PUT", "/api/blueprint/parameters/location",
		`{"parameter":{"type":"string","required":false,"enum":null,"default":"","description":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — zero values sent explicitly are how a caller clears a "+
			"field: %s", rec.Code, rec.Body)
	}
	reloaded, err = blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p := reloaded.Spec.XRD.Parameters["location"]; p.Required || len(p.Enum) != 0 || p.Default != "" {
		t.Errorf("an explicit clear did not clear: %+v", p)
	}
}

// TestSetParameterRejectsUnknownKeys pins that holding the parameter as
// json.RawMessage (so key presence survives decoding) did not cost the
// unknown-field check: a typo inside the parameter object must still be a
// 400, not a silently ignored key.
func TestSetParameterRejectsUnknownKeys(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	rec := do(t, h, "PUT", "/api/blueprint/parameters/maxMessageSize",
		`{"parameter":{"type":"integer","requird":true,"enum":null,"default":"","description":""}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for the misspelled \"requird\" key: %s", rec.Code, rec.Body)
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
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
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

// --- PUT /api/blueprint: full-document replace ---
//
// The canvas keeps its whole document client-side and has no per-field edit
// to make against the narrower parameter routes above; it PUTs its entire
// document and follows up with POST /api/generate. Unlike every handler
// above, there is no load step (see handlePutBlueprint's doc comment), so
// these tests pin decode -> validate -> persist directly rather than
// load -> edit -> persist.

// mustLoadBlueprint is a small t.Helper wrapper so the tests below can load
// the fixture currently on disk without repeating the same three-line error
// check at every call site.
func mustLoadBlueprint(t *testing.T, path string) *blueprint.Blueprint {
	t.Helper()
	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return b
}

// TestPutBlueprintReplacesTheWholeDocument is the happy path: a full,
// modified document PUT to the server comes back 200 with the persisted
// document, lands on disk, and GET afterwards agrees with what PUT reported.
func TestPutBlueprintReplacesTheWholeDocument(t *testing.T) {
	h, path := testHandlerWithPath(t)

	current := mustLoadBlueprint(t, path)
	updated := *current
	updated.Metadata.Name = "xqueue-v2"
	updated.Spec.XRD.Parameters = make(map[string]blueprint.Parameter, len(current.Spec.XRD.Parameters)+1)
	for k, v := range current.Spec.XRD.Parameters {
		updated.Spec.XRD.Parameters[k] = v
	}
	updated.Spec.XRD.Parameters["location"] = blueprint.Parameter{
		Type: "string", Required: true, Enum: []string{"EU", "US"},
	}
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var got blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if got.Metadata.Name != "xqueue-v2" {
		t.Errorf("response metadata.name = %q, want xqueue-v2", got.Metadata.Name)
	}
	if _, ok := got.Spec.XRD.Parameters["location"]; !ok {
		t.Error("response does not carry the new parameter")
	}

	reloaded := mustLoadBlueprint(t, path)
	if reloaded.Metadata.Name != "xqueue-v2" {
		t.Errorf("persisted metadata.name = %q, want xqueue-v2", reloaded.Metadata.Name)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["location"]; !ok {
		t.Error("new parameter was not persisted to disk")
	}

	// GET must agree with what PUT just persisted -- a round trip, not just
	// "the file changed somehow".
	getRec := do(t, h, "GET", "/api/blueprint", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", getRec.Code, getRec.Body)
	}
	var fromGet blueprint.Blueprint
	if err := json.Unmarshal(getRec.Body.Bytes(), &fromGet); err != nil {
		t.Fatalf("GET response not JSON: %v", err)
	}
	if diff := cmp.Diff(got, fromGet); diff != "" {
		t.Errorf("GET disagrees with what PUT persisted (-PUT +GET):\n%s", diff)
	}
}

// TestPutBlueprintInvalidDocumentIs400VerbatimAndFileUntouched sends a
// document that decodes fine but fails Blueprint.Validate (scope: Cluster,
// unsupported in M1 -- see internal/blueprint/load.go). It must come back
// 400 carrying Validate's own error text VERBATIM (not paraphrased, not
// wrapped), and the file on disk must be byte-identical to before the
// request -- reject-without-write, the same guarantee every other edit
// route in this file gives.
func TestPutBlueprintInvalidDocumentIs400VerbatimAndFileUntouched(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	current := mustLoadBlueprint(t, path)
	invalid := *current
	invalid.Spec.XRD.Scope = "Cluster"
	wantErr := invalid.Validate()
	if wantErr == nil {
		t.Fatal("test setup: a Cluster-scoped document was expected to fail Validate")
	}
	body, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}

	var errBody errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error response not JSON: %v (%s)", err, rec.Body)
	}
	if errBody.Error != wantErr.Error() {
		t.Errorf("error body = %q, want the engine's Validate error verbatim: %q", errBody.Error, wantErr.Error())
	}
	if !strings.Contains(errBody.Error, "Cluster") {
		t.Errorf("error does not name the offending scope: %s", errBody.Error)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after rejected PUT: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a rejected PUT")
	}
}

// TestPutBlueprintMalformedJSONIs400 is the malformed-body counterpart: a
// body that does not even parse as JSON must be a 400, and must not touch
// the file (there is nothing to validate yet, so no write is even attempted).
func TestPutBlueprintMalformedJSONIs400(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", `{"apiVersion":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after malformed PUT: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a malformed PUT body")
	}
}

// TestPutBlueprintRejectsUnknownTopLevelKeys pins the choice noted in
// handlePutBlueprint's doc comment: this route reuses decodeJSON, the same
// DisallowUnknownFields helper every other handler in this file uses for its
// request body, so an unrecognized top-level key is a 400 here exactly as it
// would be for a typo'd key in any parameter route's body -- not a stricter,
// bespoke rule invented for this one route.
func TestPutBlueprintRejectsUnknownTopLevelKeys(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	current := mustLoadBlueprint(t, path)
	body, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	raw["notAField"] = json.RawMessage(`true`)
	withExtra, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal with extra key: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(withExtra))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown top-level key: %s", rec.Code, rec.Body)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after rejected PUT: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a rejected PUT")
	}
}

// TestPutBlueprintIsByteStableAcrossRepeatedPuts is the brief's
// byte-stability requirement, direct: PUTting the identical document twice
// in a row must leave byte-for-byte identical files, not just
// semantically-equal YAML that happens to format differently between the two
// writes. (TestConsecutiveIdenticalEditsProduceIdenticalBytes above pins the
// analogous property across two independent servers for the parameter
// routes; this is the same property for PUT, on one server, across two
// successive requests, which is what the brief asks for.)
func TestPutBlueprintIsByteStableAcrossRepeatedPuts(t *testing.T) {
	h, path := testHandlerWithPath(t)

	current := mustLoadBlueprint(t, path)
	body, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if rec := do(t, h, "PUT", "/api/blueprint", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("first PUT: status %d: %s", rec.Code, rec.Body)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first PUT: %v", err)
	}

	if rec := do(t, h, "PUT", "/api/blueprint", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("second PUT: status %d: %s", rec.Code, rec.Body)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second PUT: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("PUTting the identical document twice produced different bytes:\n"+
			"--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestPutBlueprintIsNeverAnsweredWith304 pins the brief's conditional-request
// requirement directly, rather than trusting wrap's method/status gate in
// server.go (GET/HEAD + 200 only) by inspection alone: a PUT sent with
// If-None-Match set to the resource's own current ETag must still come back
// a normal 200 carrying the persisted body, never a bodyless 304 -- a 304
// for a PUT would be a lie (it would tell the caller "nothing changed, use
// your cached copy" for a request that just wrote the document).
func TestPutBlueprintIsNeverAnsweredWith304(t *testing.T) {
	h := testHandler(t)

	getRec := do(t, h, "GET", "/api/blueprint", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", getRec.Code, getRec.Body)
	}
	etag := getRec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET did not return an ETag")
	}

	req := httptest.NewRequest("PUT", "/api/blueprint", bytes.NewBufferString(getRec.Body.String()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 -- PUT must never be answered with a 304 even when "+
			"If-None-Match matches the current ETag: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() == 0 {
		t.Error("PUT response body is empty -- looks like it was answered as a 304")
	}
}

// TestConcurrentPutAndParameterPostSerializes is the brief's concurrency
// requirement, adapted from TestConcurrentAddsDoNotLoseEdits above.
//
// That test proves the mutex closes the lost-update race between N
// concurrent partial edits (all load -> edit -> persist). PUT does not fit
// the same shape directly -- it has no load step, and it is designed to
// unconditionally discard whatever it doesn't itself carry, so "every
// concurrent change survives" is not the right invariant here (a full
// replace legitimately superseding an earlier concurrent edit is ordinary
// last-write-wins REST semantics, not a bug).
//
// The invariant that IS a bug if it fails: PUT's own write must never
// silently vanish without any later full replace explicitly superseding it.
// Only one thing in this test ever discards content wholesale -- the single
// PUT -- so under a correctly held srv.mu, PUT's distinguishing marker
// parameter (putOnly, added to a document built from the file's own current,
// valid content) must appear in the file after the race REGARDLESS of how
// the PUT and the eight concurrent parameter POSTs interleave:
//
//   - if PUT is the last operation to run, the file is exactly PUT's
//     document -- putOnly present.
//   - if some POSTs run after PUT, each of them loads whatever is then on
//     disk before adding its own parameter; under the mutex that load can
//     only ever see PUT's already-persisted document (never a pre-PUT one),
//     so putOnly survives every such edit stacked on top of it.
//
// The lost-update bug this guards against is exactly the case where a POST's
// load races ahead of PUT's write but its persist lands after PUT's --
// silently overwriting PUT's document with an edit of the stale copy that
// POST actually read, discarding putOnly (and everything else PUT set)
// without any operation's response ever admitting it. That is precisely the
// shape of bug fix round 2 already found and fixed for N-vs-N parameter
// edits (see server.mu's own doc comment in server.go); this test is the
// same probe, aimed at PUT instead.
func TestConcurrentPutAndParameterPostSerializes(t *testing.T) {
	h, path := testHandlerWithPath(t)

	current := mustLoadBlueprint(t, path)
	putDoc := *current
	putDoc.Spec.XRD.Parameters = make(map[string]blueprint.Parameter, len(current.Spec.XRD.Parameters)+1)
	for k, v := range current.Spec.XRD.Parameters {
		putDoc.Spec.XRD.Parameters[k] = v
	}
	putDoc.Spec.XRD.Parameters["putOnly"] = blueprint.Parameter{Type: "string"}
	putBody, err := json.Marshal(putDoc)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}

	const n = 8
	postName := func(i int) string { return fmt.Sprintf("postOnly%d", i) }

	type result struct {
		code int
		body string
	}
	var putResult result
	postResults := make([]result, n)

	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("PUT", "/api/blueprint", bytes.NewReader(putBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		<-start
		h.ServeHTTP(rec, req)
		putResult = result{rec.Code, rec.Body.String()}
	}()

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/blueprint/parameters",
				strings.NewReader(fmt.Sprintf(`{"name":%q,"parameter":{"type":"string"}}`, postName(i))))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			<-start
			h.ServeHTTP(rec, req)
			postResults[i] = result{rec.Code, rec.Body.String()}
		}(i)
	}
	close(start)
	wg.Wait()

	if putResult.code != http.StatusOK {
		t.Errorf("PUT: status %d: %s", putResult.code, putResult.body)
	}
	for i, r := range postResults {
		if r.code != http.StatusOK {
			t.Errorf("POST %s: status %d: %s", postName(i), r.code, r.body)
		}
	}

	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint on disk no longer loads after the race: %v", err)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["putOnly"]; !ok {
		t.Error("the PUT's own parameter is missing from the final file: a concurrent parameter POST " +
			"persisted an edit of a stale, pre-PUT document over the top of it -- srv.mu did not " +
			"serialize PUT against the parameter handlers, reproducing the lost-update race fix round 2 " +
			"already closed for N concurrent parameter edits")
	}
}

// --- PUT /api/blueprint: spec.pipeline survives the round trip ---
//
// The canvas PUTs its whole document, and decodeJSON uses
// DisallowUnknownFields: before blueprint.Spec gained the Pipeline field, a
// document carrying spec.pipeline would have been 400'd at the door (and,
// worse, a Blueprint re-marshaled without the field would silently DROP the
// user's declared steps on the next persist). This pins the whole loop: PUT
// accepts it, GET agrees, the file on disk loads back with every step — the
// raw input string byte-for-byte — and a second identical PUT is byte-stable.
func TestPutBlueprintRoundTripsPipeline(t *testing.T) {
	h, path := testHandlerWithPath(t)

	const rawInput = "kind: Input\napiVersion: fn.example.org/v1beta1\nzeta: first\nalpha: last\n"
	current := mustLoadBlueprint(t, path)
	updated := *current
	updated.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "prep",
			FunctionRef: "function-example",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-example:v1.0.0",
			Position:    blueprint.PositionBefore,
			Input:       rawInput,
		},
		{
			Name:        "auto-ready",
			FunctionRef: "function-auto-ready",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-auto-ready",
		},
	}
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status %d (DisallowUnknownFields rejecting spec.pipeline?): %s", rec.Code, rec.Body)
	}

	var fromPut blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &fromPut); err != nil {
		t.Fatalf("PUT response not JSON: %v", err)
	}
	if diff := cmp.Diff(updated.Spec.Pipeline, fromPut.Spec.Pipeline); diff != "" {
		t.Errorf("PUT response pipeline (-sent +got):\n%s", diff)
	}

	getRec := do(t, h, "GET", "/api/blueprint", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", getRec.Code, getRec.Body)
	}
	var fromGet blueprint.Blueprint
	if err := json.Unmarshal(getRec.Body.Bytes(), &fromGet); err != nil {
		t.Fatalf("GET response not JSON: %v", err)
	}
	if diff := cmp.Diff(updated.Spec.Pipeline, fromGet.Spec.Pipeline); diff != "" {
		t.Errorf("GET pipeline (-sent +got):\n%s", diff)
	}

	reloaded := mustLoadBlueprint(t, path)
	if diff := cmp.Diff(updated.Spec.Pipeline, reloaded.Spec.Pipeline); diff != "" {
		t.Errorf("persisted pipeline (-sent +got):\n%s", diff)
	}
	if len(reloaded.Spec.Pipeline) > 0 && reloaded.Spec.Pipeline[0].Input != rawInput {
		t.Errorf("input did not survive persist+reload verbatim:\ngot  %q\nwant %q",
			reloaded.Spec.Pipeline[0].Input, rawInput)
	}

	// Byte-stability, same property TestPutBlueprintIsByteStableAcrossRepeatedPuts
	// pins for a pipeline-free document.
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first PUT: %v", err)
	}
	if rec := do(t, h, "PUT", "/api/blueprint", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("second PUT: status %d: %s", rec.Code, rec.Body)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second PUT: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("PUTting the identical pipelined document twice produced different bytes:\n"+
			"--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// A pipelined document that fails the pipeline's own validation is a 400
// with the engine's error verbatim, and the file is untouched — the same
// contract every other PUT rejection gives.
func TestPutBlueprintWithInvalidPipelineIs400(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	current := mustLoadBlueprint(t, path)
	invalid := *current
	invalid.Spec.Pipeline = []blueprint.PipelineStep{
		{Name: blueprint.TemplatingStepName, FunctionRef: "function-x", Package: "example.org/fn-x:v1"},
	}
	wantErr := invalid.Validate()
	if wantErr == nil {
		t.Fatal("test setup: a step named after the templating step was expected to fail Validate")
	}
	body, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	var errBody errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error response not JSON: %v (%s)", err, rec.Body)
	}
	if errBody.Error != wantErr.Error() {
		t.Errorf("error body = %q, want Validate's error verbatim: %q", errBody.Error, wantErr.Error())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after rejected PUT: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a rejected PUT")
	}
}
