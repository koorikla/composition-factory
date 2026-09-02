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

// chainTree exercises every effective-requiredness case on managed (strict)
// semantics: a required root leaf, a required-in-required leaf, a
// required-in-OPTIONAL leaf (raw-required but not effectively required), and
// a required branch none of whose leaves are chain-required.
func chainTree(t *testing.T) []*schema.Node {
	t.Helper()
	props := map[string]any{
		"region": map[string]any{"type": "string"},
		"config": map[string]any{
			"type": "object", "required": []any{"endpoint"},
			"properties": map[string]any{
				"endpoint": map[string]any{"type": "string"},
				"timeout":  map[string]any{"type": "integer"},
			},
		},
		"optionalBlock": map[string]any{
			"type": "object", "required": []any{"inner"},
			"properties": map[string]any{
				"inner": map[string]any{"type": "string"},
			},
		},
		"selector": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"matchLabels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
		},
	}
	nodes := schema.BuildTree(props, []string{"region", "config", "selector"})
	schema.ComputeRequiredChain(nodes, false)
	return nodes
}

// RequiredOnly runs on the required CHAIN, not the raw flag: a raw-required
// leaf inside an optional block (optionalBlock.inner — the EnvVar.name
// pattern that made the old filter noise) stays out, while a required leaf
// whose ancestors are all required (config.endpoint) and a required root
// leaf (region) stay in.
func TestRequiredOnlyKeepsChainRequiredLeaves(t *testing.T) {
	got := paths(Fields(chainTree(t), FieldQuery{RequiredOnly: true}))
	want := []string{"config.endpoint", "region"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("RequiredOnly (-want +got):\n%s\n(chain-required leaves only: required-in-optional is raw noise)", diff)
	}

	// The raw flag still travels on every row, untouched: optionalBlock.inner
	// keeps required=true with requiredChain=false.
	for _, f := range Fields(chainTree(t), FieldQuery{}) {
		if f.Path == "optionalBlock.inner" {
			if !f.Required || f.RequiredChain {
				t.Errorf("optionalBlock.inner: required=%v requiredChain=%v, want raw true and chain false",
					f.Required, f.RequiredChain)
			}
		}
	}

	// An UNANNOTATED tree (built directly, never handed out by a CRD method)
	// has no chain at all, so RequiredOnly keeps nothing rather than falling
	// back to the raw flag.
	if got := paths(Fields(deepTree(t), FieldQuery{RequiredOnly: true})); len(got) != 0 {
		t.Errorf("RequiredOnly on an unannotated tree = %v, want nothing", got)
	}
}

// A chain-required branch with no chain-required leaves (selector) surfaces
// through RequiredBranches; a required branch whose leaves already carry the
// chain (config -> config.endpoint) does not — its leaves surface it.
func TestRequiredBranchesSurfaceLeaflessRequiredSubtrees(t *testing.T) {
	got := paths(RequiredBranches(chainTree(t)))
	want := []string{"selector"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("RequiredBranches (-want +got):\n%s", diff)
	}
	rows := RequiredBranches(chainTree(t))
	if len(rows) == 1 {
		r := rows[0]
		if !r.Required || !r.RequiredChain || r.Type != "object" || r.Depth != 0 {
			t.Errorf("selector branch row = %+v, want required, chain-required, object, depth 0", r)
		}
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

// TestPrefixMatchesALeafsOwnPathExactly pins exact-path lookup: a Prefix that
// names a leaf itself (not a branch above it) returns just that leaf. This
// is safe because a schema.Node is either a leaf or a branch, never both, so
// a branch-shaped Prefix can never collide with a real leaf path — but the
// inspector uses this exact form for single-field lookup, so the behavior
// needs to be pinned rather than left as an untested side effect of the
// path-segment-boundary check.
func TestPrefixMatchesALeafsOwnPathExactly(t *testing.T) {
	got := paths(Fields(deepTree(t), FieldQuery{Prefix: "replicas"}))
	want := []string{"replicas"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Prefix=leaf's own path (-want +got):\n%s", diff)
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

// TestNegativeLimitIsUnlimited pins Limit<=0 as "unlimited", matching the
// documented behavior of MaxDepth's zero value. A malformed limit string
// (e.g. "abc") is Task 5's job to reject with 400 at the HTTP boundary,
// before it ever becomes an int; a negative int reaching this function is
// therefore never the result of that rejected input; it's a caller's
// legitimate way to ask for "no limit" internally. Do not make negative
// values an error here — that would fight the boundary validation rather
// than complement it.
func TestNegativeLimitIsUnlimited(t *testing.T) {
	unlimited := Fields(deepTree(t), FieldQuery{Limit: 0})
	negative := Fields(deepTree(t), FieldQuery{Limit: -1})
	if len(unlimited) != 4 {
		t.Fatalf("Limit=0 returned %d fields, want 4 (sanity check)", len(unlimited))
	}
	if diff := cmp.Diff(unlimited, negative); diff != "" {
		t.Errorf("Limit=-1 (-Limit=0 +Limit=-1):\n%s", diff)
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

func TestFieldsMetadataExposed(t *testing.T) {
	minVal := 1.0
	maxVal := 100.0
	props := map[string]any{
		"env": map[string]any{
			"type":    "string",
			"enum":    []any{"dev", "prod"},
			"default": "dev",
		},
		"port": map[string]any{
			"type":    "integer",
			"minimum": minVal,
			"maximum": maxVal,
			"format":  "int32",
		},
	}
	nodes := schema.BuildTree(props, nil)
	fields := Fields(nodes, FieldQuery{})

	if len(fields) != 2 {
		t.Fatalf("len(fields) = %d, want 2", len(fields))
	}

	for _, f := range fields {
		if f.Path == "env" {
			if len(f.Enum) != 2 || f.Enum[0] != "dev" || f.Enum[1] != "prod" {
				t.Errorf("env Enum = %+v, want [dev, prod]", f.Enum)
			}
			if f.Default != "dev" {
				t.Errorf("env Default = %v, want dev", f.Default)
			}
		}
		if f.Path == "port" {
			if f.Minimum == nil || *f.Minimum != 1.0 {
				t.Errorf("port Minimum = %v, want 1.0", f.Minimum)
			}
			if f.Maximum == nil || *f.Maximum != 100.0 {
				t.Errorf("port Maximum = %v, want 100.0", f.Maximum)
			}
			if f.Format != "int32" {
				t.Errorf("port Format = %q, want int32", f.Format)
			}
		}
	}
}
