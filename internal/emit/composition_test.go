package emit

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

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
	got, _ := Composition(testBlueprint(), testCRDs(t))
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
	got, _ := Composition(testBlueprint(), testCRDs(t))
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
	funcs := template.FuncMap{
		"hasKey": func(d map[string]any, key string) bool {
			_, ok := d[key]
			return ok
		},
		"setResourceNameAnnotation": func(name string) string {
			return "crossplane.io/composition-resource-name: " + name
		},
	}
	tmpl, err := template.New("t").Option("missingkey=error").Funcs(funcs).Parse(tmplBody)
	if err != nil {
		t.Fatalf("template body does not parse: %v\n---\n%s", err, tmplBody)
	}
	data := map[string]any{
		"observed": map[string]any{
			"composite": map[string]any{
				"resource": map[string]any{
					"metadata": map[string]any{"name": "my-xqueue"},
					"spec":     xrSpec,
				},
			},
		},
	}
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

func TestProviderConfigRefCarriesKindAndName(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	s := string(got)
	if !strings.Contains(s, "kind: ClusterProviderConfig") || !strings.Contains(s, "name: {{ $spec.providerName }}") {
		t.Errorf("providerConfigRef must carry both kind and name in the v2 namespaced envelope\n---\n%s", s)
	}
}

func TestNoDeletionPolicyForNamespacedMR(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	if strings.Contains(string(got), "deletionPolicy") {
		t.Error("deletionPolicy is absent from the v2 namespaced envelope and would be pruned")
	}
}

func TestResourceNameAnnotationPresent(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
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
