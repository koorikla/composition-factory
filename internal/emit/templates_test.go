package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"sigs.k8s.io/yaml"
)

// conventionCRDs is a namespaced Queue whose forProvider carries the two
// convention targets the acceptance path uses on the real provider — name
// (a scalar) and tags (a map) — plus required region and a nested branch
// (endpoint) that a convention must NOT touch.
func conventionCRDs(t *testing.T) []schema.CRD {
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
                  name: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  endpoint:
                    properties:
                      hostname: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// conventionTestBlueprint: two queues under a naming and a tags convention,
// with queue-b overriding the name explicitly — the override case.
func conventionTestBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueueconv"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XQueueConv", Plural: "xqueueconvs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Templates: map[string]string{
				"cf.name": "{{ .xr }}-{{ .resource }}",
				"cf.tags": "managed-by: crossplane\nxr: {{ .xr | quote }}",
			},
			Conventions: []blueprint.Convention{
				{Match: "name", Template: "cf.name"},
				{Match: "tags", Template: "cf.tags"},
			},
			Resources: []blueprint.Resource{{
				Name: "queue-a", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
				},
			}, {
				Name: "queue-b", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
					// Explicit wins over the naming convention: that IS the
					// override mechanism.
					"name": {Value: "custom-b"},
				},
			}},
		},
	}
}

// TestTemplatesGoldenTemplate pins the emitted body byte-for-byte: the
// define blocks heading the template in sorted order (byte-identical to what
// blueprint validation parsed), the convention-injected include calls, and
// queue-b's explicit name winning over the convention.
func TestTemplatesGoldenTemplate(t *testing.T) {
	got, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	want := `{{- define "cf.name" }}
{{ .xr }}-{{ .resource }}
{{- end }}
{{- define "cf.tags" }}
managed-by: crossplane
xr: {{ .xr | quote }}
{{- end }}
{{- $spec := .observed.composite.resource.spec -}}
{{- $xr := .observed.composite.resource.metadata.name -}}
{{- $xrMeta := .observed.composite.resource.metadata -}}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "queue-a" }}
spec:
  forProvider:
    name: {{ include "cf.name" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "queue-a" "field" "name") | trim | nindent 6 }}
    region: 'eu-north-1'
    tags: {{ include "cf.tags" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "queue-a" "field" "tags") | trim | nindent 6 }}
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "queue-b" }}
spec:
  forProvider:
    name: 'custom-b'
    region: 'eu-north-1'
    tags: {{ include "cf.tags" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "queue-b" "field" "tags") | trim | nindent 6 }}
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// TestConventionsRenderOnTheArtifact executes the emitted template and
// proves the contract structurally on the rendered YAML: the scalar template
// output becomes queue-a's forProvider.name, the multi-line template output
// becomes a real tags MAPPING (not a string) on both queues, and queue-b's
// explicit name overrides the convention. The nested endpoint branch stays
// untouched.
func TestConventionsRenderOnTheArtifact(t *testing.T) {
	got, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, got), map[string]any{
		"providerName": "aws-provider",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	byName := map[string]map[string]any{}
	for _, doc := range renderedDocs(t, rendered) {
		meta, _ := doc["metadata"].(map[string]any)
		anns, _ := meta["annotations"].(map[string]any)
		name, _ := anns["crossplane.io/composition-resource-name"].(string)
		spec, _ := doc["spec"].(map[string]any)
		fp, _ := spec["forProvider"].(map[string]any)
		byName[name] = fp
	}

	a, ok := byName["queue-a"]
	if !ok {
		t.Fatalf("no queue-a document rendered\n---\n%s", rendered)
	}
	if a["name"] != "my-xqueue-queue-a" {
		t.Errorf("queue-a name = %v, want my-xqueue-queue-a (the naming convention)", a["name"])
	}
	bq, ok := byName["queue-b"]
	if !ok {
		t.Fatalf("no queue-b document rendered\n---\n%s", rendered)
	}
	if bq["name"] != "custom-b" {
		t.Errorf("queue-b name = %v, want the explicit custom-b to win over the convention", bq["name"])
	}
	for _, q := range []string{"queue-a", "queue-b"} {
		tags, isMap := byName[q]["tags"].(map[string]any)
		if !isMap {
			t.Fatalf("%s tags = %v (%T), want a YAML mapping -- the multi-line template output "+
				"must nest under the key, not collapse into a string\n---\n%s",
				q, byName[q]["tags"], byName[q]["tags"], rendered)
		}
		if tags["managed-by"] != "crossplane" || tags["xr"] != "my-xqueue" {
			t.Errorf("%s tags = %v, want managed-by: crossplane and xr: my-xqueue", q, tags)
		}
		if _, present := byName[q]["endpoint"]; present {
			t.Errorf("%s grew an endpoint field -- a convention must only touch top-level leaves", q)
		}
	}
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(rendered, bad) {
			t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
		}
	}
}

// A field template call must also work outside conventions, composed with
// forEach: the context's .resource is the un-indexed node name.
func TestFieldTemplateInsideForEachRenders(t *testing.T) {
	b := conventionTestBlueprint()
	b.Spec.Conventions = nil
	b.Spec.XRD.Parameters["instanceCount"] = blueprint.Parameter{Type: "integer", Default: "2"}
	b.Spec.Resources = b.Spec.Resources[:1]
	b.Spec.Resources[0].ForEach = "params.instanceCount"
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{
		"region": {Value: "eu-north-1"},
		"name":   {Template: "cf.name"},
	}
	got, err := Composition(b, conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, got), map[string]any{
		"providerName": "aws-provider", "instanceCount": float64(2),
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Structural, not a string match: nindent puts a scalar on the line
	// after its key ("name:\n      my-xqueue-queue-a"), which is the same
	// YAML value in a different serialization.
	docs := renderedDocs(t, rendered)
	if len(docs) != 2 {
		t.Fatalf("got %d rendered documents, want 2 loop iterations\n---\n%s", len(docs), rendered)
	}
	for _, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		fp, _ := spec["forProvider"].(map[string]any)
		if fp["name"] != "my-xqueue-queue-a" {
			t.Errorf("forProvider.name = %v, want the template-set my-xqueue-queue-a on every "+
				"loop iteration\n---\n%s", fp["name"], rendered)
		}
	}
}

func TestTemplatesEmitIsDeterministic(t *testing.T) {
	first, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}

// TestDefineBlocksHeadTheTemplatingStepWithPipeline pins where define blocks
// land when the blueprint ALSO declares its own pipeline steps: inside the
// GO-TEMPLATING step's template specifically — after its `template: |`, before
// the $spec/$xr bindings — and in no other step. This is the seam between the
// templates feature (which heads "the template") and spec.pipeline (which
// means the Composition can carry several steps, only one of which has that
// template).
func TestDefineBlocksHeadTheTemplatingStepWithPipeline(t *testing.T) {
	b := conventionTestBlueprint()
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name: "pre-check", FunctionRef: "function-cel-filter",
			Package:  "xpkg.crossplane.io/crossplane-contrib/function-cel-filter",
			Position: blueprint.PositionBefore,
			Input:    "apiVersion: cel.fn.crossplane.io/v1beta1\nkind: Filters",
		},
		{
			Name: "auto-ready", FunctionRef: "function-auto-ready",
			Package: "xpkg.crossplane.io/crossplane-contrib/function-auto-ready",
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}

	// Step order: the before-step, then the templating step, then auto-ready.
	var parsed map[string]any
	if err := yaml.Unmarshal(comp, &parsed); err != nil {
		t.Fatalf("emitted document is not valid YAML: %v\n---\n%s", err, comp)
	}
	steps, _ := dig(t, parsed, "spec", "pipeline").([]any)
	var names []string
	var tmpl string
	for _, s := range steps {
		step, _ := s.(map[string]any)
		name, _ := step["step"].(string)
		names = append(names, name)
		if name == blueprint.TemplatingStepName {
			tmpl, _ = dig(t, step, "input", "inline", "template").(string)
		}
	}
	if diff := cmp.Diff([]string{"pre-check", blueprint.TemplatingStepName, "auto-ready"}, names); diff != "" {
		t.Fatalf("pipeline step order (-want +got):\n%s", diff)
	}
	if tmpl == "" {
		t.Fatalf("no inline template on the %s step", blueprint.TemplatingStepName)
	}

	// The define blocks head THAT step's template, in sorted name order,
	// before the $spec binding.
	wantHead := `{{- define "cf.name" }}
{{ .xr }}-{{ .resource }}
{{- end }}
{{- define "cf.tags" }}
managed-by: crossplane
xr: {{ .xr | quote }}
{{- end }}
{{- $spec := .observed.composite.resource.spec -}}
`
	if !strings.HasPrefix(tmpl, wantHead) {
		t.Errorf("templating step's template does not start with the define blocks:\n%s", tmpl)
	}
	// And nowhere else: exactly one occurrence in the whole document.
	if n := strings.Count(string(comp), `{{- define "cf.name" }}`); n != 1 {
		t.Errorf("define block appears %d times in the Composition, want exactly 1 (inside the templating step)", n)
	}
}

// Conventions x native kinds, revised ruling: conventions SKIP native
// resources instead of refusing the document, so a conventions-bearing
// blueprint can freely compose native kinds. Managed siblings still get
// the convention; the native document must carry no template call. Only
// the tags convention is kept here: no Deployment field ends in "tags",
// whereas "name" names metadata.name and the container name at depth,
// which is the refusal CF-109 and CF-149 guard.
func TestConventionsSkipNativeKinds(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	b.Spec.Conventions = []blueprint.Convention{{Match: "tags", Template: "cf.tags"}}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{"spec.replicas": {Raw: "2"}},
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want conventions accepted alongside a native resource", err)
	}
	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	doc := string(comp)
	// the managed queues keep their convention calls…
	if !strings.Contains(doc, `include "cf.tags"`) {
		t.Error("managed resources lost their convention template calls")
	}
	// …and the native document between its name annotation and the next
	// resource carries none.
	webStart := strings.Index(doc, `setResourceNameAnnotation "web"`)
	if webStart < 0 {
		t.Fatal("native resource document not found in the Composition")
	}
	webEnd := strings.Index(doc[webStart:], "setResourceNameAnnotation")
	rest := doc[webStart:]
	if next := strings.Index(rest[1:], "setResourceNameAnnotation"); next > 0 {
		rest = rest[:next+1]
	}
	_ = webEnd
	if strings.Contains(rest, `include "cf.`) {
		t.Error("a convention template call leaked into the native document")
	}
}

// The parallel ruling for template: fields on a native resource — the
// template call's output re-indents to the fixed forProvider column, which a
// native leaf at an arbitrary depth breaks — refused at Validate and again in
// the emitter for direct Composition callers.
func TestTemplateFieldsAreRefusedOnNativeKinds(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	b.Spec.Conventions = nil
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"spec.template.metadata.labels": {Template: "cf.tags"},
		},
	})
	if err := b.Validate(); err == nil {
		t.Error("Validate accepted a template: field on a native resource; the v1 ruling refuses it")
	}
	_, err = Composition(b, crds)
	if err == nil {
		t.Fatal("Composition accepted a template: field on a native resource")
	}
	for _, want := range []string{"web", "template", "native"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestTemplateObservedContextAccess proves that a template can access .observed
// to read composed resource status cleanly.
func TestTemplateObservedContextAccess(t *testing.T) {
	bp := conventionTestBlueprint()
	bp.Spec.Templates["cf.statusPolicy"] = `{{ if hasKey .observed "resources" }}arn: {{ (index .observed.resources "queue-a").resource.status.atProvider.arn }}{{ end }}`
	bp.Spec.Resources[1].Fields["name"] = blueprint.Field{Template: "cf.statusPolicy"}

	got, err := Composition(bp, conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, got)
	if !strings.Contains(tmpl, `include "cf.statusPolicy"`) {
		t.Fatalf("emitted template does not include cf.statusPolicy: %s", tmpl)
	}
}

func TestCF109ConventionMatchingNativeKindIsRefused(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	// Add a convention that matches a top-level leaf field on Secret (e.g. "immutable" or "type")
	b.Spec.Conventions = append(b.Spec.Conventions, blueprint.Convention{
		Match:    "immutable",
		Template: "cf.tags",
	})
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name:     "secret",
		Kind:     "Secret",
		Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"type": {Value: "Opaque"},
		},
	})

	_, err = Composition(b, crds)
	if err == nil {
		t.Fatal("expected error when convention matches native kind field, got nil")
	}
	wantMsg := `resource "secret": conventions cannot match native Kubernetes kind`
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("expected error containing %q, got: %v", wantMsg, err)
	}
}

// Explicit declaration on native kind wins over a matching convention: that IS
// the override mechanism, so the resource is not refused and no template call leaks.
func TestCF109ConventionExplicitOverrideOnNativeKindAllowed(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	// "name" would name the Secret's metadata.name at depth (CF-149); the
	// override under test is immutable alone.
	b.Spec.Conventions = []blueprint.Convention{
		{Match: "tags", Template: "cf.tags"},
		{Match: "immutable", Template: "cf.tags"},
	}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name:     "secret",
		Kind:     "Secret",
		Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"type":      {Value: "Opaque"},
			"immutable": {Value: "true"},
		},
	})

	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v, want explicit override on native kind allowed", err)
	}
	doc := string(comp)
	secretStart := strings.Index(doc, `setResourceNameAnnotation "secret"`)
	if secretStart < 0 {
		t.Fatal("native secret resource document not found in Composition")
	}
	rest := doc[secretStart:]
	if next := strings.Index(rest[1:], "setResourceNameAnnotation"); next > 0 {
		rest = rest[:next+1]
	}
	if strings.Contains(rest, `include "cf.tags"`) {
		t.Error("convention template call leaked into native secret document")
	}
}

// CF-149: the CF-109 refusal only inspected the TOP-LEVEL leaves of a native
// kind's field tree, so `match: replicas` against a Deployment (whose
// replicas lives at spec.replicas) was still silently ignored — cf gen exited
// 0, the define block was emitted and never called, and the Deployment
// reached the cluster without a replicas field. A convention that matches a
// field at ANY depth of a native kind is refused with the documented error,
// exercised here on the real vendored apps/v1 Deployment.
func TestCF149ConventionMatchingNestedNativeFieldIsRefused(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	// Only conventions that name no Deployment field, plus the one under
	// test: replicas exists on Deployment solely as the nested spec.replicas.
	b.Spec.Conventions = []blueprint.Convention{
		{Match: "tags", Template: "cf.tags"},
		{Match: "replicas", Template: "cf.tags"},
	}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"spec.selector.matchLabels":              {Raw: "{app: web}"},
			"spec.template.metadata.labels":          {Raw: "{app: web}"},
			"spec.template.spec.containers[0].name":  {Value: "web"},
			"spec.template.spec.containers[0].image": {Value: "nginx"},
		},
	})

	_, err = Composition(b, crds)
	if err == nil {
		t.Fatal("Composition accepted a convention matching the nested Deployment field spec.replicas; want the documented refusal")
	}
	wantMsg := `resource "web": conventions cannot match native Kubernetes kind`
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("expected error containing %q, got: %v", wantMsg, err)
	}
	if !strings.Contains(err.Error(), "spec.replicas") {
		t.Errorf("error %q does not name the matched field spec.replicas", err)
	}
}

// The override mechanism holds at depth too: a Deployment that sets
// spec.replicas explicitly is not refused, and no convention call leaks into
// the native document.
func TestCF149ConventionExplicitNestedOverrideOnNativeKindAllowed(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, conventionCRDs(t)...)

	b := conventionTestBlueprint()
	b.Spec.Conventions = []blueprint.Convention{
		{Match: "tags", Template: "cf.tags"},
		{Match: "replicas", Template: "cf.tags"},
	}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"spec.replicas":                          {Raw: "2"},
			"spec.selector.matchLabels":              {Raw: "{app: web}"},
			"spec.template.metadata.labels":          {Raw: "{app: web}"},
			"spec.template.spec.containers[0].name":  {Value: "web"},
			"spec.template.spec.containers[0].image": {Value: "nginx"},
		},
	})

	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v, want explicit spec.replicas to override the convention", err)
	}
	doc := string(comp)
	webStart := strings.Index(doc, `setResourceNameAnnotation "web"`)
	if webStart < 0 {
		t.Fatal("native Deployment document not found in the Composition")
	}
	rest := doc[webStart:]
	if next := strings.Index(rest[1:], "setResourceNameAnnotation"); next > 0 {
		rest = rest[:next+1]
	}
	if strings.Contains(rest, `include "cf.`) {
		t.Error("a convention template call leaked into the native Deployment document")
	}
}

// CF-176: conventions apply to top-level forProvider leaves only (no dots,
// no array indices). A convention whose match names a nested leaf of a managed
// kind (and does not match a top-level field) must not be silently ignored:
// it is refused loudly unless explicitly overridden.
func TestCF176ConventionMatchingNestedManagedFieldIsRefused(t *testing.T) {
	crds := conventionCRDs(t)

	b := conventionTestBlueprint()
	// conventionCRDs has Queue with:
	// - region (top)
	// - name (top)
	// - tags (top)
	// - endpoint.hostname (nested)
	// Add a convention matching only the nested endpoint.hostname leaf:
	b.Spec.Conventions = append(b.Spec.Conventions, blueprint.Convention{
		Match:    "hostname",
		Template: "cf.name",
	})

	_, err := Composition(b, crds)
	if err == nil {
		t.Fatal("Composition accepted a convention matching nested Queue field endpoint.hostname; want refusal")
	}
	wantMsg := `resource "queue-a": conventions apply to top-level forProvider fields only`
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("expected error containing %q, got: %v", wantMsg, err)
	}
	if !strings.Contains(err.Error(), "endpoint.hostname") {
		t.Errorf("error %q does not name the matched field endpoint.hostname", err)
	}
}

// The override mechanism holds for nested managed fields too: if the blueprint
// sets the nested field explicitly (or sets its parent object), the refusal does
// not trigger, and the convention template does not leak into the nested field.
func TestCF176ConventionExplicitNestedOverrideOnManagedKindAllowed(t *testing.T) {
	crds := conventionCRDs(t)

	b := conventionTestBlueprint()
	b.Spec.Conventions = append(b.Spec.Conventions, blueprint.Convention{
		Match:    "hostname",
		Template: "cf.name",
	})
	// queue-a and queue-b are in conventionTestBlueprint. Explicitly override
	// endpoint.hostname on both:
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Fields == nil {
			b.Spec.Resources[i].Fields = make(map[string]blueprint.Field)
		}
		b.Spec.Resources[i].Fields["endpoint.hostname"] = blueprint.Field{Value: "custom.endpoint"}
	}

	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v, want explicit endpoint.hostname to override the convention", err)
	}
	doc := string(comp)
	if strings.Contains(doc, `include "cf.name" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "queue-a" "field" "endpoint.hostname")`) {
		t.Error("convention template call leaked into endpoint.hostname")
	}
}
