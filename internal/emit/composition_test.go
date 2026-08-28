package emit

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

func testCRDs(t *testing.T) []schema.CRD {
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
                  maxMessageSize: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
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
                properties: {region: {type: string}}
              deletionPolicy: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

func TestCompositionSelectsNamespacedVariant(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "apiVersion: sqs.aws.m.upbound.io/v1beta1") {
		t.Errorf("did not select the .m. namespaced variant\n---\n%s", s)
	}
	if strings.Contains(s, "apiVersion: sqs.aws.upbound.io/v1beta1") {
		t.Error("emitted the legacy cluster-scoped variant for a Namespaced XRD")
	}
}

// The single most important assertion in this package.
func TestOptionsIsTopLevelNotNestedUnderInline(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	lines := strings.Split(string(got), "\n")
	var optIndent, inlineIndent int = -1, -1
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		indent := len(l) - len(trimmed)
		if strings.HasPrefix(trimmed, "options:") && optIndent == -1 {
			optIndent = indent
		}
		if strings.HasPrefix(trimmed, "inline:") && inlineIndent == -1 {
			inlineIndent = indent
		}
	}
	if optIndent == -1 {
		t.Fatal("no options: key; missingkey=error must always be emitted")
	}
	if inlineIndent == -1 {
		t.Fatal("no inline: key")
	}
	if optIndent != inlineIndent {
		t.Errorf("options: is indented %d and inline: is %d — options must be a SIBLING of inline, "+
			"not nested inside it (nesting is a fatal error at runtime)", optIndent, inlineIndent)
	}
	if !strings.Contains(string(got), "missingkey=error") {
		t.Error("missingkey=error missing; without it a missing field renders the string <no value>")
	}
}

func TestOptionalFieldIsGuarded(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	// NOT a `{{- with $spec.maxMessageSize }}` guard: under
	// options: ["missingkey=error"], `with` evaluates the pipeline (indexing
	// the map) before deciding truthiness, so a genuinely absent optional
	// key hard-fails the whole render instead of being gracefully omitted.
	// hasKey performs the presence check inside the function argument,
	// sidestepping the template engine's own strict indexing. See
	// TestOptionalFieldRendersUnderMissingKeyError, which proves this by
	// actually executing the template rather than string-matching.
	if !strings.Contains(s, `{{- if hasKey $spec "maxMessageSize" }}`) {
		t.Errorf("optional field not wrapped in a hasKey guard\n---\n%s", s)
	}
	if strings.Contains(s, "{{- with $spec.maxMessageSize }}") {
		t.Error("optional field uses a with-guard, which hard-fails the render under " +
			"missingkey=error when the key is genuinely absent")
	}
}

// extractTemplate decodes the emitted Composition and returns the Go-template
// body at spec.pipeline[0].input.inline.template — the block scalar that
// function-go-templating renders into the final managed-resource document.
func extractTemplate(t *testing.T, doc []byte) string {
	t.Helper()
	var parsed map[string]any
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("emitted document is not valid YAML: %v\n---\n%s", err, doc)
	}
	tmpl := dig(t, parsed, "spec", "pipeline", 0, "input", "inline", "template")
	s, ok := tmpl.(string)
	if !ok {
		t.Fatalf("template body: expected a string, got %T (%v)", tmpl, tmpl)
	}
	return s
}

// renderTemplate executes the emitted Go-template body the way
// function-go-templating actually does: with Option("missingkey=error") and
// the real sprig hasKey semantics (a plain map presence check). It also
// stubs setResourceNameAnnotation, a function-go-templating builtin, just
// enough that the surrounding template parses and executes — its exact
// rendered form isn't what this test is checking.
func renderTemplate(t *testing.T, tmplBody string, xrSpec map[string]any) (string, error) {
	t.Helper()
	return renderTemplateObserved(t, tmplBody, xrSpec, nil)
}

// renderTemplateObserved is renderTemplate with observed composed resources:
// observedResources becomes .observed.resources, keyed by
// composition-resource-name, each entry carrying the object under .resource
// — the shape function-go-templating hands the template. nil means the
// resources key is ABSENT entirely (what protojson produces for an empty
// map), which is exactly the case a status-wire guard must survive.
func renderTemplateObserved(t *testing.T, tmplBody string, xrSpec map[string]any, observedResources map[string]any) (string, error) {
	t.Helper()
	funcs := template.FuncMap{
		"hasKey": func(d map[string]any, key string) bool {
			_, ok := d[key]
			return ok
		},
		// sprig's kindIs, verbatim semantics: the reflect kind name of the
		// value compared to the target string. reflect.ValueOf(nil) is the
		// Invalid kind ("invalid"), never a panic.
		"kindIs": func(kind string, v any) bool {
			return reflect.ValueOf(v).Kind().String() == kind
		},
		"setResourceNameAnnotation": func(name string) string {
			return "crossplane.io/composition-resource-name: " + name
		},
	}
	tmpl, err := template.New("t").Option("missingkey=error").Funcs(funcs).Parse(tmplBody)
	if err != nil {
		t.Fatalf("template body does not parse: %v\n---\n%s", err, tmplBody)
	}
	observed := map[string]any{
		"composite": map[string]any{
			"resource": map[string]any{
				"metadata": map[string]any{"name": "my-xqueue"},
				"spec":     xrSpec,
			},
		},
	}
	if observedResources != nil {
		observed["resources"] = observedResources
	}
	data := map[string]any{"observed": observed}
	var out bytes.Buffer
	err = tmpl.Execute(&out, data)
	return out.String(), err
}

// This is the test that matters: it proves the emitted template actually
// survives options: ["missingkey=error"] for an optional field under both
// XR shapes, by really executing it with Go's text/template — not just that
// some guard string appears. A `{{- with $spec.x }}` guard string-matches
// fine but hard-fails execution the moment the key is genuinely absent; see
// the "Fix round 1" section of task-9-report.md for the reproduction that
// caught this.
func TestOptionalFieldRendersUnderMissingKeyError(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("key present renders the value", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName":   "aws-provider",
			"maxMessageSize": 2048,
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		if !strings.Contains(rendered, "maxMessageSize: 2048") {
			t.Errorf("optional field present on the XR did not render\n---\n%s", rendered)
		}
	})

	t.Run("key absent renders successfully and omits the field", func(t *testing.T) {
		// maxMessageSize is intentionally absent from xrSpec: the ordinary
		// case for an optional field the user never set. A real API server
		// does not synthesize a key for an unset field with no schema
		// default, so the observed composite's spec genuinely lacks it.
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
		})
		if err != nil {
			t.Fatalf("render must succeed when an optional field is genuinely absent, got: %v\n---\n%s", err, tmplBody)
		}
		if strings.Contains(rendered, "maxMessageSize") {
			t.Errorf("optional field must be omitted entirely when absent, got:\n%s", rendered)
		}
	})
}

// TestForProviderIsEmptyMapNotNullWhenAllOptionalFieldsAbsent proves the
// render-time fix for a bare `forProvider:` key with no children. Every
// field on testBlueprint's only resource is optional (maxMessageSize), so an
// XR that omits it is exactly the case where, without this fix, the emitted
// template would render nothing under forProvider at all. YAML parses that
// as null, and a structural schema with `type: object` and no
// `nullable: true` rejects an explicit null at apply time — not silent
// corruption, but still a generated artifact the API server refuses, which
// this project's determinism/validity rule forbids. A string match on
// "forProvider: {}" would not prove this: it wouldn't catch the parent
// still decoding as null if the fallback line were nested wrong, so this
// executes the real template and decodes the real YAML output, the same way
// TestOptionalFieldRendersUnderMissingKeyError does.
func TestForProviderIsEmptyMapNotNullWhenAllOptionalFieldsAbsent(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	rendered, err := renderTemplate(t, tmplBody, map[string]any{
		"providerName": "aws-provider",
		// maxMessageSize intentionally absent — the only field this
		// resource maps, and it's optional.
	})
	if err != nil {
		t.Fatalf("render must succeed when every optional field is absent, got: %v\n---\n%s", err, tmplBody)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, rendered)
	}
	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec: expected a map, got %T (%v)\n---\n%s", doc["spec"], doc["spec"], rendered)
	}
	fp, present := spec["forProvider"]
	if !present {
		t.Fatalf("forProvider key missing entirely\n---\n%s", rendered)
	}
	if fp == nil {
		t.Fatalf("forProvider decoded as null, not an empty map — a structural schema with "+
			"type: object (no nullable: true) rejects this at apply time\n---\n%s", rendered)
	}
	m, ok := fp.(map[string]any)
	if !ok {
		t.Fatalf("forProvider: expected a map, got %T (%v)\n---\n%s", fp, fp, rendered)
	}
	if len(m) != 0 {
		t.Errorf("forProvider: expected an empty map, got %v\n---\n%s", m, rendered)
	}
}

func TestProviderConfigRefCarriesKindAndName(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "kind: ClusterProviderConfig") || !strings.Contains(s, "name: {{ $spec.providerName }}") {
		t.Errorf("providerConfigRef must carry both kind and name in the v2 namespaced envelope\n---\n%s", s)
	}
}

// The error is asserted, not discarded: a failed Composition returns nil,
// and "does nil contain deletionPolicy" is trivially false, so this test
// passed for the wrong reason the moment Composition started erroring.
func TestNoDeletionPolicyForNamespacedMR(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if strings.Contains(string(got), "deletionPolicy") {
		t.Error("deletionPolicy is absent from the v2 namespaced envelope and would be pruned")
	}
}

func TestResourceNameAnnotationPresent(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if !strings.Contains(string(got), `setResourceNameAnnotation "main-queue"`) {
		t.Error("every composed resource needs a stable composition-resource-name annotation")
	}
}

func TestUnknownKindIsAClearError(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Kind = "Nonexistent"
	_, err := Composition(b, testCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "Nonexistent") {
		t.Fatalf("err = %v, want an error naming the unknown kind", err)
	}
}

// --- Final review: artifact-level proofs ---
//
// The tests below deliberately assert on the ARTIFACT, not on which layer
// refuses to produce it: "either Generate rejects this blueprint, or the
// documents it produced are structurally exactly what we meant to write."
// Both halves are checked by parsing the emitted YAML with sigs.k8s.io/yaml
// and inspecting the decoded structure -- a string match would prove nothing
// about YAML semantics, which is the whole point of these defects. Framed
// this way, the tests keep failing if the fix is ever moved or weakened,
// without pinning it to blueprint.Validate specifically.

// k8sTopLevelKeys is every top-level key a generated document may have.
// Anything else means user text escaped its scalar and became structure.
var k8sTopLevelKeys = map[string]bool{
	"apiVersion": true, "kind": true, "metadata": true, "spec": true,
}

// assertNoInjectedStructure decodes every emitted document and fails if any
// of them grew a top-level key, or stopped parsing at all.
func assertNoInjectedStructure(t *testing.T, outs []Output, where string) {
	t.Helper()
	for _, o := range outs {
		// functions.yaml is a multi-document stream; split it the way any
		// YAML consumer would before decoding each document.
		for i, docBytes := range bytes.Split(o.Body, []byte("\n---\n")) {
			var doc map[string]any
			if err := yaml.Unmarshal(docBytes, &doc); err != nil {
				t.Fatalf("%s: document %d is not parseable YAML after injecting into %s: %v\n---\n%s",
					o.Path, i, where, err, docBytes)
			}
			for k := range doc {
				if !k8sTopLevelKeys[k] {
					t.Fatalf("%s: grew top-level key %q out of user text injected into %s -- "+
						"the document still parses, so every downstream gate passes and "+
						"`cf gen --check` reports in sync\n---\n%s", o.Path, k, where, docBytes)
				}
			}
		}
	}
}

// TestNewlineInUserScalarCannotChangeDocumentStructure is C1. Each case
// poisons exactly one user-controlled scalar with a newline and an injected
// mapping key, then requires that no artifact carrying it ever reaches disk.
func TestNewlineInUserScalarCannotChangeDocumentStructure(t *testing.T) {
	cases := []struct {
		name   string
		poison func(*blueprint.Blueprint)
	}{
		{"Parameter.Description", func(b *blueprint.Blueprint) {
			p := b.Spec.XRD.Parameters["location"]
			p.Description = "line one\nline two: injected"
			b.Spec.XRD.Parameters["location"] = p
		}},
		// A plain newline in a description does NOT inject a key: quoteYAML
		// doubles embedded apostrophes, so the scalar cannot be closed early,
		// and YAML folds the continuation into a space (the emitted
		// description then silently differs from the blueprint's, which is
		// its own defect). A "---" at column 0 is the case that does break
		// structure: it is a document indicator wherever it appears, quoted
		// scalar or not, and the whole XRD stops parsing.
		{"Parameter.Description with a document indicator", func(b *blueprint.Blueprint) {
			p := b.Spec.XRD.Parameters["location"]
			p.Description = "line one\n---\ninjected: true"
			b.Spec.XRD.Parameters["location"] = p
		}},
		{"Parameter.Enum entry", func(b *blueprint.Blueprint) {
			p := b.Spec.XRD.Parameters["location"]
			p.Enum = []string{"EU", "US\ninjected: true"}
			b.Spec.XRD.Parameters["location"] = p
		}},
		{"Parameter.Default", func(b *blueprint.Blueprint) {
			p := b.Spec.XRD.Parameters["location"]
			p.Default = "EU\ninjected: true"
			b.Spec.XRD.Parameters["location"] = p
		}},
		{"Field.Value", func(b *blueprint.Blueprint) {
			b.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-north-1\nbogus: injected"}
		}},
		{"Field.Raw", func(b *blueprint.Blueprint) {
			b.Spec.Resources[0].Fields["region"] = blueprint.Field{Raw: "eu-north-1\nbogus: injected"}
		}},
		{"Metadata.Name", func(b *blueprint.Blueprint) {
			b.Metadata.Name = "xqueue\nbogus: injected"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBlueprint()
			tc.poison(b)
			outs, err := Generate(b, testCRDs(t), "out")
			if err != nil {
				return // Refused at the source. Nothing reached disk.
			}
			assertNoInjectedStructure(t, outs, tc.name)
			t.Fatalf("Generate accepted a newline in %s and emitted documents that happen to "+
				"survive this check; a newline in a user scalar must not reach an emitter at all", tc.name)
		})
	}
}

// TestCleanBlueprintStillEmitsExactlyTheExpectedStructure is the other half
// of the C1 pair: the rule must reject line breaks, not ordinary free text.
func TestCleanBlueprintStillEmitsExactlyTheExpectedStructure(t *testing.T) {
	b := testBlueprint()
	p := b.Spec.XRD.Parameters["location"]
	p.Description = `Region: the "place" # where it lives`
	b.Spec.XRD.Parameters["location"] = p
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-north-1: not a key # not a comment"}

	outs, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate: %v -- colons and hashes in free text are ordinary and quoteYAML handles them", err)
	}
	assertNoInjectedStructure(t, outs, "ordinary punctuation")
}

// TestCompositeParameterBehindFromCannotRenderGoFmt is C2, proved by
// executing the emitted template and decoding what it produces. Before the
// fix, `tags` rendered as the literal string "map[env:prod]" and `zones` as
// "[a b c]" -- and "[a b c]" is valid YAML that a
// `type: array, items: {type: string}` schema accepts as a ONE-element list
// containing "a b c". Legal, applied, wrong.
func TestCompositeParameterBehindFromCannotRenderGoFmt(t *testing.T) {
	cases := []struct {
		name     string
		typ      string
		value    any
		wantKind string // what a correct render would have to produce
	}{
		{"object", "object", map[string]any{"env": "prod"}, "a mapping"},
		{"array", "array", []any{"a", "b", "c"}, "a list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBlueprint()
			b.Spec.XRD.Parameters["thing"] = blueprint.Parameter{Type: tc.typ, Required: true}
			b.Spec.Resources[0].Fields = map[string]blueprint.Field{
				"maxMessageSize": {From: "params.thing"},
			}
			outs, err := Generate(b, testCRDs(t), "out")
			if err != nil {
				return // Refused at the source.
			}

			var comp []byte
			for _, o := range outs {
				if strings.Contains(filepath.ToSlash(o.Path), "/compositions/") {
					comp = o.Body
				}
			}
			if comp == nil {
				t.Fatal("no Composition among the generated outputs")
			}
			rendered, err := renderTemplate(t, extractTemplate(t, comp), map[string]any{
				"providerName": "aws-provider",
				"thing":        tc.value,
			})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
				t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, rendered)
			}
			spec, _ := doc["spec"].(map[string]any)
			fp, _ := spec["forProvider"].(map[string]any)
			got := fp["maxMessageSize"]
			if s, isString := got.(string); isString {
				t.Fatalf("a %s parameter behind from: rendered as the string %q instead of %s -- "+
					"Go's template engine formatted the composite with fmt, and the result is valid "+
					"YAML the API server will accept and store wrong\n---\n%s",
					tc.typ, s, tc.wantKind, rendered)
			}
			t.Fatalf("Generate accepted a from: mapping onto a %s parameter; M1 cannot render one", tc.typ)
		})
	}
}

// --- Final review, I2: Generate is the one entry point, so it validates ---
//
// blueprint.Load validates, but Load is only the CLI's path to a Blueprint.
// The HTTP and MCP front doors build one in memory from a request body, and
// this is the function all three call. Scope: "" is the reproduction: it is
// compared against a bool inside resolveKind, which silently selects the
// LEGACY cluster-scoped variant (whose fields the API server prunes), while
// the XRD emits a null scope:.
func TestGenerateValidatesTheBlueprintItWasHanded(t *testing.T) {
	b := testBlueprint()
	b.Spec.XRD.Scope = ""

	outs, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		if !strings.Contains(err.Error(), "scope") {
			t.Errorf("err = %v, want it to name scope", err)
		}
		return
	}

	// Not refused: then show precisely what an unvalidated Blueprint bought.
	for _, o := range outs {
		var doc map[string]any
		if err := yaml.Unmarshal(o.Body, &doc); err != nil {
			continue // functions.yaml is a multi-document stream; not the point here.
		}
		if doc["kind"] == "CompositeResourceDefinition" {
			spec, _ := doc["spec"].(map[string]any)
			if scope, present := spec["scope"]; !present || scope == nil || scope == "" {
				t.Errorf("XRD emitted scope: %v -- an omitted scope is defaulted to Namespaced by "+
					"the API server and to LegacyCluster by `crossplane xrd convert`", scope)
			}
		}
		if doc["kind"] == "Composition" {
			if !bytes.Contains(o.Body, []byte("sqs.aws.m.upbound.io")) {
				t.Errorf("Composition selected the legacy cluster-scoped variant, whose extra "+
					"fields the API server silently prunes\n---\n%s", o.Body)
			}
		}
	}
	t.Fatal("Generate accepted a Blueprint with no scope; it must validate what it is handed, " +
		"because the HTTP and MCP front doors never go through blueprint.Load")
}

// --- Final review, I1: providerName ---
//
// composition.go dereferences $spec.providerName unguarded for every
// composed resource. This proves the consequence rather than the rule: if a
// blueprint without the parameter is ever accepted, the Composition it
// produces cannot render, because under options: ["missingkey=error"] the
// dereference is a hard failure on any XR the XRD would actually admit.
func TestBlueprintWithoutProviderNameCannotProduceARenderableComposition(t *testing.T) {
	b := testBlueprint()
	delete(b.Spec.XRD.Parameters, "providerName")

	outs, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		if !strings.Contains(err.Error(), "providerName") {
			t.Errorf("err = %v, want it to name providerName", err)
		}
		return
	}
	for _, o := range outs {
		if !strings.Contains(filepath.ToSlash(o.Path), "/compositions/") {
			continue
		}
		// An XR that satisfies the emitted XRD exactly: no providerName,
		// because the XRD no longer declares it.
		if _, err := renderTemplate(t, extractTemplate(t, o.Body), map[string]any{}); err != nil {
			t.Fatalf("the emitted Composition can never render: %v -- a blueprint with no "+
				"providerName parameter must be refused, not generated", err)
		}
	}
	t.Fatal("Generate accepted a Namespaced blueprint with no providerName parameter")
}

// --- Final review, I5(a): field paths are checked against the CRD schema ---
//
// A typo'd field name used to be emitted verbatim. The Composition is valid
// YAML, `crossplane composition render` renders it (it does not schema-check
// composed resources), every gate exits 0 -- and the API server then
// silently PRUNES the unknown field on apply. Nothing anywhere says why the
// queue came up with a default. internal/schema's Leaves is what makes this
// checkable, and this is its first production caller.
func TestUnknownForProviderFieldIsRejectedWithASuggestion(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{
		"maxMessageSiz": {From: "params.maxMessageSize"}, // one character short
	}
	_, err := Composition(b, testCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a field name absent from the CRD's spec.forProvider; " +
			"the API server prunes it silently on apply, so this is the only layer that can catch it")
	}
	if !strings.Contains(err.Error(), "maxMessageSiz") {
		t.Errorf("err = %v, want it to name the offending path", err)
	}
	if !strings.Contains(err.Error(), `"maxMessageSize"`) {
		t.Errorf("err = %v, want it to suggest the closest valid path, maxMessageSize", err)
	}
}

// A field far from anything real gets an error with no misleading guess.
func TestWildlyUnknownFieldGetsNoBogusSuggestion(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{
		"totallyUnrelatedThing": {Value: "x"},
	}
	_, err := Composition(b, testCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted an unknown field")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("err = %v, want no suggestion: a wrong guess invites a second blind edit", err)
	}
}

// Known fields, including the required one, must still pass.
func TestKnownForProviderFieldsAreAccepted(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{
		"region":         {Value: "eu-north-1"},
		"maxMessageSize": {From: "params.maxMessageSize"},
	}
	if _, err := Composition(b, testCRDs(t)); err != nil {
		t.Fatalf("Composition: %v -- region and maxMessageSize are both in the fixture CRD", err)
	}
}

// --- Final review, I5(b): no forProvider is an error, not an empty map ---
//
// provider-kubernetes' ObservedObjectCollection genuinely has no
// forProvider. M1 assumes a forProvider-shaped envelope (Global Constraint 8
// is deferred to M2), so it must say so rather than emit
// `spec: {forProvider: {}}` against a schema with no such key -- which the
// API server prunes without a word.
func TestCRDWithoutForProviderIsALoudError(t *testing.T) {
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
              providerConfigRef:
                type: object
                properties: {kind: {type: string}, name: {type: string}}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Composition(testBlueprint(), crds)
	if err == nil {
		t.Fatal("Composition emitted against a CRD with no spec.forProvider; it must refuse " +
			"rather than write a forProvider the schema has no key for")
	}
	if !strings.Contains(err.Error(), "forProvider") {
		t.Errorf("err = %v, want it to name forProvider", err)
	}
}
