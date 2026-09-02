package emit_test

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

const bucketCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: buckets.storage.gcp.m.upbound.io
spec:
  group: storage.gcp.m.upbound.io
  names: {kind: Bucket, plural: buckets, categories: [managed]}
  scope: Namespaced
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
                  properties:
                    location: {type: string}
                    lifecycleRule:
                      type: array
                      items:
                        type: object
                        properties:
                          action:
                            type: object
                            properties:
                              type: {type: string}
                              storageClass: {type: string}
                          condition:
                            type: object
                            properties:
                              age: {type: integer}
                              createdBefore: {type: string}
                providerConfigRef:
                  type: object
                  required: [kind, name]
                  properties: {kind: {type: string}, name: {type: string}}
`

func providerBucketFixture(t *testing.T, fields map[string]blueprint.Field) (*blueprint.Blueprint, []schema.CRD) {
	t.Helper()
	crds, err := schema.ParseCRDs([][]byte{[]byte(bucketCRD)})
	if err != nil {
		t.Fatal(err)
	}
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xbucket"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{Provider: "xpkg.upbound.io/upbound/provider-gcp-storage:v1.0.0"}},
			XRD: blueprint.XRD{
				Group: "platform.example.org", Kind: "XBucket", Plural: "xbuckets",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"actionType":   {Type: "string", Required: false},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "bucket", Kind: "Bucket",
				Provider: "xpkg.upbound.io/upbound/provider-gcp-storage:v1.0.0",
				Fields:   fields,
			}},
		},
	}
	return b, crds
}

func TestProviderArrayElementLifecycleRule(t *testing.T) {
	b, crds := providerBucketFixture(t, map[string]blueprint.Field{
		"location":                     {Value: "US"},
		"lifecycleRule[0].action.type": {Value: "Delete"},
	})
	got, err := emit.Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}
	s := string(got)
	t.Logf("Emitted Composition:\n%s", s)

	// Must NOT contain flat literal key
	if strings.Contains(s, "'lifecycleRule[0].action.type'") || strings.Contains(s, "\"lifecycleRule[0].action.type\"") {
		t.Fatalf("emitted flat literal key instead of nested YAML:\n%s", s)
	}

	// Must contain nested lifecycleRule sequence
	if !strings.Contains(s, "lifecycleRule:") {
		t.Fatalf("missing lifecycleRule key:\n%s", s)
	}
	if !strings.Contains(s, "-") || !strings.Contains(s, "action:") || !strings.Contains(s, "type: 'Delete'") {
		t.Fatalf("missing list item structure for lifecycleRule:\n%s", s)
	}

	// Extract and verify forProvider YAML block
	fpStart := strings.Index(s, "forProvider:")
	if fpStart < 0 {
		t.Fatalf("missing forProvider in composition:\n%s", s)
	}
	fpEnd := strings.Index(s[fpStart:], "providerConfigRef:")
	if fpEnd < 0 {
		fpEnd = len(s[fpStart:])
	}
	fpBlock := s[fpStart : fpStart+fpEnd]

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(fpBlock), &parsed); err != nil {
		t.Fatalf("forProvider block does not parse as valid YAML: %v\n---\n%s", err, fpBlock)
	}
	fp := parsed["forProvider"].(map[string]any)
	lr, ok := fp["lifecycleRule"].([]any)
	if !ok || len(lr) != 1 {
		t.Fatalf("expected lifecycleRule to be 1-element slice, got: %T %v", fp["lifecycleRule"], fp["lifecycleRule"])
	}
	firstElem, ok := lr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected slice element to be map, got: %T %v", lr[0], lr[0])
	}
	action, ok := firstElem["action"].(map[string]any)
	if !ok || action["type"] != "Delete" {
		t.Fatalf("expected action.type == Delete, got: %v", firstElem)
	}
	if fp["location"] != "US" {
		t.Fatalf("expected location == US, got: %v", fp["location"])
	}
}

func TestProviderMultiElementArray(t *testing.T) {
	b, crds := providerBucketFixture(t, map[string]blueprint.Field{
		"location":                             {Value: "US"},
		"lifecycleRule[0].action.type":         {Value: "Delete"},
		"lifecycleRule[0].condition.age":       {Value: "30"},
		"lifecycleRule[1].action.type":         {Value: "SetStorageClass"},
		"lifecycleRule[1].action.storageClass": {Value: "NEARLINE"},
	})
	got, err := emit.Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}
	s := string(got)
	t.Logf("Emitted Composition:\n%s", s)

	i0 := strings.Index(s, "Delete")
	i1 := strings.Index(s, "SetStorageClass")
	if i0 < 0 || i1 < 0 || i0 > i1 {
		t.Fatalf("elements missing or out of order (Delete@%d, SetStorageClass@%d):\n%s", i0, i1, s)
	}
	if strings.Count(s, "action:") < 2 {
		t.Fatalf("expected at least 2 action blocks:\n%s", s)
	}
}

func TestProviderArrayIndexGapRefused(t *testing.T) {
	b, crds := providerBucketFixture(t, map[string]blueprint.Field{
		"lifecycleRule[0].action.type": {Value: "Delete"},
		"lifecycleRule[2].action.type": {Value: "Delete"},
	})
	_, err := emit.Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "lifecycleRule") {
		t.Fatalf("index gap not refused: %v", err)
	}
}

func TestProviderArrayElementTypoCaught(t *testing.T) {
	b, crds := providerBucketFixture(t, map[string]blueprint.Field{
		"lifecycleRule[0].action.typo": {Value: "Delete"},
	})
	_, err := emit.Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "action.type") {
		t.Fatalf("element typo not refused with suggestion: %v", err)
	}
}
