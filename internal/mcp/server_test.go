// These tests drive the MCP server the way an agent's MCP client does: a
// real SDK client connected over an in-memory transport, calling tools/call
// and reading the results. Nothing here reaches into the handlers directly,
// so what passes is what a real client would observe.
//
// The central assertion, repeated for every tool with a reachable failure,
// is ERROR PARITY: a failing tool call must surface the exact error text the
// HTTP API returns for the same input — because the whole architecture of
// this package (see server.go) is that the two front doors are one
// implementation. Each parity test therefore drives the tool AND an api.New
// handler over the same underlying state, and compares the strings verbatim.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/testfixture"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// testProviderRef is the xpkg ref the fixture CRD is cached and indexed
// under, and the provider the test blueprint's source and resource name.
const testProviderRef = "ghcr.io/x/provider-aws-sqs:v2.7.0"

// testCRDs returns the one-Queue fixture BOTH the index and the store are
// seeded from — the same single-load invariant cmd/cf's buildAPIOptions
// enforces in production (unlike internal/api's own tests, which deliberately
// diverge the two; nothing here needs that divergence).
func testCRDs(t *testing.T) []schema.CRD {
	return testfixture.QueueCRDs(t)
}

// testBlueprintYAML is a valid Namespaced blueprint against testCRDs:
// providerName (required by validation), maxMessageSize (referenced by
// main-queue's field, so deleting it must be refused) and location
// (unreferenced and carrying a description, so delete has a legal target and
// update_parameter's omitted-key refusal has a value to protect).
const testBlueprintYAML = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: ghcr.io/x/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
      location: {type: string, description: Where the queue lives}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/x/provider-aws-sqs:v2.7.0
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

// stack is one complete fixture world: an MCP client session into a server
// built by New, plus an api.New handler over the SAME api.Options, so a test
// can make the identical request through both front doors and compare.
type stack struct {
	session   *sdk.ClientSession
	handler   http.Handler
	options   api.Options
	blueprint string // path to the blueprint file on disk
	storeRoot string // the cache.Store's root dir, removable to break the cache
	outDir    string
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()

	crds := testCRDs(t)
	storeRoot := t.TempDir()
	store := cache.New(storeRoot)
	if err := store.Save(&xpkg.Package{Ref: testProviderRef, Digest: "sha256:test"}, crds); err != nil {
		t.Fatalf("seed provider cache: %v", err)
	}
	idx, err := index.Build(map[string][]schema.CRD{testProviderRef: crds})
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}

	bp := filepath.Join(t.TempDir(), "xqueue.cf.yaml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	if _, err := blueprint.Load(bp); err != nil {
		t.Fatalf("test blueprint fixture does not itself validate: %v", err)
	}

	o := api.Options{
		Index:     idx,
		Store:     store,
		Blueprint: bp,
		OutDir:    t.TempDir(),
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
		Providers: []string{testProviderRef},
	}

	srv, err := New(o, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	h, err := api.New(o)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return &stack{
		session:   session,
		handler:   h,
		options:   o,
		blueprint: bp,
		storeRoot: storeRoot,
		outDir:    o.OutDir,
	}
}

// callTool calls one tool and returns its first text content plus IsError.
func (s *stack) callTool(t *testing.T, name string, args any) (string, bool) {
	t.Helper()
	res, err := s.session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("CallTool %s: result has no content", name)
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("CallTool %s: content[0] is %T, want *TextContent", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

// toolOK calls a tool, fails the test on an error result, and decodes the
// JSON payload into a generic map for assertions.
func (s *stack) toolOK(t *testing.T, name string, args any) map[string]any {
	t.Helper()
	text, isErr := s.callTool(t, name, args)
	if isErr {
		t.Fatalf("%s(%+v) errored: %s", name, args, text)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s result is not JSON: %v\n%s", name, err, text)
	}
	return v
}

// httpError makes the given request against the parity handler, requires an
// error status, and returns the {"error": ...} message.
func (s *stack) httpError(t *testing.T, method, path, body string) string {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	if rec.Code < http.StatusBadRequest {
		t.Fatalf("%s %s: status %d, want an error status", method, path, rec.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
		t.Fatalf("%s %s: error body %q is not the JSON error shape", method, path, rec.Body.String())
	}
	return e.Error
}

// assertToolErrorMatchesHTTP is the parity check: the tool call must fail,
// and its error text must equal — verbatim — what the HTTP API returns for
// the equivalent request over the same state.
func (s *stack) assertToolErrorMatchesHTTP(t *testing.T, tool string, args any, method, path, body string) {
	t.Helper()
	text, isErr := s.callTool(t, tool, args)
	if !isErr {
		t.Fatalf("%s(%+v) succeeded, want an error; result: %s", tool, args, text)
	}
	want := s.httpError(t, method, path, body)
	if text != want {
		t.Errorf("%s error text diverged from the HTTP API's:\n mcp:  %q\n http: %q", tool, text, want)
	}
}

// reload reads the blueprint back from disk, so a test can assert what a
// mutation actually persisted (or that a refused one persisted nothing).
func (s *stack) reload(t *testing.T) *blueprint.Blueprint {
	t.Helper()
	b, err := blueprint.Load(s.blueprint)
	if err != nil {
		t.Fatalf("reload blueprint: %v", err)
	}
	return b
}

// --- initialize / tools listing ---

func TestListToolsAdvertisesTheFullOperationSet(t *testing.T) {
	s := newStack(t)
	res, err := s.session.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := []string{
		"add_parameter", "add_provider", "adopt_composition", "delete_parameter", "generate",
		"get_blueprint", "get_kind_fields", "list_kinds", "list_providers",
		"rename_parameter", "render_check", "replace_blueprint", "update_parameter",
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has no description — an agent caller has nothing to choose it by", tool.Name)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("tool set = %v, want %v", got, want)
	}
	for i := range want { // ListTools returns tools sorted by name
		if got[i] != want[i] {
			t.Fatalf("tool set = %v, want %v", got, want)
		}
	}
}

// --- list_kinds ---

func TestListKinds(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "list_kinds", map[string]any{})
	kinds := v["kinds"].([]any)
	if len(kinds) != 1 {
		t.Fatalf("kinds = %v, want exactly the fixture Queue", kinds)
	}
	k := kinds[0].(map[string]any)
	if k["kind"] != "Queue" || k["apiVersion"] != "sqs.aws.m.upbound.io/v1beta1" || k["provider"] != testProviderRef {
		t.Errorf("kind = %v, want the fixture Queue with its apiVersion and provider", k)
	}

	// A search that matches nothing is a success with an empty list — the
	// same contract GET /api/kinds has — not an error.
	v = s.toolOK(t, "list_kinds", map[string]any{"search": "nomatch", "limit": 5})
	if kinds := v["kinds"].([]any); len(kinds) != 0 {
		t.Errorf("kinds = %v for a no-match search, want []", kinds)
	}
}

// TestListKindsRejectsMistypedInput: list_kinds has no reachable server-side
// validation failure — its only HTTP 400 is a non-numeric limit string,
// which the typed input schema makes unsendable. What remains to prove is
// that the schema actually enforces that: a string limit must be refused as
// a tool error (by the SDK's own input validation) rather than coerced or
// passed through.
func TestListKindsRejectsMistypedInput(t *testing.T) {
	s := newStack(t)
	text, isErr := s.callTool(t, "list_kinds", map[string]any{"limit": "abc"})
	if !isErr {
		t.Fatalf("list_kinds(limit:\"abc\") succeeded (%s), want a schema validation error", text)
	}
	if !strings.Contains(text, "limit") {
		t.Errorf("error %q does not name the offending property", text)
	}
}

// --- get_kind_fields ---

func TestGetKindFields(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "get_kind_fields", map[string]any{
		"api_version": "sqs.aws.m.upbound.io/v1beta1",
		"kind":        "Queue",
	})
	fields := v["fields"].([]any)
	if len(fields) == 0 {
		t.Fatal("no fields for the fixture Queue")
	}
	if total := int(v["total"].(float64)); total != len(fields) {
		t.Errorf("total = %d with no limit, want %d", total, len(fields))
	}

	v = s.toolOK(t, "get_kind_fields", map[string]any{
		"api_version":   "sqs.aws.m.upbound.io/v1beta1",
		"kind":          "Queue",
		"required_only": true,
	})
	for _, f := range v["fields"].([]any) {
		if f.(map[string]any)["required"] != true {
			t.Errorf("required_only:true returned a non-required field: %v", f)
		}
	}
}

func TestGetKindFieldsUnknownKindMatchesHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"get_kind_fields", map[string]any{"api_version": "nope.example.com/v1", "kind": "Nope"},
		http.MethodGet, "/api/kinds/nope.example.com%2Fv1/Nope/fields", "")
}

// --- get_blueprint ---

func TestGetBlueprint(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "get_blueprint", nil)
	xrd := v["spec"].(map[string]any)["xrd"].(map[string]any)
	if xrd["kind"] != "XQueue" {
		t.Errorf("spec.xrd.kind = %v, want XQueue", xrd["kind"])
	}
}

func TestGetBlueprintUnparseableFileMatchesHTTP(t *testing.T) {
	s := newStack(t)
	// Corrupt the shared file AFTER both stacks are built: both front doors
	// load it per request, so both now fail — with the identical message,
	// path included, because it is the same path.
	if err := os.WriteFile(s.blueprint, []byte("{invalid: [yaml"), 0o644); err != nil {
		t.Fatalf("corrupt blueprint: %v", err)
	}
	s.assertToolErrorMatchesHTTP(t, "get_blueprint", nil, http.MethodGet, "/api/blueprint", "")
}

// --- replace_blueprint ---

func TestReplaceBlueprint(t *testing.T) {
	s := newStack(t)

	// Round-trip the real document with one addition, the way an agent
	// would: read, edit, send back whole.
	doc := s.toolOK(t, "get_blueprint", nil)
	params := doc["spec"].(map[string]any)["xrd"].(map[string]any)["parameters"].(map[string]any)
	params["visibilityTimeout"] = map[string]any{"type": "integer"}

	s.toolOK(t, "replace_blueprint", map[string]any{"blueprint": doc})

	if _, ok := s.reload(t).Spec.XRD.Parameters["visibilityTimeout"]; !ok {
		t.Error("the replaced document did not persist to disk")
	}
}

func TestReplaceBlueprintValidationFailureMatchesHTTP(t *testing.T) {
	s := newStack(t)
	doc := s.toolOK(t, "get_blueprint", nil)
	doc["spec"].(map[string]any)["xrd"].(map[string]any)["scope"] = "Cluster"

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s.assertToolErrorMatchesHTTP(t,
		"replace_blueprint", map[string]any{"blueprint": doc},
		http.MethodPut, "/api/blueprint", string(body))

	if s.reload(t).Spec.XRD.Scope != "Namespaced" {
		t.Error("a refused replace still mutated the file on disk")
	}
}

// TestReplaceBlueprintUnknownKeyMatchesHTTP proves the raw passthrough: an
// unknown key must reach internal/api's DisallowUnknownFields decoder intact
// and come back with its exact error — the reason the input is
// json.RawMessage rather than a typed struct that would silently drop it.
func TestReplaceBlueprintUnknownKeyMatchesHTTP(t *testing.T) {
	s := newStack(t)
	doc := s.toolOK(t, "get_blueprint", nil)
	doc["bogusTopLevelKey"] = true

	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s.assertToolErrorMatchesHTTP(t,
		"replace_blueprint", map[string]any{"blueprint": doc},
		http.MethodPut, "/api/blueprint", string(body))
}

// --- add_parameter ---

func TestAddParameter(t *testing.T) {
	s := newStack(t)
	s.toolOK(t, "add_parameter", map[string]any{
		"name":      "visibilityTimeout",
		"parameter": map[string]any{"type": "integer", "description": "Seconds a message stays hidden"},
	})
	p, ok := s.reload(t).Spec.XRD.Parameters["visibilityTimeout"]
	if !ok {
		t.Fatal("added parameter did not persist")
	}
	if p.Type != "integer" || p.Description != "Seconds a message stays hidden" {
		t.Errorf("persisted parameter = %+v, want the declaration sent", p)
	}
}

func TestAddParameterDuplicateMatchesHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"add_parameter", map[string]any{"name": "maxMessageSize", "parameter": map[string]any{"type": "integer"}},
		http.MethodPost, "/api/blueprint/parameters",
		`{"name":"maxMessageSize","parameter":{"type":"integer"}}`)
}

// --- update_parameter ---

func TestUpdateParameter(t *testing.T) {
	s := newStack(t)
	// A full replacement names every key the declaration currently holds a
	// value for — here location's type and description.
	s.toolOK(t, "update_parameter", map[string]any{
		"name":      "location",
		"parameter": map[string]any{"type": "string", "description": "Region the queue lives in"},
	})
	if got := s.reload(t).Spec.XRD.Parameters["location"].Description; got != "Region the queue lives in" {
		t.Errorf("description = %q after update, want the replacement", got)
	}
}

func TestUpdateParameterOmittedKeyRefusalMatchesHTTP(t *testing.T) {
	s := newStack(t)
	// location currently holds a description; omitting the key from a full
	// replace would silently discard it, which the API refuses — the MCP
	// caller must see that refusal word for word.
	s.assertToolErrorMatchesHTTP(t,
		"update_parameter", map[string]any{"name": "location", "parameter": map[string]any{"type": "string"}},
		http.MethodPut, "/api/blueprint/parameters/location",
		`{"parameter":{"type":"string"}}`)

	if got := s.reload(t).Spec.XRD.Parameters["location"].Description; got != "Where the queue lives" {
		t.Errorf("a refused update still changed the file: description = %q", got)
	}
}

func TestUpdateParameterUnknownNameMatchesHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"update_parameter", map[string]any{"name": "nope", "parameter": map[string]any{"type": "string"}},
		http.MethodPut, "/api/blueprint/parameters/nope",
		`{"parameter":{"type":"string"}}`)
}

// --- rename_parameter ---

func TestRenameParameter(t *testing.T) {
	s := newStack(t)
	s.toolOK(t, "rename_parameter", map[string]any{"name": "maxMessageSize", "to": "messageSize"})

	b := s.reload(t)
	if _, ok := b.Spec.XRD.Parameters["messageSize"]; !ok {
		t.Fatal("renamed parameter is not declared on disk")
	}
	if _, ok := b.Spec.XRD.Parameters["maxMessageSize"]; ok {
		t.Error("old parameter name is still declared")
	}
	if got := b.Spec.Resources[0].Fields["maxMessageSize"].From; got != "params.messageSize" {
		t.Errorf("resource field from = %q, want the reference rewritten to params.messageSize", got)
	}
}

func TestRenameParameterCollisionMatchesHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"rename_parameter", map[string]any{"name": "location", "to": "providerName"},
		http.MethodPost, "/api/blueprint/parameters/location/rename",
		`{"to":"providerName"}`)
}

// --- delete_parameter ---

func TestDeleteParameter(t *testing.T) {
	s := newStack(t)
	s.toolOK(t, "delete_parameter", map[string]any{"name": "location"})
	if _, ok := s.reload(t).Spec.XRD.Parameters["location"]; ok {
		t.Error("deleted parameter is still declared on disk")
	}
}

func TestDeleteParameterStillReferencedMatchesHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"delete_parameter", map[string]any{"name": "maxMessageSize"},
		http.MethodDelete, "/api/blueprint/parameters/maxMessageSize", "")

	if _, ok := s.reload(t).Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("a refused delete still removed the parameter from disk")
	}
}

// --- providers ---

func TestListProviders(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "list_providers", nil)
	providers := v["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("providers = %v, want exactly the fixture provider", providers)
	}
	p := providers[0].(map[string]any)
	if p["ref"] != testProviderRef || p["digest"] != "sha256:test" || p["kinds"] != float64(1) {
		t.Errorf("provider = %v, want {ref: %s, digest: sha256:test, kinds: 1}", p, testProviderRef)
	}
}

func TestListProvidersBrokenCacheMatchesHTTP(t *testing.T) {
	s := newStack(t)
	if err := os.RemoveAll(s.storeRoot); err != nil {
		t.Fatalf("break cache: %v", err)
	}
	s.options.Store.Clear()
	s.assertToolErrorMatchesHTTP(t, "list_providers", nil, http.MethodGet, "/api/providers", "")
}

// TestAddProviderFailuresMatchHTTP covers the two failures reachable without
// a network: a ref that fails validation, and a ref the server already
// serves. The fetch-success path cannot be exercised here — the network seam
// (api.Options' unexported fetch field) is private to internal/api by
// design, and this package deliberately owns no fetching logic of its own to
// stub — but both failures pass through the full bridge, decode and
// duplicate-check machinery the success path shares.
func TestAddProviderFailuresMatchHTTP(t *testing.T) {
	s := newStack(t)
	s.assertToolErrorMatchesHTTP(t,
		"add_provider", map[string]any{"ref": "not a valid ref"},
		http.MethodPost, "/api/providers", `{"ref":"not a valid ref"}`)

	s.assertToolErrorMatchesHTTP(t,
		"add_provider", map[string]any{"ref": testProviderRef},
		http.MethodPost, "/api/providers", `{"ref":"`+testProviderRef+`"}`)
}

// --- generate ---

func TestGenerateDryRunTouchesNothing(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "generate", map[string]any{"write": false})
	outputs := v["outputs"].([]any)
	// Four, not three: testBlueprintYAML's one source (provider-aws-sqs)
	// derives provider family "aws" (internal/emit/providerconfigs.go), and
	// testCRDs carries no ClusterProviderConfig CRD for it, so Generate also
	// emits the family's providerconfigs/aws.yaml scaffold.
	if len(outputs) != 4 {
		t.Fatalf("outputs = %d files, want 4 (composition, functions, providerconfigs/aws.yaml, xrd)", len(outputs))
	}
	if v["written"] != false {
		t.Errorf("written = %v on a dry run, want false", v["written"])
	}
	for _, o := range outputs {
		out := o.(map[string]any)
		path := out["path"].(string)
		if !strings.HasPrefix(path, s.outDir+string(os.PathSeparator)) {
			t.Errorf("output path %q is not inside the workspace %q", path, s.outDir)
		}
		if out["body"].(string) == "" {
			t.Errorf("output %q has an empty body — the preview must carry the content", path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("dry run left %q on disk (stat err: %v)", path, err)
		}
	}
}

func TestGenerateWriteMatchesTheEngineByteForByte(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "generate", map[string]any{"write": true})
	if v["written"] != true {
		t.Fatalf("written = %v, want true", v["written"])
	}

	// The reference: emit.Generate called directly with the same inputs the
	// server holds — the engine rule made checkable.
	b := s.reload(t)
	want, err := emit.Generate(b, testCRDs(t), s.outDir)
	if err != nil {
		t.Fatalf("emit.Generate: %v", err)
	}
	if len(want) != 4 {
		t.Fatalf("engine produced %d outputs, want 4", len(want))
	}
	for _, w := range want {
		got, err := os.ReadFile(w.Path)
		if err != nil {
			t.Errorf("expected output %q was not written: %v", w.Path, err)
			continue
		}
		if !bytes.Equal(got, w.Body) {
			t.Errorf("%q differs from the engine's own output", w.Path)
		}
	}
}

func TestGenerateBrokenCacheMatchesHTTP(t *testing.T) {
	s := newStack(t)
	if err := os.RemoveAll(s.storeRoot); err != nil {
		t.Fatalf("break cache: %v", err)
	}
	s.options.Store.Clear()
	s.assertToolErrorMatchesHTTP(t,
		"generate", map[string]any{"write": false},
		http.MethodPost, "/api/generate", `{"write":false}`)
}

// --- render_check ---

// TestRenderCheckUnavailableMatchesHTTP pins the no-crossplane outcome, made
// deterministic by emptying PATH: a 200 whose payload says the environment
// cannot run the check — never a fake ok, and never an error dressed up as
// one. The whole payload is compared against the HTTP route's, not just the
// error string, since both are success responses.
func TestRenderCheckUnavailableMatchesHTTP(t *testing.T) {
	t.Setenv("PATH", "")

	s := newStack(t)
	text, isErr := s.callTool(t, "render_check", nil)
	if isErr {
		t.Fatalf("render_check errored: %s — unavailability is a payload, not an error", text)
	}
	var v struct {
		OK          bool   `json:"ok"`
		Unavailable string `json:"unavailable"`
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("render_check result is not JSON: %v", err)
	}
	if v.OK || !strings.Contains(v.Unavailable, "crossplane") {
		t.Errorf("payload = %s, want ok:false and an unavailable message naming the crossplane CLI", text)
	}

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/render", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP render status = %d, want 200", rec.Code)
	}
	if text != rec.Body.String() {
		t.Errorf("render_check payload diverged from the HTTP API's:\n mcp:  %s\n http: %s", text, rec.Body)
	}
}

// TestRenderCheckBrokenCacheMatchesHTTP reaches render's 400 path — inputs
// failing before any exec — by putting a stub crossplane on PATH (so the
// availability probe passes) and breaking the schema cache.
func TestRenderCheckBrokenCacheMatchesHTTP(t *testing.T) {
	bin := t.TempDir()
	stub := filepath.Join(bin, "crossplane")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write stub crossplane: %v", err)
	}
	t.Setenv("PATH", bin)

	s := newStack(t)
	if err := os.RemoveAll(s.storeRoot); err != nil {
		t.Fatalf("break cache: %v", err)
	}
	s.options.Store.Clear()
	s.assertToolErrorMatchesHTTP(t, "render_check", nil, http.MethodPost, "/api/render", "")
}

func TestAdoptComposition(t *testing.T) {
	s := newStack(t)
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-adopted-mcp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`
	v := s.toolOK(t, "adopt_composition", map[string]any{
		"manifest": manifest,
		"provider": "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
		"persist":  true,
	})
	bpRaw, ok := v["blueprint"].(map[string]any)
	if !ok {
		t.Fatalf("expected blueprint in adopt_composition response")
	}
	meta, _ := bpRaw["metadata"].(map[string]any)
	if meta["name"] != "test-adopted-mcp" {
		t.Errorf("blueprint name = %v, want test-adopted-mcp", meta["name"])
	}
}

func TestGetKindFieldsStatus(t *testing.T) {
	s := newStack(t)
	v := s.toolOK(t, "get_kind_fields", map[string]any{
		"api_version": "sqs.aws.m.upbound.io/v1beta1",
		"kind":        "Queue",
		"status":      true,
	})
	fieldsRaw, ok := v["fields"].([]any)
	if !ok || len(fieldsRaw) == 0 {
		t.Fatalf("expected status fields in response: %+v", v)
	}
	foundURL := false
	for _, fRaw := range fieldsRaw {
		fMap, _ := fRaw.(map[string]any)
		if fMap["path"] == "atProvider.url" {
			foundURL = true
			break
		}
	}
	if !foundURL {
		t.Errorf("status fields %+v missing atProvider.url", fieldsRaw)
	}
}

func TestReplaceBlueprintValidationAndAtomic(t *testing.T) {
	s := newStack(t)
	before, _ := os.ReadFile(s.blueprint)

	// Load valid blueprint
	bp, err := blueprint.Load(s.blueprint)
	if err != nil {
		t.Fatalf("load blueprint: %v", err)
	}

	// Insert invalid typo field
	bp.Spec.Resources[0].Fields["regin"] = blueprint.Field{Value: "eu-west-1"}
	bpBytes, _ := json.Marshal(bp)

	var doc map[string]any
	_ = json.Unmarshal(bpBytes, &doc)

	text, isErr := s.callTool(t, "replace_blueprint", map[string]any{
		"blueprint": doc,
	})
	if !isErr {
		t.Fatal("expected replace_blueprint with invalid field to error")
	}
	if !strings.Contains(text, `did you mean "region"?`) {
		t.Errorf("error %q should contain did-you-mean suggestion", text)
	}

	after, _ := os.ReadFile(s.blueprint)
	if !bytes.Equal(before, after) {
		t.Error("blueprint file changed on disk despite invalid replace_blueprint")
	}
}
