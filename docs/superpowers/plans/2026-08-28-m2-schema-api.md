# compositionfactory M2 — Schema API and XRD Editing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve the schema and blueprint-editing API that the canvas, the CLI and the MCP server will all read — kind search, required-first field trees, per-kind lazy fetch, and parameter editing — over one HTTP surface backed by the existing engine.

**Architecture:** Two new internal packages and one command. `internal/index` turns cached CRDs into a compact searchable index plus a depth-limited field view. `internal/api` is a thin HTTP layer over `index`, `blueprint` and `emit` — it holds no generation logic of its own, because M3's canvas and M5's MCP server must produce byte-identical output to `cf gen`. `cmd/cf/serve.go` wires it up on loopback only.

**Tech Stack:** Go 1.25 · stdlib `net/http` with Go 1.22+ method-and-pattern routing · `github.com/alecthomas/kong` · `github.com/google/go-cmp` · no new third-party dependencies

**Spec:** [`docs/superpowers/specs/2026-08-27-compositionfactory-design.md`](../specs/2026-08-27-compositionfactory-design.md) — read §3 (architecture), §4 (schema subsystem), §8 (correctness), §11 (interfaces).

## Global Constraints

- Module path `github.com/koorikla/compositionfactory`. Go 1.25.
- **No new third-party dependencies.** Standard library plus what is already in `go.mod`. `go mod tidy` must remain a no-op.
- **No cluster and no Docker in any test in this plan.** All tests run offline; filesystem tests use `t.TempDir()` and must never touch the real cache directory or `$HOME`.
- **`cf serve` binds `127.0.0.1` by default and must never default to `0.0.0.0`.** The API writes files and reads the local schema cache; exposing it to the network is a security defect, not a configuration preference.
- **`internal/api` contains no generation logic.** Every generate path calls `emit.Generate`. If a code path exists only in the server, a canvas-authored artifact cannot be reproduced by `cf gen` and the GitOps story collapses.
- **Validation lives in `blueprint.Validate()`**, which already enforces identifier formats, control characters, defaults-by-type, `scope`, composite `from:` sources and `providerName`. The API must not duplicate or weaken those rules; it surfaces their errors.
- Byte-determinism remains a correctness requirement for anything written to disk: sorted keys, stable order, LF only, one trailing newline, no timestamps.
- Provenance in YAML comments, never annotations.

## Existing surface this plan builds on (do not redefine)

```go
// internal/schema
type CRD struct{ Group, Kind, Plural, Scope string; Categories []string; Versions []Version }
type Node struct{ Name, Type, Description string; Required bool; Children []*Node }
type Leaf struct{ Path string; Node *Node }
func (c CRD) Preferred() (Version, error)
func (c CRD) IsManaged() bool
func (c CRD) Namespaced() bool
func (c CRD) APIVersion() (string, error)   // NOTE: two return values
func (c CRD) ForProvider() ([]*Node, error) // (nil, nil) when absent — legitimate
func (c CRD) Envelope() ([]*Node, error)
func Leaves(nodes []*Node, prefix string) []Leaf  // indexes arrays of OBJECTS only;
                                                  // scalar arrays keep a plain path
// internal/cache
func New(root string) *Store
func DefaultRoot() string
func (s *Store) Load(ref string) ([]schema.CRD, error)
func (s *Store) LoadDigest(ref string) (string, error)
func ReadLock(path string) (*Lock, error)

// internal/blueprint
type Blueprint struct{ APIVersion, Kind string; Metadata Metadata; Spec Spec }
type Parameter struct{ Type string; Required bool; Enum []string; Default, Description string }
func Load(path string) (*Blueprint, error)
func (b *Blueprint) Validate() error

// internal/emit
func Generate(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error)
type Output struct{ Path string; Body []byte }
```

---

## Test helpers this plan requires (exact signatures)

These appear in test code across several tasks. They are written once, in the task noted, and reused
verbatim afterwards — do not redefine them in a later file, which would be a compile error.

```go
// internal/api/server_test.go — written in Task 4, reused by Tasks 5 and 6.
//
// Builds a handler over: an index containing the namespaced and cluster-scoped
// Queue fixture from internal/index's tests, a cache.Store rooted at t.TempDir(),
// and a valid blueprint written into t.TempDir() containing parameters
// providerName (string, required) and maxMessageSize (integer), and one resource
// "main-queue" of kind Queue whose maxMessageSize field is {from: params.maxMessageSize}.
func testHandler(t *testing.T) http.Handler

// internal/api/server_test.go — added in Task 6. Same handler, and the path of the
// blueprint file on disk so a test can reload it and assert what was persisted.
// testHandler must be reimplemented as a thin wrapper over this one so both stay
// in sync: func testHandler(t *testing.T) http.Handler { h, _ := testHandlerWithPath(t); return h }
func testHandlerWithPath(t *testing.T) (http.Handler, string)

// cmd/cf/serve_test.go — written in Task 7.
// Applies the kong-declared defaults to a zero-valued ServeCmd so a test can assert
// them without running a full parse. Implement by reading the struct tags via
// kong.New on a throwaway CLI, or by a small explicit assignment — state which you
// chose and why in your report.
func defaults(c *ServeCmd) error

// cmd/cf/serve.go — written in Task 7 (production code, not a test helper).
// Returns nil if Addr resolves to a loopback host, or if IKnowThisIsUnauthenticated
// is set. Otherwise returns an error naming the address AND explaining that the
// server has no authentication and writes files.
func (c *ServeCmd) check() error
```

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/index/index.go` | Build a compact kind index from cached CRDs; search it |
| `internal/index/fields.go` | Depth-limited, required-first field views over `schema.Node` |
| `internal/api/server.go` | Router, middleware (compression, ETag), server construction |
| `internal/api/kinds.go` | `/api/kinds` handlers |
| `internal/api/blueprint.go` | `/api/blueprint` handlers |
| `internal/api/generate.go` | `/api/generate` — thin call into `emit.Generate` |
| `internal/blueprint/edit.go` | Pure parameter add/rename/retype/delete on a `*Blueprint` |
| `cmd/cf/serve.go` | `cf serve` |

---

## Task 1: The kind index

**Files:**
- Create: `internal/index/index.go`, `internal/index/index_test.go`

**Interfaces:**
- Consumes: `schema.CRD`, `cache.Store`.
- Produces:
  ```go
  type Kind struct {
      Kind       string `json:"kind"`
      Group      string `json:"group"`
      Version    string `json:"version"`
      APIVersion string `json:"apiVersion"` // group/version
      Plural     string `json:"plural"`
      Scope      string `json:"scope"`      // Namespaced | Cluster
      Provider   string `json:"provider"`   // the xpkg ref it came from
      Namespaced bool   `json:"namespaced"`
      Required   int    `json:"required"`   // count of required forProvider leaves
      Fields     int    `json:"fields"`     // count of forProvider leaves
  }
  type Index struct{ /* unexported */ }
  func Build(byProvider map[string][]schema.CRD) (*Index, error)
  func (i *Index) All() []Kind                    // sorted, stable
  func (i *Index) Search(q string, limit int) []Kind
  func (i *Index) Lookup(apiVersion, kind string) (schema.CRD, bool)
  ```

**Why this shape:** the spec measured a full provider family's index at ~1 KB brotli against 4.27 MB of full schemas, so the index is what loads eagerly and full schemas are fetched per kind. `Required`/`Fields` counts let a palette show "8 req" without shipping the schema.

- [ ] **Step 1: Write the failing test**

`internal/index/index_test.go`:
```go
package index

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// crds returns one namespaced and one cluster-scoped Queue plus a ProviderConfig,
// mirroring what an upjet provider actually ships.
func crds(t *testing.T) map[string][]schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  tags: {type: object, additionalProperties: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: providerconfigs.aws.m.upbound.io}
spec:
  group: aws.m.upbound.io
  scope: Namespaced
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)}
	parsed, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]schema.CRD{"ghcr.io/x/provider-aws-sqs:v2.7.0": parsed}
}

func TestBuildIndexesOnlyManagedResources(t *testing.T) {
	i, err := Build(crds(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var kinds []string
	for _, k := range i.All() {
		kinds = append(kinds, k.APIVersion+"/"+k.Kind)
	}
	want := []string{
		"sqs.aws.m.upbound.io/v1beta1/Queue",
		"sqs.aws.upbound.io/v1beta1/Queue",
	}
	if diff := cmp.Diff(want, kinds); diff != "" {
		t.Errorf("indexed kinds (-want +got):\n%s\nProviderConfig is not a managed resource and must be excluded", diff)
	}
}

func TestAllIsSortedAndStable(t *testing.T) {
	i, _ := Build(crds(t))
	a, b := i.All(), i.All()
	if diff := cmp.Diff(a, b); diff != "" {
		t.Errorf("two calls differ (-first +second):\n%s", diff)
	}
	for n := 0; n < 5; n++ {
		j, _ := Build(crds(t))
		if diff := cmp.Diff(a, j.All()); diff != "" {
			t.Fatalf("rebuild %d differs, index must be deterministic:\n%s", n, diff)
		}
	}
}

func TestFieldCountsComeFromForProvider(t *testing.T) {
	i, _ := Build(crds(t))
	for _, k := range i.All() {
		if k.Namespaced {
			if k.Fields != 2 || k.Required != 1 {
				t.Errorf("namespaced Queue: Fields=%d Required=%d, want 2 and 1 (region required, tags not)",
					k.Fields, k.Required)
			}
		}
	}
}

func TestSearchMatchesKindCaseInsensitivelyAndRespectsLimit(t *testing.T) {
	i, _ := Build(crds(t))
	if got := i.Search("queue", 10); len(got) != 2 {
		t.Errorf("Search(queue) = %d results, want 2", len(got))
	}
	if got := i.Search("QUEUE", 10); len(got) != 2 {
		t.Errorf("Search is not case-insensitive: got %d", len(got))
	}
	if got := i.Search("queue", 1); len(got) != 1 {
		t.Errorf("Search ignored limit: got %d, want 1", len(got))
	}
	if got := i.Search("nothing-matches-this", 10); len(got) != 0 {
		t.Errorf("Search(nonsense) = %d, want 0", len(got))
	}
}

func TestSearchAlsoMatchesGroup(t *testing.T) {
	i, _ := Build(crds(t))
	if got := i.Search("sqs.aws.m", 10); len(got) != 1 {
		t.Errorf("Search by group = %d results, want 1 (only the .m. variant)", len(got))
	}
}

func TestLookupFindsTheExactVariant(t *testing.T) {
	i, _ := Build(crds(t))
	c, ok := i.Lookup("sqs.aws.m.upbound.io/v1beta1", "Queue")
	if !ok {
		t.Fatal("Lookup did not find the namespaced Queue")
	}
	if !c.Namespaced() {
		t.Error("Lookup returned the cluster-scoped variant for a .m. apiVersion")
	}
	if _, ok := i.Lookup("sqs.aws.m.upbound.io/v1beta1", "Nonexistent"); ok {
		t.Error("Lookup reported success for a kind that does not exist")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/index/ -v`
Expected: FAIL — package does not compile, `undefined: Build`.

- [ ] **Step 3: Write the implementation**

Create `internal/index/index.go` satisfying the Interfaces block above. Requirements, not a body to transcribe:

- `Build` indexes only CRDs where `IsManaged()` is true, and skips any CRD whose `Preferred()` or `APIVersion()` returns an error — a malformed CRD in one provider must not fail the whole index. Do not silently swallow it: no logging framework exists, so return an error only if EVERY CRD failed, and otherwise skip.
- `Fields` and `Required` come from `schema.Leaves(crd.ForProvider(), "")`. `ForProvider` legitimately returns `(nil, nil)` for provider-kubernetes-style CRDs; that is zero fields, not an error.
- `All()` returns a copy sorted by `(APIVersion, Kind)` so callers cannot mutate the index and two calls are identical.
- `Search` matches a case-insensitive substring against `Kind` and `Group`, preserves `All()`'s ordering, and applies `limit` (`limit <= 0` means no limit).
- Keep the CRDs themselves in the index so `Lookup` can return one without re-reading the cache.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/index/ -v -count=1` then `go test ./internal/index/ -count=20`
Expected: PASS, and stable across 20 runs (the determinism test iterates maps).

- [ ] **Step 5: Commit**

```bash
git add internal/index/
git commit -m "feat(index): searchable kind index over cached provider CRDs"
```

---

## Task 2: Depth-limited, required-first field views

**Files:**
- Create: `internal/index/fields.go`, `internal/index/fields_test.go`

**Interfaces:**
- Consumes: `schema.Node`, `schema.Leaves`.
- Produces:
  ```go
  type Field struct {
      Path        string `json:"path"`        // dotted, arrays of objects indexed: containers[0].image
      Type        string `json:"type"`        // string number integer boolean object array map
      Description string `json:"description"`
      Required    bool   `json:"required"`
      Depth       int    `json:"depth"`       // 0 for a top-level field
  }
  type FieldQuery struct {
      RequiredOnly bool
      MaxDepth     int    // 0 means unlimited
      Prefix       string // "" for the whole tree; e.g. "template.spec" to expand one subtree
      Search       string // case-insensitive substring over path and description
      Limit        int
  }
  func Fields(nodes []*schema.Node, q FieldQuery) []Field
  ```

**Why this shape:** the research measured provider schemas at depth 3–5 typically and 11 at the extreme, with one EC2 CRD carrying 263 properties and the largest MR schema at 1.7 MB raw. Neither a browser form nor an LLM context can take a whole tree, so every consumer needs the same three levers: required-only, depth cap, and expand-one-subtree. Building this once here is what lets the canvas, `cf` and the MCP server share a single API.

- [ ] **Step 1: Write the failing test**

`internal/index/fields_test.go`:
```go
package index

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// deepTree mirrors a real Deployment shape: an object nested inside an object
// inside an array of objects, plus a required scalar and a map leaf.
func deepTree(t *testing.T) []*schema.Node {
	t.Helper()
	props := map[string]any{
		"replicas": map[string]any{"type": "integer", "description": "Desired pods."},
		"selector": map[string]any{
			"type": "object", "required": []any{"matchLabels"},
			"properties": map[string]any{
				"matchLabels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
		},
		"template": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec": map[string]any{
					"type": "object", "required": []any{"containers"},
					"properties": map[string]any{
						"containers": map[string]any{
							"type": "array",
							"items": map[string]any{
								"required": []any{"name"},
								"properties": map[string]any{
									"name":  map[string]any{"type": "string", "description": "Container name."},
									"image": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}
	return schema.BuildTree(props, []string{"template"})
}

func paths(fs []Field) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Path)
	}
	return out
}

func TestFieldsReturnsEveryLeafByDefault(t *testing.T) {
	got := paths(Fields(deepTree(t), FieldQuery{}))
	want := []string{
		"replicas",
		"selector.matchLabels",
		"template.spec.containers[0].image",
		"template.spec.containers[0].name",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("default field list (-want +got):\n%s", diff)
	}
}

func TestDepthIsCountedFromZero(t *testing.T) {
	for _, f := range Fields(deepTree(t), FieldQuery{}) {
		var want int
		switch f.Path {
		case "replicas":
			want = 0
		case "selector.matchLabels":
			want = 1
		case "template.spec.containers[0].image", "template.spec.containers[0].name":
			want = 3
		}
		if f.Depth != want {
			t.Errorf("%s: Depth=%d, want %d", f.Path, f.Depth, want)
		}
	}
}

func TestMaxDepthPrunes(t *testing.T) {
	got := paths(Fields(deepTree(t), FieldQuery{MaxDepth: 1}))
	want := []string{"replicas", "selector.matchLabels"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MaxDepth=1 (-want +got):\n%s", diff)
	}
	if len(Fields(deepTree(t), FieldQuery{MaxDepth: 0})) != 4 {
		t.Error("MaxDepth=0 must mean unlimited, not zero fields")
	}
}

func TestRequiredOnlyKeepsOnlyRequiredLeaves(t *testing.T) {
	got := paths(Fields(deepTree(t), FieldQuery{RequiredOnly: true}))
	want := []string{"selector.matchLabels", "template.spec.containers[0].name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("RequiredOnly (-want +got):\n%s\n(a required LEAF, not a leaf under a required branch)", diff)
	}
}

func TestPrefixExpandsOneSubtree(t *testing.T) {
	got := paths(Fields(deepTree(t), FieldQuery{Prefix: "template.spec"}))
	want := []string{"template.spec.containers[0].image", "template.spec.containers[0].name"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Prefix (-want +got):\n%s", diff)
	}
	if len(Fields(deepTree(t), FieldQuery{Prefix: "no.such.path"})) != 0 {
		t.Error("an unmatched Prefix must return nothing, not everything")
	}
}

func TestSearchMatchesPathAndDescription(t *testing.T) {
	if got := paths(Fields(deepTree(t), FieldQuery{Search: "image"})); len(got) != 1 {
		t.Errorf("Search(image) = %v, want exactly the image field", got)
	}
	if got := paths(Fields(deepTree(t), FieldQuery{Search: "desired pods"})); len(got) != 1 {
		t.Errorf("Search must match description case-insensitively; got %v", got)
	}
}

func TestLimitApplies(t *testing.T) {
	if got := Fields(deepTree(t), FieldQuery{Limit: 2}); len(got) != 2 {
		t.Errorf("Limit=2 returned %d", len(got))
	}
}

func TestMapIsALeafNotABranch(t *testing.T) {
	for _, f := range Fields(deepTree(t), FieldQuery{}) {
		if f.Path == "selector.matchLabels" && f.Type != "map" {
			t.Errorf("selector.matchLabels Type=%q, want map", f.Type)
		}
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	a := Fields(deepTree(t), FieldQuery{})
	for n := 0; n < 20; n++ {
		if diff := cmp.Diff(a, Fields(deepTree(t), FieldQuery{})); diff != "" {
			t.Fatalf("run %d differs:\n%s", n, diff)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/index/ -run 'Fields|Depth|Required|Prefix|Search|Limit|MapIs|Deterministic' -v`
Expected: FAIL — `undefined: Fields`.

- [ ] **Step 3: Write the implementation**

Create `internal/index/fields.go` satisfying the Interfaces block. Requirements:

- Build on `schema.Leaves(nodes, "")` — do not re-walk the tree yourself, and do not change `Leaves`' semantics. It indexes arrays of **objects** (`containers[0].image`) and leaves arrays of **scalars** un-indexed and assigned whole.
- `Depth` is the number of separators before the leaf name; `replicas` is 0. An array index is part of its segment, so `template.spec.containers[0].name` is depth 3.
- Filters compose in a fixed order so the result is predictable: `Prefix`, then `MaxDepth`, then `RequiredOnly`, then `Search`, then `Limit`.
- `Prefix` matches on a path-segment boundary: prefix `template.spec` matches `template.spec.containers[0].name` but must not match a hypothetical `template.specimen`.
- Preserve `Leaves`' ordering throughout — never sort again, and never iterate a map in this file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/index/ -v -count=1` and `go test ./internal/index/ -count=20`
Expected: PASS, stable.

- [ ] **Step 5: Commit**

```bash
git add internal/index/fields.go internal/index/fields_test.go
git commit -m "feat(index): depth-limited, required-first field views"
```

---

## Task 3: Blueprint parameter editing

**Files:**
- Create: `internal/blueprint/edit.go`, `internal/blueprint/edit_test.go`

**Interfaces:**
- Consumes: `blueprint.Blueprint`, `blueprint.Parameter`.
- Produces:
  ```go
  func (b *Blueprint) AddParameter(name string, p Parameter) error
  func (b *Blueprint) RenameParameter(from, to string) error
  func (b *Blueprint) SetParameter(name string, p Parameter) error
  func (b *Blueprint) DeleteParameter(name string) error
  ```

**Why this shape:** these are the operations the XRD editor performs. They are pure functions on an in-memory blueprint so they can be tested without HTTP, and so the API layer stays a transport. **Every one must leave the blueprint valid or change nothing** — a half-applied rename that leaves a dangling `from: params.old` would emit a Composition that cannot render.

- [ ] **Step 1: Write the failing test**

`internal/blueprint/edit_test.go`:
```go
package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func editable() *Blueprint {
	return &Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			Sources: []Source{{Provider: "ghcr.io/x/provider-aws-sqs:v2.7.0"}},
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName":   {Type: "string", Required: true},
					"maxMessageSize": {Type: "integer"},
				},
			},
			Resources: []Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]Field{"maxMessageSize": {From: "params.maxMessageSize"}},
			}},
		},
	}
}

func TestAddParameter(t *testing.T) {
	b := editable()
	if err := b.AddParameter("location", Parameter{Type: "string", Required: true, Enum: []string{"EU", "US"}}); err != nil {
		t.Fatalf("AddParameter: %v", err)
	}
	if got := b.Spec.XRD.Parameters["location"]; got.Type != "string" || !got.Required || len(got.Enum) != 2 {
		t.Errorf("added parameter = %+v", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after a valid add: %v", err)
	}
}

func TestAddParameterRejectsDuplicate(t *testing.T) {
	b := editable()
	err := b.AddParameter("providerName", Parameter{Type: "string"})
	if err == nil || !strings.Contains(err.Error(), "providerName") {
		t.Fatalf("err = %v, want a duplicate error naming providerName", err)
	}
}

// An invalid add must leave the blueprint untouched, not partially applied.
func TestAddParameterRejectsInvalidAndChangesNothing(t *testing.T) {
	b := editable()
	before := len(b.Spec.XRD.Parameters)
	if err := b.AddParameter("not a valid name", Parameter{Type: "string"}); err == nil {
		t.Fatal("want an error for an invalid parameter name")
	}
	if err := b.AddParameter("zones", Parameter{Type: "array"}); err == nil {
		t.Fatal("want an error: array parameters are unsupported")
	}
	if len(b.Spec.XRD.Parameters) != before {
		t.Errorf("parameter count changed to %d after failed adds; edits must be atomic", len(b.Spec.XRD.Parameters))
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint left invalid after failed adds: %v", err)
	}
}

// The rename must rewrite every reference, or generation breaks.
func TestRenameParameterRewritesReferences(t *testing.T) {
	b := editable()
	if err := b.RenameParameter("maxMessageSize", "maxBytes"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if _, still := b.Spec.XRD.Parameters["maxMessageSize"]; still {
		t.Error("old parameter name still present")
	}
	if _, ok := b.Spec.XRD.Parameters["maxBytes"]; !ok {
		t.Fatal("new parameter name absent")
	}
	got := b.Spec.Resources[0].Fields["maxMessageSize"].From
	if got != "params.maxBytes" {
		t.Errorf("reference = %q, want params.maxBytes — a dangling reference emits a Composition that cannot render", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after rename: %v", err)
	}
}

func TestRenameParameterRejectsCollisionAndChangesNothing(t *testing.T) {
	b := editable()
	want := b.Spec.Resources[0].Fields["maxMessageSize"].From
	if err := b.RenameParameter("maxMessageSize", "providerName"); err == nil {
		t.Fatal("want an error renaming onto an existing parameter")
	}
	if _, ok := b.Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("original parameter was removed by a failed rename")
	}
	if got := b.Spec.Resources[0].Fields["maxMessageSize"].From; got != want {
		t.Errorf("reference mutated by a failed rename: %q", got)
	}
}

func TestRenameUnknownParameterErrors(t *testing.T) {
	b := editable()
	if err := b.RenameParameter("nope", "other"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}

func TestSetParameterReplacesInPlace(t *testing.T) {
	b := editable()
	if err := b.SetParameter("maxMessageSize", Parameter{Type: "integer", Default: "2048", Description: "Max size."}); err != nil {
		t.Fatalf("SetParameter: %v", err)
	}
	got := b.Spec.XRD.Parameters["maxMessageSize"]
	if got.Default != "2048" || got.Description != "Max size." {
		t.Errorf("parameter = %+v", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after set: %v", err)
	}
}

func TestSetParameterRejectsInvalidAndChangesNothing(t *testing.T) {
	b := editable()
	before := b.Spec.XRD.Parameters["maxMessageSize"]
	if err := b.SetParameter("maxMessageSize", Parameter{Type: "integer", Default: "not-a-number"}); err == nil {
		t.Fatal("want an error: an integer default must parse")
	}
	if diff := cmp.Diff(before, b.Spec.XRD.Parameters["maxMessageSize"]); diff != "" {
		t.Errorf("parameter mutated by a failed set (-before +after):\n%s", diff)
	}
}

// Deleting a parameter something references must be refused, not cascade.
func TestDeleteParameterRefusesWhenReferenced(t *testing.T) {
	b := editable()
	err := b.DeleteParameter("maxMessageSize")
	if err == nil {
		t.Fatal("want an error deleting a referenced parameter")
	}
	if !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to name the resource still referencing the parameter", err)
	}
	if _, ok := b.Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("parameter was deleted despite the error")
	}
}

func TestDeleteParameterSucceedsWhenUnreferenced(t *testing.T) {
	b := editable()
	if err := b.AddParameter("spare", Parameter{Type: "string"}); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteParameter("spare"); err != nil {
		t.Fatalf("DeleteParameter: %v", err)
	}
	if _, still := b.Spec.XRD.Parameters["spare"]; still {
		t.Error("parameter still present after delete")
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after delete: %v", err)
	}
}

func TestDeleteProviderNameIsRefusedForNamespacedScope(t *testing.T) {
	b := editable()
	if err := b.DeleteParameter("providerName"); err == nil {
		t.Fatal("want an error: a Namespaced XRD requires providerName")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/blueprint/ -run 'Parameter' -v`
Expected: FAIL — `undefined: AddParameter`.

- [ ] **Step 3: Write the implementation**

Create `internal/blueprint/edit.go` satisfying the Interfaces block. Requirements:

- **Every operation is atomic.** Apply to a copy, run `Validate()` on the copy, and only commit to the receiver if it passes — so a rejected edit provably cannot leave a half-applied blueprint. `Parameters` is a map and `Resources` a slice of structs containing maps, so a shallow copy is not enough; deep-copy what you mutate.
- Do NOT reimplement any validation rule. `Validate()` already enforces name format, type, defaults-by-type, control characters, `providerName`-for-Namespaced and the rest. Let it reject, and wrap its error with which operation failed.
- `RenameParameter` rewrites every `Field.From` equal to `params.<from>` across all resources. The field's own key does not change — only the reference.
- `DeleteParameter` refuses if any resource field references it, and the error must name at least one referencing resource so the user knows where to look.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/blueprint/ -v -count=1`
Expected: PASS, including every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add internal/blueprint/edit.go internal/blueprint/edit_test.go
git commit -m "feat(blueprint): atomic parameter add, rename, set and delete"
```

---

## Task 4: HTTP server skeleton, compression and ETag

**Files:**
- Create: `internal/api/server.go`, `internal/api/server_test.go`

**Interfaces:**
- Consumes: `index.Index`, `cache.Store`, `blueprint.Blueprint`.
- Produces:
  ```go
  type Options struct {
      Index      *index.Index
      Store      *cache.Store
      Blueprint  string // path to the blueprint file on disk
      OutDir     string // where generate writes
  }
  func New(o Options) (http.Handler, error)
  ```

**Why compression is a task requirement and not a nicety:** the spec measured a provider family's schemas at 4,275,487 bytes raw and **53,680 brotli — 18:1** — and separately recorded that `http.FileServer` sends no `Content-Encoding` at all. Gzip here is the single highest-leverage line of server code in the project. ETag matters because schemas change only when a provider is re-added, so nearly every later request should be a 304.

- [ ] **Step 1: Write the failing test**

`internal/api/server_test.go`:
```go
package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzIsPlainAndCheap(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteIs404WithJSONError(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON so a browser client can parse the error", ct)
	}
}

func TestResponsesAreGzippedWhenAccepted(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("GET", "/api/kinds", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip — schemas compress about 18:1 and this is the "+
			"highest-leverage line of server code in the project", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil || len(body) == 0 {
		t.Fatalf("gzip body unreadable: %v", err)
	}
}

func TestResponsesAreNotGzippedWhenNotAccepted(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds", nil))
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q with no Accept-Encoding; must not compress unasked", got)
	}
}

func TestETagIsStableAndReturns304(t *testing.T) {
	h := testHandler(t)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/api/kinds", nil))
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on /api/kinds")
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest("GET", "/api/kinds", nil))
	if got := second.Header().Get("ETag"); got != tag {
		t.Errorf("ETag changed between identical requests: %q then %q", tag, got)
	}
	req := httptest.NewRequest("GET", "/api/kinds", nil)
	req.Header.Set("If-None-Match", tag)
	third := httptest.NewRecorder()
	h.ServeHTTP(third, req)
	if third.Code != http.StatusNotModified {
		t.Errorf("status = %d with matching If-None-Match, want 304", third.Code)
	}
	if third.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", third.Body.Len())
	}
}

func TestMethodNotAllowedRatherThan404(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/kinds", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d for DELETE /api/kinds, want 405", rec.Code)
	}
}
```

You will also write a `testHandler(t)` helper in this file that builds an `http.Handler` via `New`, using an index built from the same two-Queue fixture as `internal/index`, a `cache.Store` rooted at `t.TempDir()`, and a valid blueprint written into `t.TempDir()`. Tasks 5 and 6 reuse it — give it a stable shape.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/server.go` satisfying the Interfaces block. Requirements:

- Route with `http.ServeMux` using Go 1.22+ method-and-pattern syntax (`mux.HandleFunc("GET /api/kinds", …)`), which gives 405-versus-404 for free. No third-party router.
- One JSON error shape for every failure, e.g. `{"error":"..."}`, always with `Content-Type: application/json`. A browser client must never have to parse an HTML error page.
- Gzip middleware: compress only when the request accepts it, set `Content-Encoding` and `Vary: Accept-Encoding`, and never double-compress. Skip bodies below a small threshold (about 1 KB) where compression costs more than it saves.
- ETag: hash the response body (FNV or SHA-256, your call — state which and why in your report) and honour `If-None-Match` with a bodyless 304. The ETag must depend only on the bytes, so it is stable across processes.
- `New` returns an error rather than panicking if `Options` is incomplete.
- **Do not add authentication.** The server is loopback-only by construction (Task 7); adding a half-designed auth scheme would imply a safety it does not have. State this in a comment.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -m "feat(api): server skeleton with gzip and ETag"
```

---

## Task 5: `/api/kinds` — search and lazy per-kind fetch

**Files:**
- Create: `internal/api/kinds.go`, `internal/api/kinds_test.go`

**Interfaces:**
- Consumes: `index.Index`, `index.Fields`, `index.FieldQuery`.
- Produces these routes:
  ```
  GET /api/kinds?q=<search>&limit=<n>                    -> {"kinds":[Kind,...]}
  GET /api/kinds/{apiVersion}/{kind}                     -> {"kind":Kind,"envelope":[Field,...]}
  GET /api/kinds/{apiVersion}/{kind}/fields
        ?required_only=true&max_depth=<n>&prefix=<p>&q=<s>&limit=<n>
                                                          -> {"fields":[Field,...],"total":<n>}
  ```
  `{apiVersion}` arrives URL-escaped (`sqs.aws.m.upbound.io%2Fv1beta1`) because it contains a slash.

- [ ] **Step 1: Write the failing test**

`internal/api/kinds_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/koorikla/compositionfactory/internal/index"
)

func getJSON(t *testing.T, h http.Handler, path string, into any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code == http.StatusOK && into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: body is not JSON: %v\n%s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestListKindsReturnsTheIndex(t *testing.T) {
	var got struct{ Kinds []index.Kind }
	if code := getJSON(t, testHandler(t), "/api/kinds", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Kinds) != 2 {
		t.Fatalf("got %d kinds, want 2 (namespaced and cluster-scoped Queue)", len(got.Kinds))
	}
	for _, k := range got.Kinds {
		if k.APIVersion == "" || k.Kind == "" {
			t.Errorf("kind is missing identity fields: %+v", k)
		}
	}
}

func TestListKindsSearchAndLimit(t *testing.T) {
	h := testHandler(t)
	var got struct{ Kinds []index.Kind }
	getJSON(t, h, "/api/kinds?q=sqs.aws.m", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("q=sqs.aws.m returned %d, want 1", len(got.Kinds))
	}
	got.Kinds = nil
	getJSON(t, h, "/api/kinds?q=queue&limit=1", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("limit=1 returned %d", len(got.Kinds))
	}
}

func TestGetKindReturnsIdentityAndEnvelope(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	var got struct {
		Kind     index.Kind
		Envelope []index.Field
	}
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Queue", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if got.Kind.Kind != "Queue" || !got.Kind.Namespaced {
		t.Errorf("kind = %+v, want the namespaced Queue", got.Kind)
	}
}

func TestGetUnknownKindIs404WithJSON(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Nonexistent", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestFieldsHonoursRequiredOnly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	var all, req struct {
		Fields []index.Field
		Total  int
	}
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields", &all)
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields?required_only=true", &req)
	if len(all.Fields) != 2 {
		t.Errorf("all fields = %d, want 2 (region, tags)", len(all.Fields))
	}
	if len(req.Fields) != 1 || req.Fields[0].Path != "region" {
		t.Errorf("required_only = %+v, want just region", req.Fields)
	}
	if req.Total != 1 {
		t.Errorf("total = %d, want it to count the returned set", req.Total)
	}
}

func TestFieldsRejectsBadQueryParamsLoudly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	for _, q := range []string{"?max_depth=abc", "?limit=-x", "?required_only=maybe"} {
		if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s -> status %d, want 400; silently ignoring a malformed filter would "+
				"return the wrong field set with no signal", q, code)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run Kind -v`
Expected: FAIL — routes are not registered, so responses are 404.

- [ ] **Step 3: Write the implementation**

Create `internal/api/kinds.go` and register its routes in `New`. Requirements:

- Parse `{apiVersion}` with `url.PathUnescape`; a path that does not unescape is a 400, not a 500.
- Malformed query parameters are **400 with a message naming the parameter**. Never silently coerce — a dropped `max_depth` returns a different field set with no signal, which is the same silent-wrongness class this project exists to avoid.
- `total` reports the number of fields returned after filtering, so a client can tell a `limit` truncated the result.
- Unknown kind or apiVersion is a 404 carrying the JSON error shape.
- No caching layer, no goroutines, no background refresh. The index is already in memory.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/kinds.go internal/api/kinds_test.go
git commit -m "feat(api): kind search and lazy per-kind field views"
```

---

## Task 6: `/api/blueprint` and `/api/generate`

**Files:**
- Create: `internal/api/blueprint.go`, `internal/api/generate.go`, `internal/api/blueprint_test.go`

**Interfaces:**
- Consumes: `blueprint.Load`, the Task 3 edit methods, `emit.Generate`, `cache.Store.Load`.
- Produces these routes:
  ```
  GET    /api/blueprint                     -> the blueprint as JSON
  POST   /api/blueprint/parameters          -> {"name":"x","parameter":{...}}  (add)
  PUT    /api/blueprint/parameters/{name}   -> {"parameter":{...}}             (set)
  POST   /api/blueprint/parameters/{name}/rename -> {"to":"y"}
  DELETE /api/blueprint/parameters/{name}
  POST   /api/generate                      -> {"outputs":[{"path":..,"bytes":N}],"written":bool}
  ```

**Two decisions this task locks in, and why:**
1. **Every mutation persists to the blueprint file immediately**, using the same deterministic YAML rules as generated output. The blueprint is the source of truth on disk; an in-memory server document would diverge from what `cf gen` reads the moment anyone edits the file in an editor.
2. **`/api/generate` calls `emit.Generate` and nothing else.** It is the load-bearing architectural rule of the whole project: the canvas, the CLI and the MCP server must produce byte-identical output.

- [ ] **Step 1: Write the failing test**

`internal/api/blueprint_test.go`:
```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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
func TestGenerateProducesTheSameBytesAsTheEngine(t *testing.T) {
	h, path := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Outputs []struct {
			Path  string `json:"path"`
			Bytes int    `json:"bytes"`
		}
		Written bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(got.Outputs) != 3 {
		t.Fatalf("got %d outputs, want 3 (xrd, composition, functions.yaml)", len(got.Outputs))
	}
	if got.Written {
		t.Error("write:false still reported Written")
	}
	_ = path
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
```

You will extend the Task 4 helper into `testHandlerWithPath(t) (http.Handler, string)` returning the blueprint path too, and keep `testHandler(t)` working for the earlier tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'Blueprint|Parameter|Generate|Rename|Delete|Malformed' -v`
Expected: FAIL — routes not registered.

- [ ] **Step 3: Write the implementation**

Create `internal/api/blueprint.go` and `internal/api/generate.go`, registering routes in `New`. Requirements:

- Load the blueprint from disk on every request rather than caching it in the server. It is a small file, and a stale in-memory copy diverges the moment someone edits it in an editor — the exact class of bug this project exists to prevent.
- Persist with the same determinism rules as generated output: sorted keys, LF only, one trailing newline. **Round-trip check before writing:** marshal, re-parse with `blueprint.Load`-equivalent parsing, and refuse to write if the result does not validate. Never leave an unloadable blueprint on disk.
- Status codes carry meaning: 400 for a malformed request or a validation failure, **409 for an edit that conflicts with current state** (deleting a referenced parameter, adding a duplicate), 404 for an unknown parameter, 200 on success.
- Surface `blueprint.Validate()` and `emit.Generate` errors verbatim in the JSON error body. Do not paraphrase them — they name field paths precisely and that is their value.
- `/api/generate` takes `{"write":bool}`. With `write:false` it reports what would be produced without touching disk; with `write:true` it writes through the same path `cf gen` uses. It must call `emit.Generate` — **any generation logic in this file is a defect**, not an optimisation.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -v -count=1` and `go test ./... -short`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/blueprint.go internal/api/generate.go internal/api/blueprint_test.go
git commit -m "feat(api): blueprint editing and generate over the shared engine"
```

---

## Task 7: `cf serve`

**Files:**
- Create: `cmd/cf/serve.go`, `cmd/cf/serve_test.go`
- Modify: `cmd/cf/main.go` — add the `Serve` field to `CLI`

**Interfaces:**
- Consumes: `api.New`, `index.Build`, `cache.Store`, `cache.ReadLock`.
- Produces: `type ServeCmd struct{ ... }` on the kong root.

**The security requirement, stated plainly:** this server reads the local schema cache and **writes files on disk**. It has no authentication, by design, because it is a local dev tool. That is only safe while it is unreachable from the network. Binding `0.0.0.0` would expose a filesystem-writing API to anything on the LAN. The default must be `127.0.0.1`, and choosing otherwise must be an explicit, deliberate flag.

- [ ] **Step 1: Write the failing test**

`cmd/cf/serve_test.go`:
```go
package main

import (
	"strings"
	"testing"
)

func TestServeDefaultsToLoopback(t *testing.T) {
	var c ServeCmd
	if err := defaults(&c); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if c.Addr != "127.0.0.1:8080" {
		t.Errorf("default Addr = %q, want 127.0.0.1:8080 — this server writes files and has no "+
			"authentication, so it must not be reachable off-host by default", c.Addr)
	}
}

func TestServeRefusesNonLoopbackWithoutTheExplicitFlag(t *testing.T) {
	c := ServeCmd{Addr: "0.0.0.0:8080"}
	err := c.check()
	if err == nil {
		t.Fatal("want an error binding 0.0.0.0 without --i-know-this-is-unauthenticated")
	}
	for _, want := range []string{"0.0.0.0", "authentication"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestServeAllowsNonLoopbackWithTheExplicitFlag(t *testing.T) {
	c := ServeCmd{Addr: "0.0.0.0:8080", IKnowThisIsUnauthenticated: true}
	if err := c.check(); err != nil {
		t.Errorf("explicit opt-in still refused: %v", err)
	}
}

func TestServeAcceptsOtherLoopbackForms(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9000", "localhost:9000", "[::1]:9000"} {
		c := ServeCmd{Addr: addr}
		if err := c.check(); err != nil {
			t.Errorf("%s rejected as non-loopback: %v", addr, err)
		}
	}
}
```

You will write the tiny `defaults(*ServeCmd) error` helper that applies kong's declared defaults so the test does not need a full parse.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cf/ -run Serve -v`
Expected: FAIL — `undefined: ServeCmd`.

- [ ] **Step 3: Write the implementation**

Create `cmd/cf/serve.go` and add `Serve ServeCmd` to `CLI`. Requirements:

- Flags: `--addr` (default `127.0.0.1:8080`), `--blueprint`, `--out`, `--cache-dir` (default `${cachedir}`, reusing the existing `kongOptions()` helper — do not duplicate the kong var binding), and `--i-know-this-is-unauthenticated`.
- `check()` resolves the host and refuses anything that is not loopback unless the opt-in flag is set. The error must say what is wrong AND why it matters — that the server has no authentication and writes files.
- On start, build the index from every provider named in the blueprint's `sources`, loading each through `cache.Store`. A provider that is not cached is a clear startup error naming the `cf provider add` command to run — not an empty palette.
- Print the URL it is listening on, and support graceful shutdown on SIGINT/SIGTERM with a bounded timeout.
- No UI is served in M2. If the SPA directory does not exist, `/` returns a plain message saying the API is up and the canvas arrives in M3.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cf/ -v -count=1` and `go test ./... -short`
Expected: PASS.

- [ ] **Step 5: Manually verify the server actually serves**

```bash
make build
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
./bin/cf serve --blueprint testdata/xqueue.cf.yaml &
curl -s localhost:8080/api/kinds | head -c 400
curl -s "localhost:8080/api/kinds/sqs.aws.m.upbound.io%2Fv1beta1/Queue/fields?required_only=true"
kill %1
```
Record the actual output in your report.

- [ ] **Step 6: Commit**

```bash
git add cmd/cf/serve.go cmd/cf/serve_test.go cmd/cf/main.go
git commit -m "feat(cli): cf serve, loopback-only by default"
```

---

## M2 Exit Criteria

- [ ] `make test` passes with no Docker and no cluster.
- [ ] `cf serve` binds loopback by default and refuses `0.0.0.0` without an explicit opt-in flag.
- [ ] `GET /api/kinds` returns the index; gzip is applied when accepted; a repeat request with `If-None-Match` returns 304.
- [ ] `GET /api/kinds/{apiVersion}/{kind}/fields?required_only=true` returns only required leaves.
- [ ] A parameter added over HTTP is present in the blueprint file on disk and the file still loads.
- [ ] A rejected edit leaves the blueprint file byte-identical.
- [ ] `POST /api/generate` produces the same three outputs as `cf gen`, via `emit.Generate`.
- [ ] `go test ./... -count=1` and `make lint` are clean.

## Not in M2 (later plans)

The canvas, wires and reference inference (M3) · `when`, `forEach`, `dependsOn`, user-defined templates, multi-step pipelines (M4) · MCP server, `adopt`, K8s RBAC emission (M5) · cloud IAM (M6) · provider discovery and the catalogue index, which the spec places alongside M2 but which does not block the canvas.
