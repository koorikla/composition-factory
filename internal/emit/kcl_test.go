package emit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

func TestEmitKCLComposition(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      region:
        type: string
        required: true
        description: Target AWS region
      retention:
        type: integer
        default: "345600"
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          from: params.region
        messageRetentionSeconds:
          from: params.retention
`

	dir := t.TempDir()
	path := filepath.Join(dir, "xqueue.cf.yaml")
	if err := os.WriteFile(path, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}

	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint.Load: %v", err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  messageRetentionSeconds: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)

	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}

	compStr := string(compBytes)

	// Verify function-kcl step
	if !strings.Contains(compStr, "name: function-kcl") {
		t.Errorf("expected function-kcl in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "apiVersion: krm.kcl.dev/v1alpha1") {
		t.Errorf("expected krm.kcl.dev/v1alpha1 apiVersion in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "kind: KCLInput") {
		t.Errorf("expected KCLInput kind in composition, got:\n%s", compStr)
	}

	// Verify KCL source content
	if !strings.Contains(compStr, `oxr = option("params")?.oxr or {}`) {
		t.Errorf("expected oxr preamble in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `"krm.kcl.dev/composition-resource-name" = "work-queue"`) {
		t.Errorf("expected composition-resource-name in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "region = _spec?.region") {
		t.Errorf("expected region field wire in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "messageRetentionSeconds = _spec?.retention") {
		t.Errorf("expected messageRetentionSeconds field wire in KCL, got:\n%s", compStr)
	}

	// Verify functions.yaml
	fnBytes, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	fnStr := string(fnBytes)
	if !strings.Contains(fnStr, "name: function-kcl") {
		t.Errorf("expected function-kcl in functions.yaml, got:\n%s", fnStr)
	}
	if !strings.Contains(fnStr, "package: xpkg.upbound.io/crossplane-contrib/function-kcl:v0.11.2") {
		t.Errorf("expected function-kcl package in functions.yaml, got:\n%s", fnStr)
	}
}

func TestTranslateWhenToKCL_BooleanSubstrings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "params.is_true_enabled == true",
			want: "_spec?.is_true_enabled == True",
		},
		{
			in:   "params.use_false_fallback == false",
			want: "_spec?.use_false_fallback == False",
		},
		{
			in:   "params.truename != false",
			want: "_spec?.truename != False",
		},
	}

	for _, tc := range cases {
		got := translateWhenToKCL(tc.in)
		if got != tc.want {
			t.Errorf("translateWhenToKCL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTranslateWhenToKCL_StringComparisons(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   `params.version == "1.0"`,
			want: `_spec?.version == "1.0"`,
		},
		{
			in:   `params.code == "123"`,
			want: `_spec?.code == "123"`,
		},
		{
			in:   `params.flag == "false"`,
			want: `_spec?.flag == "false"`,
		},
		{
			in:   `params.flag == "true"`,
			want: `_spec?.flag == "true"`,
		},
		{
			in:   "params.enabled",
			want: "_spec?.enabled",
		},
	}

	for _, tc := range cases {
		got := translateWhenToKCL(tc.in)
		if got != tc.want {
			t.Errorf("translateWhenToKCL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKCLCompositionWhenCondition(t *testing.T) {
	b := testBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	b.Spec.XRD.Parameters["version"] = blueprint.Parameter{Type: "string", Default: "1.0"}
	b.Spec.XRD.Parameters["flag"] = blueprint.Parameter{Type: "string", Default: "false"}
	b.Spec.Resources = append(b.Spec.Resources,
		blueprint.Resource{
			Name:   "queue-v1",
			Kind:   "Queue",
			When:   `params.version == "1.0"`,
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
		blueprint.Resource{
			Name:   "queue-flag",
			Kind:   "Queue",
			When:   `params.flag == "false"`,
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
	)
	out, err := Composition(b, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `if _spec?.version == "1.0":`) {
		t.Errorf("expected kcl composition to contain 'if _spec?.version == \"1.0\":', got:\n%s", s)
	}
	if !strings.Contains(s, `if _spec?.flag == "false":`) {
		t.Errorf("expected kcl composition to contain 'if _spec?.flag == \"false\":', got:\n%s", s)
	}
}

func TestKCLStatusWireReadsStatusPathOnce(t *testing.T) {
	got := kclStructuredRHS(structuredRHS{kind: rhsStatus, resource: "role", statusPath: "atProvider.arn"}, "")
	want := `ocds?["role"]?.Resource?.status?.atProvider?.arn`
	if got != want {
		t.Errorf("kclStructuredRHS = %q, want %q", got, want)
	}

	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	out, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatal(err)
	}
	line := lineContaining(t, string(out), "queueUrl =")
	if n := strings.Count(line, "atProvider"); n != 1 {
		t.Errorf("queueUrl wire mentions atProvider %d times, want 1:\n%s", n, line)
	}
	if !strings.Contains(line, `ocds?["main-queue"]?.Resource?.status?.atProvider?.url`) {
		t.Errorf("queueUrl wire = %s", line)
	}
}

func TestTranslateForEachToKCL_StatusBoundReadsPathOnce(t *testing.T) {
	got := translateForEachToKCL("resources.main-queue.status.atProvider.nodeCount")
	want := `range(0, int(ocds?["main-queue"]?.Resource?.status?.atProvider?.nodeCount or 0))`
	if got != want {
		t.Errorf("translateForEachToKCL = %q, want %q", got, want)
	}
}

func TestKCLEnvelopeNesting(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-env
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      secretName:
        type: string
        required: true
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
      envelope:
        writeConnectionSecretToRef.name:
          from: params.secretName
        writeConnectionSecretToRef.namespace:
          value: crossplane-system
        managementPolicies:
          value: "*"
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                required: [name, namespace]
                properties: {name: {type: string}, namespace: {type: string}}
              managementPolicies:
                type: array
                items: {type: string}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "writeConnectionSecretToRef.name") {
		t.Errorf("flattened dot path found in KCL output:\n%s", s)
	}
	if !strings.Contains(s, "writeConnectionSecretToRef = {\n") {
		t.Errorf("expected nested writeConnectionSecretToRef object in KCL:\n%s", s)
	}
	if !strings.Contains(s, "name = _spec?.secretName") {
		t.Errorf("expected name child in KCL:\n%s", s)
	}
	if !strings.Contains(s, `namespace = "crossplane-system"`) {
		t.Errorf("expected namespace child in KCL:\n%s", s)
	}
}

func TestKCLRefusesTemplateConventions(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-conv
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
  templates:
    cf.name: "{{ .xr }}-{{ .resource }}"
  conventions:
    - match: name
      template: cf.name
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
`
	var b blueprint.Blueprint
	if err := yaml.Unmarshal([]byte(bpYAML), &b); err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, _ := schema.ParseCRDs([][]byte{crdDoc})
	_, err := Composition(&b, crds)
	if err == nil {
		t.Fatal("expected error on KCL with conventions, got nil")
	}
	if !strings.Contains(err.Error(), "conventions") {
		t.Errorf("expected error to mention conventions, got: %v", err)
	}
}

// lineContaining returns the single line of s that contains needle.
func lineContaining(t *testing.T, s, needle string) string {
	t.Helper()
	var hits []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, needle) {
			hits = append(hits, strings.TrimSpace(l))
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly one line containing %q, got %d:\n%s", needle, len(hits), s)
	}
	return hits[0]
}

func TestKCLForEachSyntaxAndLoopNaming(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-loop
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      count: {type: integer, default: "3"}
  resources:
    - name: replica-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      forEach: params.count
      fields:
        region:
          value: us-east-1
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, _ := schema.ParseCRDs([][]byte{crdDoc})
	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "*[\n") {
		t.Errorf("expected list comprehension unpacking *[ in KCL, got:\n%s", s)
	}
	if !strings.Contains(s, `} for _i in range(0, int(_spec?.count or 0))`) {
		t.Errorf("expected comprehension footer in KCL, got:\n%s", s)
	}
	if !strings.Contains(s, `"krm.kcl.dev/composition-resource-name" = "replica-queue-${_i}"`) {
		t.Errorf("expected loop instance naming replica-queue-${_i}, got:\n%s", s)
	}
	if strings.Contains(s, `"${_i}-replica-queue"`) {
		t.Errorf("found old loop instance naming ${_i}-replica-queue in output:\n%s", s)
	}
	if strings.Contains(s, "for _i in range") && !strings.Contains(s, "} for _i in range") {
		t.Errorf("found invalid statement-style for loop in list:\n%s", s)
	}
}

func TestKCLRefusesGoTemplateInRaw(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue"},
	}
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	b.Spec.Sources = []blueprint.Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0"}}
	b.Spec.XRD = blueprint.XRD{
		Group: "aws.example.org", Version: "v1alpha1", Kind: "XQueue", Plural: "xqueues", Scope: "Namespaced",
		Parameters: map[string]blueprint.Parameter{"providerName": {Type: "string", Required: true}},
	}
	b.Spec.Sources = []blueprint.Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0"}}
	b.Spec.Resources = []blueprint.Resource{{
		Name: "work-queue", Kind: "Queue", Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
		Fields: map[string]blueprint.Field{"region": {Raw: "{{ $spec.region }}"}},
	}}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, _ := schema.ParseCRDs([][]byte{crdDoc})
	_, err := Composition(b, crds)
	if err == nil {
		t.Fatal("expected error on KCL with Go-template in raw field, got nil")
	}
	if !strings.Contains(err.Error(), "Go-template syntax") || !strings.Contains(err.Error(), "go-templating engine") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKCLStatusWireEmitsConditionalGuard(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	out, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `if ocds?["main-queue"]?.Resource?.status?.atProvider?.url != None:`) {
		t.Errorf("expected conditional status guard in KCL output:\n%s", s)
	}
}

func TestKCLDottedPathNesting(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xcomplex
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XComplex
    plural: xcomplexes
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  resources:
    - name: bucket-item
      provider: xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0
      kind: Bucket
      fields:
        bucketRef.name:
          value: "my-bucket"
        tags[team]:
          value: "infra"
        containers[0].name:
          value: "web"
        containers[0].image:
          value: "nginx:latest"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "xcomplex.cf.yaml")
	if err := os.WriteFile(path, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}

	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint.Load: %v", err)
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: buckets.s3.aws.m.upbound.io}
spec:
  group: s3.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Bucket, plural: buckets, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                properties:
                  bucketRef:
                    type: object
                    properties:
                      name: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  containers:
                    type: array
                    items:
                      type: object
                      properties:
                        name: {type: string}
                        image: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(compBytes)

	// Verify nested structure and ensure no flat dotted keys exist
	if strings.Contains(s, `"bucketRef.name"`) || strings.Contains(s, `"containers[0].name"`) {
		t.Fatalf("KCL output contains literal dotted keys:\n%s", s)
	}
	if !strings.Contains(s, "bucketRef = {\n") || !strings.Contains(s, "name = \"my-bucket\"") {
		t.Errorf("KCL output missing nested bucketRef object:\n%s", s)
	}
	if !strings.Contains(s, "containers = [\n") || !strings.Contains(s, "image = \"nginx:latest\"") {
		t.Errorf("KCL output missing nested containers array:\n%s", s)
	}
	if !strings.Contains(s, "tags = {\n") || !strings.Contains(s, "team = \"infra\"") {
		t.Errorf("KCL output missing nested tags map:\n%s", s)
	}
}

func TestEmitKCLNativeMetadataLabels(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "native-labels-kcl"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group: "example.org", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"appName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.labels[app]":  {From: "params.appName"},
						"metadata.labels[tier]": {Value: "backend"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	compBytes, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(compBytes)
	if !strings.Contains(s, "labels = {") ||
		!strings.Contains(s, "app = _spec?.appName") ||
		!strings.Contains(s, `tier = "backend"`) {
		t.Errorf("KCL missing metadata.labels:\n%s", s)
	}
}

func TestKCLTypedObjectMemberWiresEmitConditionalGuards(t *testing.T) {
	b, err := blueprint.Load("../../testdata/xqueue-typedobj.cf.yaml")
	if err != nil {
		t.Fatalf("blueprint.Load: %v", err)
	}
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  maxMessageSize: {type: integer}
                  messageRetentionSeconds: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)

	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(compBytes)

	// Nested typed-object member wires must be conditionally guarded so that
	// an XR omitting the optional object does not serialise null fields (CF-231).
	wantMaxSize := "if _spec?.tuning?.maxSize != None:\n"
	wantRetention := "if _spec?.tuning?.retention != None:\n"

	if !strings.Contains(s, wantMaxSize) {
		t.Errorf("KCL missing conditional guard for tuning.maxSize, got:\n%s", s)
	}
	if !strings.Contains(s, wantRetention) {
		t.Errorf("KCL missing conditional guard for tuning.retention, got:\n%s", s)
	}
}

func TestKCLStatusWireNoneCheck(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"example.com/url": {From: "resources.main-queue.status.atProvider.url"},
	}
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name:     "sa",
		Kind:     "ServiceAccount",
		Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"metadata.name": {From: "resources.main-queue.status.atProvider.url"},
		},
	})

	crds := append(nativeTestCRDs(t), wireCRDs(t)...)
	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)

	// Status wire conditions in KCL must check `!= None:` rather than truthiness,
	// so falsy status values (such as false, 0, "") are preserved and not dropped (CF-286).
	wantGuard := `if ocds?["main-queue"]?.Resource?.status?.atProvider?.url != None:`
	if !strings.Contains(s, wantGuard) {
		t.Errorf("expected None guard for status wire %q in KCL output, got:\n%s", wantGuard, s)
	}

	// Verify that bare truthiness guards (which drop falsy values) are NOT present.
	unwantedGuard := `if ocds?["main-queue"]?.Resource?.status?.atProvider?.url:`
	if strings.Contains(s, unwantedGuard) {
		t.Errorf("found bare truthiness guard %q in KCL output (must use != None):\n%s", unwantedGuard, s)
	}
}

func TestKCLWriteConnectionSecretToRefOptionalOmitted(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xsec
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XSec
    plural: xsecs
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      secretName:
        type: string
        required: false
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
      envelope:
        writeConnectionSecretToRef.name:
          from: params.secretName
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                required: [name]
                properties: {name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)

	// An envelope subtree whose child parameters are optional must be guarded
	// so that an XR omitting secretName does not emit an empty dictionary
	// writeConnectionSecretToRef = {}, which Crossplane admission rejects (CF-291).
	wantGuard := "if _spec?.secretName != None:"
	if !strings.Contains(s, wantGuard) {
		t.Errorf("expected conditional guard %q for writeConnectionSecretToRef in KCL output, got:\n%s", wantGuard, s)
	}

	// Verify that unconditional envelope mapping is NOT emitted.
	// In KCL, writeConnectionSecretToRef must be indented inside the guard rather than
	// emitted unconditionally as `            writeConnectionSecretToRef = {\n`.
	unconditionalKey := "\n            writeConnectionSecretToRef = {\n"
	if strings.Contains(s, unconditionalKey) {
		t.Errorf("found unconditional envelope mapping in KCL output:\n%s", s)
	}
}

func TestKCLOptionalEnvelopeGuardingVariations(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xsec-variations
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XSecVar
    plural: xsecvars
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      secretName:
        type: string
        required: false
      secretNamespace:
        type: string
        required: false
      configRefName:
        type: string
        required: false
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
      envelope:
        writeConnectionSecretToRef.name:
          from: params.secretName
        writeConnectionSecretToRef.namespace:
          from: params.secretNamespace
        publishConnectionDetailsTo.configRef.name:
          from: params.configRefName
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                required: [name, namespace]
                properties: {name: {type: string}, namespace: {type: string}}
              publishConnectionDetailsTo:
                type: object
                properties:
                  configRef:
                    type: object
                    required: [name]
                    properties: {name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)

	// Multi-child envelope with all optional leaves must guard parent with disjunction
	// and children with their own individual guards.
	wantDisjunction := "if _spec?.secretName != None or _spec?.secretNamespace != None:"
	if !strings.Contains(s, wantDisjunction) {
		t.Errorf("expected disjunction guard %q for writeConnectionSecretToRef in KCL output, got:\n%s", wantDisjunction, s)
	}
	if !strings.Contains(s, "if _spec?.secretName != None:") || !strings.Contains(s, "name = _spec?.secretName") {
		t.Errorf("expected child guard for secretName in KCL output, got:\n%s", s)
	}
	if !strings.Contains(s, "if _spec?.secretNamespace != None:") || !strings.Contains(s, "namespace = _spec?.secretNamespace") {
		t.Errorf("expected child guard for secretNamespace in KCL output, got:\n%s", s)
	}

	// Nested envelope mapping publishConnectionDetailsTo.configRef.name must guard
	// publishConnectionDetailsTo with configRefName check without repeating identical guard on configRef or name.
	if !strings.Contains(s, "if _spec?.configRefName != None:") ||
		!strings.Contains(s, "publishConnectionDetailsTo = {") ||
		!strings.Contains(s, "configRef = {") ||
		!strings.Contains(s, "name = _spec?.configRefName") {
		t.Errorf("expected nested envelope structure with deduplicated guard in KCL output, got:\n%s", s)
	}
	// Verify configRef does NOT get a redundant duplicate guard
	redundantGuard := "configRef = {\n                                      if _spec?.configRefName != None:"
	if strings.Contains(s, redundantGuard) {
		t.Errorf("found redundant duplicate guard inside configRef:\n%s", s)
	}
}

func TestCF301_KCLLoopedCustomMetadataNameIncludesIndex(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-looped-name-kcl"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XLoopedName", Plural: "xloopednames",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"prefix":   {Type: "string"},
					"replicas": {Type: "integer", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "worker",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					ForEach:  "params.replicas",
					Fields: map[string]blueprint.Field{
						"metadata.name": {From: "params.prefix"},
					},
				},
			},
		},
	}
	out, err := Composition(bp, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `name = "${_spec?.prefix}-${_i}"`) {
		t.Fatalf("expected KCL looped custom metadata.name to incorporate loop index _i, got:\n%s", s)
	}
}

func TestCF301_KCLLoopedCustomMetadataNameLiteralValue(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-looped-name-kcl-lit"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XLoopedNameLit", Plural: "xloopednamelits",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"replicas": {Type: "integer", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "worker",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					ForEach:  "params.replicas",
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "worker-custom"},
					},
				},
			},
		},
	}
	out, err := Composition(bp, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `name = "worker-custom-${_i}"`) {
		t.Fatalf("expected KCL literal name to incorporate loop index _i, got:\n%s", s)
	}
}

var fakeQueueCRD = []byte(`
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
            required: [forProvider]
            properties:
              forProvider:
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)

func TestKCLRefusesSpecTemplates(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-tmpl
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      region:
        type: string
        required: true
  templates:
    cf.tags: "{{ .xr }}-tags"
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          from: params.region
`
	var b blueprint.Blueprint
	if err := yaml.Unmarshal([]byte(bpYAML), &b); err != nil {
		t.Fatal(err)
	}
	crds, err := schema.ParseCRDs([][]byte{fakeQueueCRD})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Composition(&b, crds)
	if err == nil {
		t.Fatal("expected error on KCL with spec.templates, got nil")
	}
	if !strings.Contains(err.Error(), "spec.templates") {
		t.Errorf("expected error to mention spec.templates, got: %v", err)
	}
}

func TestMetadataRefCustomNameTargetKCL(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"image": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name":                {Value: "custom-sa"},
						"automountServiceAccountToken": {Value: "true"},
					},
				},
				{
					Name: "app", Kind: "Deployment", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector.matchLabels":              {Raw: "{app: web}"},
						"spec.template.metadata.labels":          {Raw: "{app: web}"},
						"spec.template.spec.containers[0].name":  {Value: "app"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
						"spec.template.spec.serviceAccountName":  {From: "resources.sa.metadata.name"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if !strings.Contains(s, `serviceAccountName = "custom-sa"`) {
		t.Fatalf("expected KCL to contain custom serviceAccountName, got:\n%s", s)
	}
}

func TestMetadataRefCustomNameTargetParamKCL(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"image":  {Type: "string", Required: true},
					"saName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name":                {From: "params.saName"},
						"automountServiceAccountToken": {Value: "true"},
					},
				},
				{
					Name: "app", Kind: "Deployment", Provider: blueprint.NativeProvider,
					Annotations: map[string]blueprint.Field{
						"app.kubernetes.io/sa-ref": {From: "resources.sa.metadata.name"},
					},
					Fields: map[string]blueprint.Field{
						"spec.selector.matchLabels":              {Raw: "{app: web}"},
						"spec.template.metadata.labels":          {Raw: "{app: web}"},
						"spec.template.spec.containers[0].name":  {Value: "app"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
						"spec.template.spec.serviceAccountName":  {From: "resources.sa.metadata.name"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if !strings.Contains(s, `serviceAccountName = _spec?.saName`) {
		t.Fatalf("expected KCL to contain serviceAccountName = _spec?.saName, got:\n%s", s)
	}
	if !strings.Contains(s, `"app.kubernetes.io/sa-ref" = _spec?.saName`) {
		t.Fatalf("expected KCL to contain annotation ref, got:\n%s", s)
	}
}

func TestCF430_KCLArrayEmissionSuppressesEmptyElements(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-array"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group:   "test.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"extraEnvName": {Type: "string"},
					"extraEnvVal":  {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "app",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.template.spec.containers[0].name":         {Value: "my-app"},
						"spec.template.spec.containers[0].image":        {Value: "nginx"},
						"spec.template.spec.containers[0].env[0].name":  {From: "params.extraEnvName"},
						"spec.template.spec.containers[0].env[0].value": {From: "params.extraEnvVal"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	crds := nativeTestCRDs(t)
	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if strings.Contains(s, "env = [\n") && !strings.Contains(s, "if _spec?.extraEnvName != None") {
		t.Fatalf("expected KCL to guard conditional array env, got:\n%s", s)
	}
	if !strings.Contains(s, "if _spec?.extraEnvName != None or _spec?.extraEnvVal != None:") {
		t.Fatalf("expected KCL to guard env with disjunction of optional child fields, got:\n%s", s)
	}

	// Also verify multi-element array: element 0 static, element 1 optional
	bMulti := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-multi-array"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group:   "test.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"extraEnvName": {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "app",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.template.spec.containers[0].name":         {Value: "my-app"},
						"spec.template.spec.containers[0].image":        {Value: "nginx"},
						"spec.template.spec.containers[0].env[0].name":  {Value: "PORT"},
						"spec.template.spec.containers[0].env[0].value": {Value: "8080"},
						"spec.template.spec.containers[0].env[1].name":  {From: "params.extraEnvName"},
					},
				},
			},
		},
	}
	if err := bMulti.Validate(); err != nil {
		t.Fatalf("Validate bMulti: %v", err)
	}
	compMulti, err := Composition(bMulti, crds)
	if err != nil {
		t.Fatalf("Composition bMulti: %v", err)
	}
	sMulti := string(compMulti)
	if !strings.Contains(sMulti, "if _spec?.extraEnvName != None:") {
		t.Fatalf("expected KCL to guard individual optional element 1 in multi-element array, got:\n%s", sMulti)
	}

	// Runtime verification using Docker if available
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		return
	}

	kclBody, err := kclTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("kclTemplateBody: %v", err)
	}

	// Test case 1: parameters omitted -> output must not have env or [{}]
	cmd1 := exec.Command(dockerBin, "run", "-i", "--rm", "kcllang/kcl:v0.11.0", "kcl", "run",
		"-D", `params={"oxr": {"metadata": {"name": "test-app"}, "spec": {}}}`, "-")
	cmd1.Stdin = strings.NewReader(kclBody)
	out1, err := cmd1.CombinedOutput()
	if err != nil {
		t.Fatalf("kcl docker execution failed: %v\nOutput:\n%s", err, out1)
	}
	out1Str := string(out1)
	if strings.Contains(out1Str, "env:") {
		t.Fatalf("expected env to be omitted in KCL output when optional params omitted, got:\n%s", out1Str)
	}

	// Test case 2: parameters populated -> output has env
	cmd2 := exec.Command(dockerBin, "run", "-i", "--rm", "kcllang/kcl:v0.11.0", "kcl", "run",
		"-D", `params={"oxr": {"metadata": {"name": "test-app"}, "spec": {"extraEnvName": "MY_VAR", "extraEnvVal": "val"}}}`, "-")
	cmd2.Stdin = strings.NewReader(kclBody)
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("kcl docker execution failed: %v\nOutput:\n%s", err, out2)
	}
	out2Str := string(out2)
	if !strings.Contains(out2Str, "name: MY_VAR") || !strings.Contains(out2Str, "value: val") {
		t.Fatalf("expected env to be populated in KCL output, got:\n%s", out2Str)
	}
}

func TestCF433_KCLAnnotationsOptionalGuard(t *testing.T) {
	// 1. Verify kclNodeFieldGuard returns expected guards for optional params, env keys, and status fields
	optParamField := &forProviderField{
		path: "example.com/size",
		structured: structuredRHS{
			kind:      rhsParam,
			param:     "maxMessageSize",
			paramSegs: []string{"maxMessageSize"},
			optional:  true,
		},
	}
	if g := kclNodeFieldGuard(optParamField); g != "_spec?.maxMessageSize != None" {
		t.Fatalf("kclNodeFieldGuard(optParamField) = %q, want %q", g, "_spec?.maxMessageSize != None")
	}

	optEnvField := &forProviderField{
		path: "example.com/region",
		structured: structuredRHS{
			kind:      rhsEnv,
			param:     "CLUSTER_REGION",
			paramSegs: []string{"CLUSTER_REGION"},
			optional:  true,
		},
	}
	if g := kclNodeFieldGuard(optEnvField); g != "_env?.CLUSTER_REGION != None" {
		t.Fatalf("kclNodeFieldGuard(optEnvField) = %q, want %q", g, "_env?.CLUSTER_REGION != None")
	}

	reqField := &forProviderField{
		path: "example.com/app",
		structured: structuredRHS{
			kind:      rhsParam,
			param:     "appName",
			paramSegs: []string{"appName"},
			optional:  false,
		},
	}
	if g := kclNodeFieldGuard(reqField); g != "" {
		t.Fatalf("kclNodeFieldGuard(reqField) = %q, want empty string", g)
	}

	// 2. Blueprint emission verification
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-annotations-guard"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
			XRD: blueprint.XRD{
				Group:   "test.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName":   {Type: "string", Required: true},
					"maxMessageSize": {Type: "integer"}, // optional integer
					"team":           {Type: "string"},  // optional string
					"appName":        {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "main-queue",
					Kind: "Queue",
					Annotations: map[string]blueprint.Field{
						"example.com/size": {From: "params.maxMessageSize"},
						"example.com/team": {From: "params.team"},
						"example.com/app":  {From: "params.appName"},
					},
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-west-1"},
					},
				},
				{
					Name: "consumer",
					Kind: "Queue",
					Annotations: map[string]blueprint.Field{
						"example.com/queue-arn": {From: "resources.main-queue.status.atProvider.arn"},
					},
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-west-1"},
					},
				},
			},
		},
	}

	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	crds := nativeTestCRDs(t)
	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)

	kclBody, err := kclTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("kclTemplateBody: %v", err)
	}

	// Verify guards are present in KCL annotations
	expectedSizeGuard := "                if _spec?.maxMessageSize != None:\n                    \"example.com/size\" = str(_spec?.maxMessageSize)"
	if !strings.Contains(kclBody, expectedSizeGuard) {
		t.Fatalf("expected kclBody to contain guarded optional integer annotation, got:\n%s", kclBody)
	}

	expectedTeamGuard := "                if _spec?.team != None:\n                    \"example.com/team\" = _spec?.team"
	if !strings.Contains(kclBody, expectedTeamGuard) {
		t.Fatalf("expected kclBody to contain guarded optional string annotation, got:\n%s", kclBody)
	}

	expectedStatusGuard := "                if ocds?[\"main-queue\"]?.Resource?.status?.atProvider?.arn != None:\n                    \"example.com/queue-arn\" = str(ocds?[\"main-queue\"]?.Resource?.status?.atProvider?.arn)"
	if !strings.Contains(kclBody, expectedStatusGuard) {
		t.Fatalf("expected kclBody to contain guarded status annotation, got:\n%s", kclBody)
	}

	// Required field must NOT have an if guard
	if strings.Contains(s, "if _spec?.appName != None:") {
		t.Fatalf("expected KCL to NOT guard required parameter appName, got:\n%s", s)
	}
	if !strings.Contains(s, "\"example.com/app\" = _spec?.appName") {
		t.Fatalf("expected KCL to contain required annotation, got:\n%s", s)
	}

	// 3. Runtime verification via Docker
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		return
	}

	// Case A: optional parameters omitted -> annotations for optional params and unobserved status MUST be omitted cleanly
	cmdA := exec.Command(dockerBin, "run", "-i", "--rm", "kcllang/kcl:v0.11.0", "kcl", "run",
		"-D", `params={"oxr": {"metadata": {"name": "test-app"}, "spec": {}}}`, "-")
	cmdA.Stdin = strings.NewReader(kclBody)
	outA, err := cmdA.CombinedOutput()
	if err != nil {
		t.Fatalf("kcl docker execution failed: %v\nOutput:\n%s", err, outA)
	}
	outAStr := string(outA)
	if strings.Contains(outAStr, "example.com/size") {
		t.Fatalf("expected example.com/size to be omitted when optional param is absent, got:\n%s", outAStr)
	}
	if strings.Contains(outAStr, "example.com/team") {
		t.Fatalf("expected example.com/team to be omitted when optional param is absent, got:\n%s", outAStr)
	}
	if strings.Contains(outAStr, "example.com/queue-arn") {
		t.Fatalf("expected example.com/queue-arn to be omitted when status is unobserved, got:\n%s", outAStr)
	}

	// Case B: optional parameters and status populated -> annotations must be present
	cmdB := exec.Command(dockerBin, "run", "-i", "--rm", "kcllang/kcl:v0.11.0", "kcl", "run",
		"-D", `params={"oxr": {"metadata": {"name": "test-app"}, "spec": {"providerName": "default", "appName": "my-app", "maxMessageSize": 2048, "team": "devops"}}, "ocds": {"main-queue": {"Resource": {"status": {"atProvider": {"arn": "arn:aws:sqs:eu-west-1:123456:queue"}}}}}}`, "-")
	cmdB.Stdin = strings.NewReader(kclBody)
	outB, err := cmdB.CombinedOutput()
	if err != nil {
		t.Fatalf("kcl docker execution failed: %v\nOutput:\n%s", err, outB)
	}
	outBStr := string(outB)
	if !strings.Contains(outBStr, `example.com/size: "2048"`) && !strings.Contains(outBStr, `example.com/size: '2048'`) && !strings.Contains(outBStr, `example.com/size: 2048`) {
		t.Fatalf("expected example.com/size to be populated in output, got:\n%s", outBStr)
	}
	if !strings.Contains(outBStr, "example.com/team: devops") {
		t.Fatalf("expected example.com/team to be devops, got:\n%s", outBStr)
	}
	if !strings.Contains(outBStr, "example.com/queue-arn: arn:aws:sqs:eu-west-1:123456:queue") {
		t.Fatalf("expected example.com/queue-arn to be populated, got:\n%s", outBStr)
	}
	if !strings.Contains(outBStr, "example.com/app: my-app") {
		t.Fatalf("expected example.com/app to be my-app, got:\n%s", outBStr)
	}
}
