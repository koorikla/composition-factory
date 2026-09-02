// Tests for blueprint-authored metadata.annotations: emitted shape (sorted,
// quoted, guarded) and render semantics — the guard proofs are executed
// against Go's real text/template under Option("missingkey=error"), never
// string-matched alone, per this package's convention.
package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// annotatedBlueprint is testBlueprint() with annotations covering the value,
// required-wire and optional-wire forms on the managed queue.
func annotatedBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.Resources[0].Annotations = map[string]blueprint.Field{
		"example.com/size":     {From: "params.maxMessageSize"}, // optional -> guarded
		"example.com/location": {From: "params.location"},       // required -> bare
		"example.com/note":     {Value: "made by: cf"},          // literal with ": " inside
		"zzz.example.com/raw":  {Raw: `"literal"`},
	}
	return b
}

// docAnnotations returns metadata.annotations of the rendered document whose
// kind matches, decoded.
func docAnnotations(t *testing.T, rendered, kind string) map[string]any {
	t.Helper()
	for _, doc := range renderedDocs(t, rendered) {
		if doc["kind"] != kind {
			continue
		}
		meta, _ := doc["metadata"].(map[string]any)
		anns, ok := meta["annotations"].(map[string]any)
		if !ok {
			t.Fatalf("%s metadata.annotations = %T (%v), want a map\n---\n%s",
				kind, meta["annotations"], meta["annotations"], rendered)
		}
		return anns
	}
	t.Fatalf("no %s document in the rendered output\n---\n%s", kind, rendered)
	return nil
}

// The emitted shape: keys sorted and single-quoted, literals quoteYAML'd,
// wires piped through quote, the optional wire behind exactly the hasKey
// guard fields get — and the function-set setResourceNameAnnotation line
// still first, untouched.
func TestAnnotationsEmitSortedQuotedAndGuarded(t *testing.T) {
	got, err := Composition(annotatedBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		`{{ setResourceNameAnnotation "main-queue" }}`,
		`'example.com/location': {{ $spec.location | quote }}`,
		`{{- if hasKey $spec "maxMessageSize" }}`,
		`'example.com/size': {{ $spec.maxMessageSize | quote }}`,
		`'example.com/note': 'made by: cf'`,
		`'zzz.example.com/raw': "literal"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("emitted Composition missing %q\n---\n%s", want, s)
		}
	}
	// Sorted key order, after the function-set line.
	idx := func(sub string) int { return strings.Index(s, sub) }
	order := []int{
		idx("setResourceNameAnnotation"),
		idx("'example.com/location'"),
		idx("'example.com/note'"),
		idx("'example.com/size'"),
		idx("'zzz.example.com/raw'"),
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] < 0 || order[i] < 0 || order[i-1] > order[i] {
			t.Fatalf("annotation lines out of sorted order (offsets %v)\n---\n%s", order, s)
		}
	}
}

func TestAnnotationEmitIsDeterministic(t *testing.T) {
	a, err := Composition(annotatedBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition (first run): %v", err)
	}
	b, err := Composition(annotatedBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two Composition runs over the same annotated blueprint produced different bytes")
	}
}

// Guard proof, executed both ways: with the optional parameter present the
// annotation lands — as a STRING, even though the parameter is an integer —
// and with it absent the KEY is omitted cleanly under missingkey=error.
func TestAnnotationParamWireRendersBothWays(t *testing.T) {
	got, err := Composition(annotatedBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("present", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "localstack", "location": "EU",
			// float64, the protobuf number shape the real engine sees.
			"maxMessageSize": float64(2048),
		})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		anns := docAnnotations(t, rendered, "Queue")
		if anns["example.com/size"] != "2048" {
			t.Errorf("example.com/size = %v (%T), want the string \"2048\" — quote must make "+
				"the integer scalar a string", anns["example.com/size"], anns["example.com/size"])
		}
		if anns["example.com/location"] != "EU" {
			t.Errorf("example.com/location = %v, want EU", anns["example.com/location"])
		}
		if anns["example.com/note"] != "made by: cf" {
			t.Errorf("example.com/note = %v, want the literal with its colon intact", anns["example.com/note"])
		}
		if anns["crossplane.io/composition-resource-name"] != "main-queue" {
			t.Errorf("composition-resource-name = %v — the function-set annotation must survive "+
				"authored neighbours", anns["crossplane.io/composition-resource-name"])
		}
	})

	t.Run("absent", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "localstack", "location": "EU",
		})
		if err != nil {
			t.Fatalf("render must succeed with the optional parameter absent, got: %v", err)
		}
		anns := docAnnotations(t, rendered, "Queue")
		if _, present := anns["example.com/size"]; present {
			t.Errorf("example.com/size must be omitted when the parameter is absent, got %v",
				anns["example.com/size"])
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(rendered, bad) {
				t.Errorf("rendered output contains %q", bad)
			}
		}
	})
}

// The IRSA shape in miniature, proven both ways on a MANAGED annotation
// target: an annotation wired from another resource's observed status lands
// once observed, and while unobserved the key is cleanly absent — the same
// guard chain field wires get, executed, not string-matched.
func TestAnnotationStatusWireRendersBothWays(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"example.com/queue-url": {From: "resources.main-queue.status.atProvider.url"},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)
	const url = "https://sqs.eu-north-1.amazonaws.com/1/demo"

	t.Run("observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, observedQueue(url))
		if err != nil {
			t.Fatalf("render with an observed source: %v", err)
		}
		anns := docAnnotations(t, rendered, "QueuePolicy")
		if anns["example.com/queue-url"] != url {
			t.Errorf("example.com/queue-url = %v, want the observed URL", anns["example.com/queue-url"])
		}
	})

	t.Run("unobserved", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, nil)
		if err != nil {
			t.Fatalf("render must succeed while the source is unobserved, got: %v", err)
		}
		anns := docAnnotations(t, rendered, "QueuePolicy")
		if _, present := anns["example.com/queue-url"]; present {
			t.Errorf("example.com/queue-url must be omitted while unobserved, got %v",
				anns["example.com/queue-url"])
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(rendered, bad) {
				t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
			}
		}
	})
}

// Annotations on a NATIVE kind — the motivating IRSA case: the ServiceAccount
// carries the wired annotation once the source is observed, the key is
// cleanly absent before, and the native document still has no Crossplane
// envelope anywhere near it.
func TestNativeAnnotationRendersOnTheObject(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, wireCRDs(t)...)

	b := wireBlueprint()
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"automountServiceAccountToken": {Raw: "true"},
		},
		Annotations: map[string]blueprint.Field{
			"eks.amazonaws.com/role-arn": {From: "resources.main-queue.status.atProvider.url"},
			"example.com/team":           {Value: "platform"},
		},
	})
	got, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)
	const url = "https://sqs.eu-north-1.amazonaws.com/1/demo"

	t.Run("observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, observedQueue(url))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		anns := docAnnotations(t, rendered, "ServiceAccount")
		if anns["eks.amazonaws.com/role-arn"] != url {
			t.Errorf("role-arn = %v, want the observed value", anns["eks.amazonaws.com/role-arn"])
		}
		if anns["example.com/team"] != "platform" {
			t.Errorf("team = %v, want platform", anns["example.com/team"])
		}
	})

	t.Run("unobserved", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, nil)
		if err != nil {
			t.Fatalf("render must succeed while the source is unobserved, got: %v", err)
		}
		anns := docAnnotations(t, rendered, "ServiceAccount")
		if _, present := anns["eks.amazonaws.com/role-arn"]; present {
			t.Errorf("role-arn must be omitted while unobserved, got %v", anns["eks.amazonaws.com/role-arn"])
		}
		if anns["example.com/team"] != "platform" {
			t.Errorf("team = %v — the unconditional annotation must render regardless", anns["example.com/team"])
		}
		for _, doc := range renderedDocs(t, rendered) {
			if doc["kind"] != "ServiceAccount" {
				continue
			}
			if _, has := doc["spec"]; has {
				t.Errorf("ServiceAccount grew a spec — annotations must not disturb the native body\n---\n%s", rendered)
			}
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(rendered, bad) {
				t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
			}
		}
	})
}

// A template-form annotation renders through the same include pipeline
// fields use — legal on the native family too, since every annotation sits
// at one fixed column.
func TestAnnotationTemplateFormRenders(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, testCRDs(t)...)

	b := annotatedBlueprint()
	b.Spec.Templates = map[string]string{
		"cf.team": "team-{{ .resource }}-{{ .spec.location }}",
	}
	b.Spec.Resources[0].Annotations["example.com/templated"] = blueprint.Field{Template: "cf.team"}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
		Annotations: map[string]blueprint.Field{
			"example.com/templated": {Template: "cf.team"},
		},
	})
	got, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, got), map[string]any{
		"providerName": "localstack", "location": "EU",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := docAnnotations(t, rendered, "Queue")["example.com/templated"]; got != "team-main-queue-EU" {
		t.Errorf("Queue templated annotation = %v, want team-main-queue-EU", got)
	}
	if got := docAnnotations(t, rendered, "ServiceAccount")["example.com/templated"]; got != "team-sa-EU" {
		t.Errorf("ServiceAccount templated annotation = %v, want team-sa-EU", got)
	}
}

// The reserved composition-resource-name keys are refused by the emitter
// too: Composition is exported, so the rule cannot depend on every caller
// running Validate first.
func TestReservedAnnotationKeyIsRefusedAtEmit(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Annotations = map[string]blueprint.Field{
		"crossplane.io/composition-resource-name": {Value: "impostor"},
	}
	_, err := Composition(b, testCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "setResourceNameAnnotation") {
		t.Fatalf("Composition = %v, want the reserved-key refusal", err)
	}
}

// The CRD half of an annotation wire: a status path the source kind never
// declares is caught here — the only layer that can — and the error names
// the ANNOTATION, with the same did-you-mean help field wires get.
func TestAnnotationStatusWireUnknownPathIsRejected(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"example.com/queue-url": {From: "resources.main-queue.status.atProvider.ur"},
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a status path the CRD does not declare")
	}
	if !strings.Contains(err.Error(), `annotation "example.com/queue-url"`) {
		t.Errorf("err = %v, want it to name the annotation", err)
	}
	if !strings.Contains(err.Error(), `did you mean "atProvider.url"`) {
		t.Errorf("err = %v, want the suggestion", err)
	}
}

// A composite status leaf behind an annotation wire is refused for the same
// fmt-of-a-map reason as field wires — quote would just quote the garbage.
func TestAnnotationStatusWireNonScalarLeafIsRejected(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"example.com/tags": {From: "resources.main-queue.status.atProvider.tags"},
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "only carry a scalar") {
		t.Fatalf("Composition = %v, want the scalar-only refusal", err)
	}
}

// An empty annotations map authors nothing: the emitted Composition is
// byte-identical to one from a blueprint that never declared the key.
func TestEmptyAnnotationsMapEmitsIdenticallyToAbsent(t *testing.T) {
	withEmpty := testBlueprint()
	withEmpty.Spec.Resources[0].Annotations = map[string]blueprint.Field{}
	a, err := Composition(withEmpty, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition (empty map): %v", err)
	}
	b, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition (absent): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("an empty annotations map changed the emitted bytes")
	}
}
