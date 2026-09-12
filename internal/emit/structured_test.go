package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/adopt"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// TestRawJSONInStringField verifies CF-269:
// 1. Generate Composition for a blueprint defining a raw JSON policy string on a string field (spec.forProvider.policy).
// 2. Verify that the emitted YAML treats policy as a string scalar (validating with cf gen --validate schema validation).
// 3. Round-trip adopt the generated Composition through adopt.Adopt and assert that res.Fields["policy"] is retained with its raw JSON value and no drops are recorded for policy.
func TestRawJSONInStringField(t *testing.T) {
	const rawPolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"]}]}`

	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-iam:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "role-policy",
					Kind:     "RolePolicy",
					Provider: "xpkg.upbound.io/upbound/provider-aws-iam:v2",
					Fields: map[string]blueprint.Field{
						"policy": {Raw: rawPolicy},
					},
				},
			},
		},
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: rolepolicies.iam.aws.m.upbound.io}
spec:
  group: iam.aws.m.upbound.io
  scope: Namespaced
  names: {kind: RolePolicy, plural: rolepolicies, categories: [managed]}
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
                required: [policy]
                properties:
                  policy: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	// 1. Generate Composition for blueprint defining raw JSON policy string
	compBytes, err := Composition(bp, crds)
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	compStr := string(compBytes)

	// 2. Verify that the emitted YAML treats policy as a string scalar
	expectedPolicyScalar := `policy: '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"]}]}'`
	if !strings.Contains(compStr, expectedPolicyScalar) {
		t.Errorf("expected quoted YAML string scalar for policy field, got:\n%s", compStr)
	}
	if strings.Contains(compStr, `policy: {"Version":`) {
		t.Errorf("policy was emitted as an unquoted YAML mapping, expected quoted string scalar:\n%s", compStr)
	}

	// Validate rendered template with ValidateRendered (the validator used by cf gen --validate)
	rendered, err := renderTemplate(t, extractTemplate(t, compBytes), map[string]any{"providerName": "default"})
	if err != nil {
		t.Fatalf("renderTemplate failed: %v", err)
	}
	if err := ValidateRendered([]byte(rendered), crds); err != nil {
		t.Errorf("ValidateRendered failed: %v", err)
	}

	// 3. Round-trip adopt the generated Composition through adopt.Adopt
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs("xpkg.upbound.io/upbound/provider-aws-iam:v2", "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}
	adoptedBP, report, err := adopt.Adopt(compBytes, adopt.Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	res := adoptedBP.ResourceNamed("role-policy")
	if res == nil {
		t.Fatalf("adopted blueprint missing role-policy resource")
	}
	policyField, ok := res.Fields["policy"]
	if !ok {
		t.Fatalf("adopted blueprint missing policy field on role-policy: %+v", res.Fields)
	}
	val := policyField.Raw
	if val == "" {
		val = policyField.Value
	}
	if val != rawPolicy {
		t.Errorf("expected policy to have raw JSON value %q, got raw=%q value=%q", rawPolicy, policyField.Raw, policyField.Value)
	}

	for _, d := range report.Drops {
		if strings.Contains(d.Path, "policy") {
			t.Errorf("unexpected drop recorded for policy: %+v", d)
		}
	}
}

func TestRawJSONVariations(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-iam:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "role-policy",
					Kind:     "RolePolicy",
					Provider: "xpkg.upbound.io/upbound/provider-aws-iam:v2",
					Fields: map[string]blueprint.Field{
						"policy":      {Raw: `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"]}]}`},
						"policyArray": {Raw: `["s3:GetObject","s3:PutObject"]`},
						"actions":     {Raw: `["s3:GetObject","s3:PutObject"]`},
						"tags":        {Raw: `{"tier":"front"}`},
						"templated":   {Raw: `{{ $xr }}`},
					},
				},
			},
		},
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: rolepolicies.iam.aws.m.upbound.io}
spec:
  group: iam.aws.m.upbound.io
  scope: Namespaced
  names: {kind: RolePolicy, plural: rolepolicies, categories: [managed]}
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
                  policy: {type: string}
                  policyArray: {type: string}
                  actions: {type: array}
                  tags: {type: object}
                  templated: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(bp, crds)
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(compBytes)

	// String fields with raw JSON object or array must be emitted as quoted YAML scalars
	expectedPolicy := `policy: '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"]}]}'`
	if !strings.Contains(s, expectedPolicy) {
		t.Errorf("expected quoted YAML string scalar for policy field, got:\n%s", s)
	}

	expectedArrayInString := `policyArray: '["s3:GetObject","s3:PutObject"]'`
	if !strings.Contains(s, expectedArrayInString) {
		t.Errorf("expected quoted YAML string scalar for policyArray field, got:\n%s", s)
	}

	// Array destined for array-typed field must remain unquoted
	if !strings.Contains(s, `actions: ["s3:GetObject","s3:PutObject"]`) {
		t.Errorf("expected unquoted array for actions field, got:\n%s", s)
	}

	// Object destined for object-typed field must remain unquoted
	if strings.Contains(s, `tags: '{"tier":"front"}'`) {
		t.Errorf("expected unquoted object for tags field, got:\n%s", s)
	}

	// Template expression must remain unquoted template
	if !strings.Contains(s, `templated: {{ $xr }}`) {
		t.Errorf("expected templated: {{ $xr }}, got:\n%s", s)
	}
}

// TestStatusWireSchemaTypeCompatibility verifies CF-349:
// Wiring a status leaf to a field with an incompatible schema type must fail with
// an isFieldTypeCompatible error naming the mismatched types, while compatible status
// wires succeed.
func TestStatusWireSchemaTypeCompatibility(t *testing.T) {
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
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  fifoQueue: {type: boolean}
                  maxMessageSize: {type: integer}
                  url: {type: string}
          status:
            properties:
              atProvider:
                properties:
                  arn: {type: string}
                  fifoQueue: {type: boolean}
                  maxMessageSize: {type: integer}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	newBlueprint := func(targetField string, fromWire string) *blueprint.Blueprint {
		return &blueprint.Blueprint{
			APIVersion: blueprint.APIVersion,
			Kind:       blueprint.Kind,
			Metadata:   blueprint.Metadata{Name: "xqueue"},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "platform.example.org",
					Kind:    "XQueue",
					Plural:  "xqueues",
					Version: "v1alpha1",
					Scope:   "Namespaced",
					Parameters: map[string]blueprint.Parameter{
						"providerName": {Type: "string", Required: true},
					},
				},
				Resources: []blueprint.Resource{
					{
						Name: "queue1",
						Kind: "Queue",
						Fields: map[string]blueprint.Field{
							"region": {Value: "eu-north-1"},
						},
					},
					{
						Name: "queue2",
						Kind: "Queue",
						Fields: map[string]blueprint.Field{
							"region":    {Value: "eu-north-1"},
							targetField: {From: fromWire},
						},
					},
				},
			},
		}
	}

	t.Run("string status to boolean field fails with type incompatibility", func(t *testing.T) {
		bp := newBlueprint("fifoQueue", "resources.queue1.status.atProvider.arn")
		_, err := Composition(bp, crds)
		if err == nil {
			t.Fatal("expected Composition to fail when wiring string status to boolean field, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "the wire would render a YAML scalar of the wrong type") {
			t.Errorf("expected type incompatibility error, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, `"boolean"`) || !strings.Contains(errMsg, `"string"`) {
			t.Errorf("expected error to name mismatched types \"boolean\" and \"string\", got: %s", errMsg)
		}
	})

	t.Run("string status to integer field fails with type incompatibility", func(t *testing.T) {
		bp := newBlueprint("maxMessageSize", "resources.queue1.status.atProvider.arn")
		_, err := Composition(bp, crds)
		if err == nil {
			t.Fatal("expected Composition to fail when wiring string status to integer field, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "the wire would render a YAML scalar of the wrong type") {
			t.Errorf("expected type incompatibility error, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, `"integer"`) || !strings.Contains(errMsg, `"string"`) {
			t.Errorf("expected error to name mismatched types \"integer\" and \"string\", got: %s", errMsg)
		}
	})

	t.Run("compatible status types succeed", func(t *testing.T) {
		// String status to string target
		bpStr := newBlueprint("url", "resources.queue1.status.atProvider.arn")
		if _, err := Composition(bpStr, crds); err != nil {
			t.Errorf("wiring string status to string target failed: %v", err)
		}

		// Boolean status to boolean target
		bpBool := newBlueprint("fifoQueue", "resources.queue1.status.atProvider.fifoQueue")
		if _, err := Composition(bpBool, crds); err != nil {
			t.Errorf("wiring boolean status to boolean target failed: %v", err)
		}

		// Integer status to integer target
		bpInt := newBlueprint("maxMessageSize", "resources.queue1.status.atProvider.maxMessageSize")
		if _, err := Composition(bpInt, crds); err != nil {
			t.Errorf("wiring integer status to integer target failed: %v", err)
		}
	})
}

func TestMetadataNameByteTarget_GoTemplating(t *testing.T) {
	crds := nativeTestCRDs(t)

	t.Run("default naming to Secret data", func(t *testing.T) {
		b := &blueprint.Blueprint{
			APIVersion: "factory.crossplane.io/v1alpha1",
			Kind:       "Blueprint",
			Metadata:   blueprint.Metadata{Name: "xmeta"},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "platform.sparky.ee",
					Kind:    "XMeta",
					Plural:  "xmetas",
					Version: "v1alpha1",
					Scope:   "Namespaced",
				},
				Resources: []blueprint.Resource{
					{
						Name:     "my-svc",
						Kind:     "Service",
						Provider: blueprint.NativeProvider,
					},
					{
						Name:     "my-secret",
						Kind:     "Secret",
						Provider: blueprint.NativeProvider,
						Fields: map[string]blueprint.Field{
							"data[svc_name]": {From: "resources.my-svc.metadata.name"},
						},
					},
				},
			},
		}

		comp, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition failed: %v", err)
		}
		s := string(comp)

		if strings.Contains(s, "$xr-my-svc") {
			t.Errorf("emitted Go template contains illegal identifier $xr-my-svc:\n%s", s)
		}
		if !strings.Contains(s, `{{ printf "%s-my-svc" $xr | b64enc | quote }}`) {
			t.Errorf("expected '{{ printf \"%%s-my-svc\" $xr | b64enc | quote }}' in composition, got:\n%s", s)
		}

		// Verify template body parses and renders without bad character U+002D '-'
		tmplBody := extractTemplate(t, comp)
		rendered, err := renderTemplate(t, tmplBody, map[string]any{})
		if err != nil {
			t.Fatalf("renderTemplate failed: %v\n---\n%s", err, tmplBody)
		}
		// my-xqueue is the default XR name in renderTemplate
		expectedB64 := "bXkteHF1ZXVlLW15LXN2Yw==" // base64 of "my-xqueue-my-svc"
		if !strings.Contains(rendered, expectedB64) {
			t.Errorf("expected base64 encoded name %q in rendered output, got:\n%s", expectedB64, rendered)
		}
	})

	t.Run("static naming to Secret data", func(t *testing.T) {
		b := &blueprint.Blueprint{
			APIVersion: "factory.crossplane.io/v1alpha1",
			Kind:       "Blueprint",
			Metadata:   blueprint.Metadata{Name: "xmeta"},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "platform.sparky.ee",
					Kind:    "XMeta",
					Plural:  "xmetas",
					Version: "v1alpha1",
					Scope:   "Namespaced",
				},
				Resources: []blueprint.Resource{
					{
						Name:     "my-svc",
						Kind:     "Service",
						Provider: blueprint.NativeProvider,
						Fields: map[string]blueprint.Field{
							"metadata.name": {Value: "my-static-svc"},
						},
					},
					{
						Name:     "my-secret",
						Kind:     "Secret",
						Provider: blueprint.NativeProvider,
						Fields: map[string]blueprint.Field{
							"data[svc_name]": {From: "resources.my-svc.metadata.name"},
						},
					},
				},
			},
		}

		comp, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition failed: %v", err)
		}
		s := string(comp)

		if strings.Contains(s, "{{  | b64enc | quote }}") {
			t.Errorf("emitted Go template contains empty expression '{{  | b64enc | quote }}':\n%s", s)
		}

		// Verify template body parses and renders
		tmplBody := extractTemplate(t, comp)
		rendered, err := renderTemplate(t, tmplBody, map[string]any{})
		if err != nil {
			t.Fatalf("renderTemplate failed: %v\n---\n%s", err, tmplBody)
		}
		expectedB64 := "bXktc3RhdGljLXN2Yw==" // base64 of "my-static-svc"
		if !strings.Contains(rendered, expectedB64) {
			t.Errorf("expected base64 encoded static name %q in rendered output, got:\n%s", expectedB64, rendered)
		}
	})

	t.Run("default naming to CRD format byte field", func(t *testing.T) {
		byteCrds := byteContainerCRD(t)
		b := &blueprint.Blueprint{
			APIVersion: "factory.crossplane.io/v1alpha1",
			Kind:       "Blueprint",
			Metadata:   blueprint.Metadata{Name: "xmeta"},
			Spec: blueprint.Spec{
				Sources: []blueprint.Source{{Provider: "example.org"}},
				XRD: blueprint.XRD{
					Group:   "platform.sparky.ee",
					Kind:    "XMeta",
					Plural:  "xmetas",
					Version: "v1alpha1",
					Scope:   "Namespaced",
					Parameters: map[string]blueprint.Parameter{
						"providerName": {Type: "string", Required: true},
					},
				},
				Resources: []blueprint.Resource{
					{
						Name:     "my-svc",
						Kind:     "Service",
						Provider: blueprint.NativeProvider,
					},
					{
						Name:     "target",
						Kind:     "ByteContainer",
						Provider: "example.org",
						Fields: map[string]blueprint.Field{
							"payload": {From: "resources.my-svc.metadata.name"},
						},
					},
				},
			},
		}

		comp, err := Composition(b, byteCrds)
		if err != nil {
			t.Fatalf("Composition failed: %v", err)
		}
		s := string(comp)

		if strings.Contains(s, "$xr-my-svc") {
			t.Errorf("emitted Go template contains illegal identifier $xr-my-svc:\n%s", s)
		}
		if !strings.Contains(s, `{{ printf "%s-my-svc" $xr | b64enc | quote }}`) {
			t.Errorf("expected '{{ printf \"%%s-my-svc\" $xr | b64enc | quote }}' in composition, got:\n%s", s)
		}

		tmplBody := extractTemplate(t, comp)
		_, err = renderTemplate(t, tmplBody, map[string]any{"providerName": "default"})
		if err != nil {
			t.Fatalf("renderTemplate failed: %v\n---\n%s", err, tmplBody)
		}
	})
}
