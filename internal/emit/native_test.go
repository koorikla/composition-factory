package emit

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"sigs.k8s.io/yaml"
)

// nativeTestCRDs is the emit-side schema set for a blueprint mixing both
// families: the managed Queue fixtures plus the REAL vendored native kinds.
// Using the real vendored schemas rather than a hand-written Deployment is
// deliberate: the acceptance criterion is that the actual pod-template
// hierarchy (spec.template.spec.containers[0].image) validates and renders,
// and a toy fixture could pass while the vendored tree diverged.
func nativeTestCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	return append(native, testCRDs(t)...)
}

// nativeTestBlueprint composes a managed Queue, a native Deployment whose
// container image comes from a parameter, and a native Service — the
// design's acceptance shape.
func nativeTestBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xwebapp"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.hooli.tech", Kind: "XWebApp", Plural: "xwebapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"image":        {Type: "string", Required: true},
					"replicas":     {Type: "integer"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "main-queue", Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-north-1"},
					},
				},
				{
					Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.replicas":                          {From: "params.replicas"},
						"spec.selector.matchLabels":              {Raw: "{app: web}"},
						"spec.template.metadata.labels":          {Raw: "{app: web}"},
						"spec.template.spec.containers[0].name":  {Value: "web"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
					},
				},
				{
					Name: "web-svc", Kind: "Service", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector":      {Raw: "{app: web}"},
						"spec.ports[0].port": {Raw: "80"},
						"spec.type":          {Value: "ClusterIP"},
					},
				},
			},
		},
	}
}

// renderedDocs executes the emitted template and decodes every document in
// the rendered stream, keyed by kind (each kind appears once in these
// fixtures).
func renderedDocs(t *testing.T, comp []byte, xrSpec map[string]any) map[string]map[string]any {
	t.Helper()
	rendered, err := renderTemplate(t, extractTemplate(t, comp), xrSpec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	docs := map[string]map[string]any{}
	for _, chunk := range strings.Split(rendered, "\n---\n") {
		chunk = strings.TrimPrefix(strings.TrimSpace(chunk), "---")
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("rendered document is not valid YAML: %v\n---\n%s", err, chunk)
		}
		kind, _ := doc["kind"].(string)
		if kind == "" {
			t.Fatalf("rendered document has no kind:\n%s", chunk)
		}
		docs[kind] = doc
	}
	return docs
}

// The design's acceptance shape, proven by executing the emitted template:
// the Deployment lands as the Kubernetes object itself — the parameter's
// image at the real nested path, and NO forProvider envelope, NO
// providerConfigRef, NO managementPolicies anywhere on it — while the
// managed Queue beside it keeps its envelope untouched.
func TestNativeResourceRendersAsTheObjectItself(t *testing.T) {
	comp, err := Composition(nativeTestBlueprint(), nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	docs := renderedDocs(t, comp, map[string]any{
		"providerName": "aws-provider",
		"image":        "nginx:1.29",
		"replicas":     3,
	})

	dep, ok := docs["Deployment"]
	if !ok {
		t.Fatalf("no Deployment among rendered docs: %v", docs)
	}
	if av := dep["apiVersion"]; av != "apps/v1" {
		t.Errorf("Deployment apiVersion = %v, want apps/v1", av)
	}
	spec, _ := dep["spec"].(map[string]any)
	if spec == nil {
		t.Fatalf("Deployment has no spec: %v", dep)
	}
	for _, forbidden := range []string{"forProvider", "providerConfigRef", "managementPolicies", "deletionPolicy"} {
		if _, present := spec[forbidden]; present {
			t.Errorf("Deployment spec carries %q — a native object has no Crossplane envelope", forbidden)
		}
	}
	if got := dig(t, dep, "spec", "template", "spec", "containers", 0, "image"); got != "nginx:1.29" {
		t.Errorf("containers[0].image = %v, want the parameter value nginx:1.29", got)
	}
	if got := dig(t, dep, "spec", "template", "spec", "containers", 0, "name"); got != "web" {
		t.Errorf("containers[0].name = %v, want web", got)
	}
	if got := dig(t, dep, "spec", "replicas"); got != float64(3) && got != 3 {
		t.Errorf("spec.replicas = %v (%T), want 3", got, got)
	}
	if got := dig(t, dep, "spec", "selector", "matchLabels", "app"); got != "web" {
		t.Errorf("selector.matchLabels.app = %v, want web (raw subtree)", got)
	}

	svc, ok := docs["Service"]
	if !ok {
		t.Fatalf("no Service among rendered docs")
	}
	if av := svc["apiVersion"]; av != "v1" {
		t.Errorf("Service apiVersion = %v, want bare v1 (core group)", av)
	}
	if got := dig(t, svc, "spec", "ports", 0, "port"); got != float64(80) && got != 80 {
		t.Errorf("Service ports[0].port = %v, want 80", got)
	}
	svcSpec, _ := svc["spec"].(map[string]any)
	if _, present := svcSpec["providerConfigRef"]; present {
		t.Error("Service spec carries providerConfigRef")
	}

	queue, ok := docs["Queue"]
	if !ok {
		t.Fatalf("no Queue among rendered docs")
	}
	if got := dig(t, queue, "spec", "forProvider", "region"); got != "eu-north-1" {
		t.Errorf("Queue kept its forProvider envelope? region = %v", got)
	}
	if got := dig(t, queue, "spec", "providerConfigRef", "name"); got != "aws-provider" {
		t.Errorf("Queue providerConfigRef.name = %v — the managed path must be untouched", got)
	}
}

// An optional parameter under an unconditionally-rendered branch is guarded
// leaf-by-leaf; when it is the ONLY thing in its subtree, the whole subtree
// (key included) must vanish rather than render `spec:` as a YAML null.
func TestNativeOptionalFieldsAreOmittedNotNulled(t *testing.T) {
	b := nativeTestBlueprint()

	t.Run("optional leaf beside unconditional siblings", func(t *testing.T) {
		comp, err := Composition(b, nativeTestCRDs(t))
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		docs := renderedDocs(t, comp, map[string]any{
			"providerName": "aws-provider",
			"image":        "nginx:1.29",
			// replicas deliberately absent
		})
		spec, _ := docs["Deployment"]["spec"].(map[string]any)
		if _, present := spec["replicas"]; present {
			t.Errorf("spec.replicas rendered without the parameter being set: %v", spec["replicas"])
		}
		if dig(t, docs["Deployment"], "spec", "template", "spec", "containers", 0, "image") != "nginx:1.29" {
			t.Error("unconditional fields must render regardless of the absent optional")
		}
	})

	t.Run("subtree whose every leaf is optional vanishes entirely", func(t *testing.T) {
		only := *b
		only.Spec.Resources = []blueprint.Resource{{
			Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
			Fields: map[string]blueprint.Field{
				"spec.replicas": {From: "params.replicas"},
			},
		}}
		comp, err := Composition(&only, nativeTestCRDs(t))
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		docs := renderedDocs(t, comp, map[string]any{"providerName": "aws-provider", "image": "x"})
		dep := docs["Deployment"]
		if v, present := dep["spec"]; present {
			t.Errorf("spec rendered as %v with no set field under it — a bare key is a YAML null the API server rejects", v)
		}

		withParam := renderedDocs(t, comp, map[string]any{"providerName": "p", "image": "x", "replicas": 2})
		if got := dig(t, withParam["Deployment"], "spec", "replicas"); got != float64(2) && got != 2 {
			t.Errorf("spec.replicas = %v, want 2 once the parameter is set", got)
		}
	})
}

// Field paths on a native resource are checked against the vendored schema
// through the same closest-path machinery managed resources get.
func TestUnknownNativeFieldIsRejectedWithASuggestion(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[1].Fields = map[string]blueprint.Field{
		"spec.template.spec.containers[0].imag": {Value: "nginx"}, // one character short
	}
	_, err := Composition(b, nativeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a field path absent from the vendored Deployment schema")
	}
	if !strings.Contains(err.Error(), "containers[0].imag") {
		t.Errorf("err = %v, want it to name the offending path", err)
	}
	if !strings.Contains(err.Error(), `"spec.template.spec.containers[0].image"`) {
		t.Errorf("err = %v, want it to suggest the real path", err)
	}
	if !strings.Contains(err.Error(), "native Deployment schema") {
		t.Errorf("err = %v, want it to say the check ran against the native schema, not spec.forProvider", err)
	}
}

// (kind, provider) matching: a native kind is composed only via an explicit
// provider: k8s — kind names collide across families for real (Kubernetes
// and provider-aws-ecs both have a "Service"), so a bare name must fail
// with the hint, never quietly pick a family.
func TestNativeKindRequiresExplicitProvider(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[1].Provider = "" // Deployment, no provider
	_, err := Composition(b, nativeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition resolved a native kind without provider: k8s")
	}
	if !strings.Contains(err.Error(), "provider: k8s") {
		t.Errorf("err = %v, want the provider: k8s hint", err)
	}
}

func TestUnknownKindUnderK8sProviderNamesThePin(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[1].Kind = "Ingress" // real kind, not in the vendored subset
	_, err := Composition(b, nativeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition resolved a kind the vendored subset does not carry")
	}
	for _, want := range []string{"Ingress", "vendored", k8s.Version} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// The two unrenderable path shapes are refused, not guessed at.
func TestConflictingNativeFieldPathsAreRefused(t *testing.T) {
	t.Run("subtree set whole and by leaf", func(t *testing.T) {
		b := nativeTestBlueprint()
		b.Spec.Resources[1].Fields["spec.selector"] = blueprint.Field{Raw: "{matchLabels: {app: web}}"}
		// spec.selector.matchLabels is already set in the fixture.
		_, err := Composition(b, nativeTestCRDs(t))
		if err == nil {
			t.Fatal("Composition accepted a path that is both a set value and a parent of another set value")
		}
		if !strings.Contains(err.Error(), "spec.selector") {
			t.Errorf("err = %v, want it to name the conflicting subtree", err)
		}
	})

	t.Run("array set whole and by element", func(t *testing.T) {
		b := nativeTestBlueprint()
		b.Spec.Resources[2].Fields["spec.ports"] = blueprint.Field{Raw: "[{port: 80}]"}
		// spec.ports[0].port is already set in the fixture.
		_, err := Composition(b, nativeTestCRDs(t))
		if err == nil {
			t.Fatal("Composition accepted an array set both whole and by element")
		}
		if !strings.Contains(err.Error(), "ports") {
			t.Errorf("err = %v, want it to name the array", err)
		}
	})
}

// The golden pins the emitted Composition for the mixed managed+native
// blueprint byte-for-byte: the envelope fork, the nested pod-template tree,
// the dash placement inside containers[0], the guards — all of it. Churning
// bytes on a prune:true GitOps repo is a live-cluster incident, so any
// intentional change to this output must show up as a reviewed golden diff
// (regenerate with CF_UPDATE_GOLDEN=1), never as an invisible drift.
func TestNativeCompositionMatchesGolden(t *testing.T) {
	const goldenPath = "testdata/native-composition.golden.yaml"
	got, err := Composition(nativeTestBlueprint(), nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with CF_UPDATE_GOLDEN=1)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("emitted Composition drifted from the golden (regenerate deliberately with CF_UPDATE_GOLDEN=1):\n--- got ---\n%s", got)
	}
}

// Determinism is a correctness requirement: the same blueprint over the
// same schemas must emit byte-identical artifacts, twice in one process and
// on every rebuild.
func TestNativeGenerateIsDeterministic(t *testing.T) {
	crds := nativeTestCRDs(t)
	first, err := Generate(nativeTestBlueprint(), crds, "out")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := Generate(nativeTestBlueprint(), nativeTestCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (second): %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("output counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Errorf("output %d path differs: %q vs %q", i, first[i].Path, second[i].Path)
		}
		if !bytes.Equal(first[i].Body, second[i].Body) {
			t.Errorf("output %s is not byte-identical across two generations", first[i].Path)
		}
	}
}
