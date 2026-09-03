package emit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// prepEnvInput deliberately declares its keys UNSORTED (kind before
// apiVersion, zRef before mode) so the goldens prove the emitter re-encodes
// the parsed input deterministically instead of pasting the string.
const prepEnvInput = "kind: Input\n" +
	"apiVersion: fn.example.org/v1beta1\n" +
	"zRef:\n" +
	"  items:\n" +
	"  - b\n" +
	"  - a\n" +
	"mode: strict\n" +
	"count: 5\n" +
	"quoted: \"yes\"\n"

// pipelineBlueprint is testBlueprint plus a declared pipeline: two before
// steps (one with input, one without), and an explicit auto-ready step after
// the templating step, its package left tag-free exactly as a user pinning by
// digest later would write it.
func pipelineBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "prep-env",
			FunctionRef: "function-environment-configs",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0",
			Position:    blueprint.PositionBefore,
			Input:       prepEnvInput,
		},
		{
			Name:        "shape-status",
			FunctionRef: "function-status-transformer",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-status-transformer:v0.4.0",
			Position:    blueprint.PositionBefore,
		},
		{
			Name:        "auto-ready",
			FunctionRef: "function-auto-ready",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-auto-ready",
		},
	}
	return b
}

// assertGolden byte-compares got against testdata/<name>, rewriting the file
// when CF_UPDATE_GOLDEN is set. Byte-exact on purpose: determinism is a
// correctness requirement here (a churning file on a prune:true GitOps repo
// is a live-cluster incident), so the golden pins bytes, not structure.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden %s updated", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set CF_UPDATE_GOLDEN=1 to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s (-want +got):\n%s", path,
			cmp.Diff(string(want), string(got)))
	}
}

func TestPipelineCompositionGolden(t *testing.T) {
	got, err := Composition(pipelineBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	assertGolden(t, "pipeline_composition.golden.yaml", got)
}

func TestPipelineFunctionsGolden(t *testing.T) {
	got, err := Functions(pipelineBlueprint())
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	assertGolden(t, "pipeline_functions.golden.yaml", got)
}

// TestPipelineStepOrder proves placement structurally: before-steps in
// declaration order, then the templating step, then after-steps in
// declaration order -- by decoding the emitted document, not string-matching.
func TestPipelineStepOrder(t *testing.T) {
	got, err := Composition(pipelineBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	var doc struct {
		Spec struct {
			Pipeline []struct {
				Step        string `json:"step"`
				FunctionRef struct {
					Name string `json:"name"`
				} `json:"functionRef"`
			} `json:"pipeline"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(got, &doc); err != nil {
		t.Fatalf("emitted Composition is not valid YAML: %v\n---\n%s", err, got)
	}
	var steps, fns []string
	for _, s := range doc.Spec.Pipeline {
		steps = append(steps, s.Step)
		fns = append(fns, s.FunctionRef.Name)
	}
	wantSteps := []string{"prep-env", "shape-status", blueprint.TemplatingStepName, "auto-ready"}
	wantFns := []string{"function-environment-configs", "function-status-transformer",
		blueprint.TemplatingFunctionName, "function-auto-ready"}
	if diff := cmp.Diff(wantSteps, steps); diff != "" {
		t.Errorf("step order (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantFns, fns); diff != "" {
		t.Errorf("functionRef order (-want +got):\n%s", diff)
	}
}

// TestPipelineInputIsReencodedNotPasted decodes the emitted step input and
// requires it to be semantically identical to the blueprint's raw input --
// same keys, same values, same TYPES (count stays an integer, "yes" stays a
// string) -- while the goldens above pin that the emitted bytes are the
// sorted, deterministic rendering rather than the user's own key order.
func TestPipelineInputIsReencodedNotPasted(t *testing.T) {
	got, err := Composition(pipelineBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(got, &doc); err != nil {
		t.Fatalf("emitted Composition is not valid YAML: %v", err)
	}
	emitted := dig(t, doc, "spec", "pipeline", 0, "input")
	want, err := blueprint.ParsePipelineInput(prepEnvInput)
	if err != nil {
		t.Fatalf("fixture input does not parse: %v", err)
	}
	if diff := cmp.Diff(any(want), emitted); diff != "" {
		t.Errorf("emitted input is not semantically identical to the declared one (-want +got):\n%s", diff)
	}

	// And the bytes are the canonical ordering, not the declared one: within
	// the input block, apiVersion must precede kind even though the user
	// wrote kind first.
	s := string(got)
	iAPI := strings.Index(s, "apiVersion: fn.example.org/v1beta1")
	iKind := strings.Index(s, "kind: Input")
	if iAPI == -1 || iKind == -1 {
		t.Fatalf("input block missing from emitted Composition\n---\n%s", s)
	}
	if iAPI > iKind {
		t.Error("input keys kept the user's declaration order; they must be re-encoded sorted")
	}
}

// TestFunctionsOnePerDistinctFunctionRef: two steps backed by the same
// function+package yield ONE Function document, and the built-in templating
// function is always declared first.
func TestFunctionsOnePerDistinctFunctionRef(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
			Input: "apiVersion: fn.example.org/v1\nkind: Input\nmode: a\n"},
		{Name: "step-b", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
			Input: "apiVersion: fn.example.org/v1\nkind: Input\nmode: b\n"},
	}
	got, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	s := string(got)
	if n := strings.Count(s, "kind: Function"); n != 2 {
		t.Errorf("want 2 Function documents (templating + one for function-x), got %d\n---\n%s", n, s)
	}
	if n := strings.Count(s, "name: function-x"); n != 1 {
		t.Errorf("function-x declared %d times, want once", n)
	}
	if !strings.Contains(s, "package: example.org/fn-x:v1") {
		t.Errorf("functions.yaml must carry the step's package verbatim\n---\n%s", s)
	}
	if !strings.Contains(s, "name: "+blueprint.TemplatingFunctionName) {
		t.Errorf("the built-in templating function must always be declared\n---\n%s", s)
	}
}

// TestDeclaredPipelineReplacesTheDefaultAutoReady: a blueprint that declares
// any pipeline owns the whole pipeline -- the default auto-ready step and its
// default Function must NOT be smuggled in alongside.
func TestDeclaredPipelineReplacesTheDefaultAutoReady(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{Name: "only-step", FunctionRef: "function-x", Package: "example.org/fn-x:v1"},
	}
	comp, err := Composition(b, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if strings.Contains(string(comp), "auto-ready") {
		t.Errorf("declared pipeline must replace the default auto-ready step, not join it\n---\n%s", comp)
	}
	fns, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	if strings.Contains(string(fns), "function-auto-ready") {
		t.Errorf("functions.yaml must not declare the default auto-ready Function for a declared pipeline\n---\n%s", fns)
	}
}

// TestDefaultPipelineStillEmitsAutoReady pins backward compatibility: a
// blueprint with no spec.pipeline keeps today's two-step pipeline exactly.
func TestDefaultPipelineStillEmitsAutoReady(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	for _, want := range []string{"- step: auto-ready", "name: function-auto-ready"} {
		if !strings.Contains(s, want) {
			t.Errorf("default pipeline lost its auto-ready step: missing %q\n---\n%s", want, s)
		}
	}
}

// TestPipelineGenerateIsDeterministic: two runs over the pipelined blueprint
// byte-compare equal, input re-encoding included.
func TestPipelineGenerateIsDeterministic(t *testing.T) {
	a, err := Generate(pipelineBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (first run): %v", err)
	}
	b, err := Generate(pipelineBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (second run): %v", err)
	}
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("file counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || !bytes.Equal(a[i].Body, b[i].Body) {
			t.Fatalf("output %q differs between runs", a[i].Path)
		}
	}
}

// TestPipelineTemplateStillRenders: the declared pipeline must not disturb
// the templating step itself -- the emitted template body still executes
// under missingkey=error.
func TestPipelineTemplateStillRenders(t *testing.T) {
	got, err := Composition(pipelineBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(got, &doc); err != nil {
		t.Fatalf("emitted Composition is not valid YAML: %v", err)
	}
	// The templating step is now index 2 (after two before-steps).
	tmpl, ok := dig(t, doc, "spec", "pipeline", 2, "input", "inline", "template").(string)
	if !ok {
		t.Fatal("templating step's inline template missing")
	}
	rendered, err := renderTemplate(t, tmpl, map[string]any{"providerName": "aws-provider"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(rendered, "kind: Queue") {
		t.Errorf("rendered template lost its composed resource\n---\n%s", rendered)
	}
}

func functionCRDs() []schema.CRD {
	return []schema.CRD{
		{
			Group:    "autoready.fn.crossplane.io",
			Kind:     "AutoReady",
			Plural:   "autoreadies",
			Scope:    "Namespaced",
			Function: true,
			Versions: []schema.Version{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Properties: map[string]any{
					"apiVersion": map[string]any{"type": "string"},
					"kind":       map[string]any{"type": "string"},
					"ignore": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			}},
		},
		{
			Group:    "gotemplating.fn.crossplane.io",
			Kind:     "GoTemplate",
			Plural:   "gotemplates",
			Scope:    "Namespaced",
			Function: true,
			Versions: []schema.Version{{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Properties: map[string]any{
					"apiVersion": map[string]any{"type": "string"},
					"kind":       map[string]any{"type": "string"},
					"source": map[string]any{
						"type": "string",
						"enum": []any{"Inline", "FileSystem"},
					},
					"inline": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"template": map[string]any{"type": "string"},
						},
					},
				},
			}},
		},
	}
}

func TestValidatePipelineInputs_ValidInput(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "template-step",
			FunctionRef: "function-go-templating",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.4.1",
			Input: "apiVersion: gotemplating.fn.crossplane.io/v1beta1\n" +
				"kind: GoTemplate\n" +
				"source: Inline\n" +
				"inline:\n" +
				"  template: \"{{ . }}\"\n",
		},
	}
	crds := append(testCRDs(t), functionCRDs()...)
	warnings, err := ValidatePipelineInputs(b, crds)
	if err != nil {
		t.Fatalf("ValidatePipelineInputs: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

func TestValidatePipelineInputs_UnknownFieldSuggestsNearest(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "template-step",
			FunctionRef: "function-go-templating",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.4.1",
			Input: "apiVersion: gotemplating.fn.crossplane.io/v1beta1\n" +
				"kind: GoTemplate\n" +
				"sourc: Inline\n", // typo: sourc instead of source
		},
	}
	crds := append(testCRDs(t), functionCRDs()...)
	_, err := ValidatePipelineInputs(b, crds)
	if err == nil {
		t.Fatal("expected error for unknown field 'sourc', got nil")
	}
	if !strings.Contains(err.Error(), `field "sourc" is not in GoTemplate schema; did you mean "source"?`) {
		t.Errorf("error %q does not contain nearest-match suggestion", err.Error())
	}
}

func TestValidatePipelineInputs_UncachedFunctionProducesWarning(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "custom-fn",
			FunctionRef: "function-custom",
			Package:     "example.org/function-custom:v1.0.0",
			Input: "apiVersion: custom.fn.example.org/v1\n" +
				"kind: CustomInput\n" +
				"foo: bar\n",
		},
	}
	crds := testCRDs(t) // no function CRDs in cache
	warnings, err := ValidatePipelineInputs(b, crds)
	if err != nil {
		t.Fatalf("ValidatePipelineInputs should accept uncached function with warning, got err: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected explicit warning for uncached function, got none")
	}
	if !strings.Contains(warnings[0], "custom.fn.example.org/v1 CustomInput") || !strings.Contains(warnings[0], "not cached") {
		t.Errorf("warning %q does not match expected format", warnings[0])
	}
}

func TestValidatePipelineInputs_EnumValidation(t *testing.T) {
	b := testBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "template-step",
			FunctionRef: "function-go-templating",
			Package:     "xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.4.1",
			Input: "apiVersion: gotemplating.fn.crossplane.io/v1beta1\n" +
				"kind: GoTemplate\n" +
				"source: InvalidSource\n",
		},
	}
	crds := append(testCRDs(t), functionCRDs()...)
	_, err := ValidatePipelineInputs(b, crds)
	if err == nil {
		t.Fatal("expected error for invalid enum value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid value \"InvalidSource\"") {
		t.Errorf("error %q does not mention invalid enum value", err.Error())
	}
}
