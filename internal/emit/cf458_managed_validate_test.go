package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// cf458CRDs is testCRDsWithNative plus a managed kind whose spec declares
// forProvider required, so the required-property path can be exercised.
func cf458CRDs(t *testing.T) []schema.CRD {
	t.Helper()
	strict := schema.CRD{
		Group:      "sqs.aws.m.upbound.io",
		Kind:       "StrictQueue",
		Plural:     "strictqueues",
		Scope:      "Namespaced",
		Categories: []string{"crossplane", "managed"},
		Versions: []schema.Version{
			{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Properties: map[string]any{
					"spec": map[string]any{
						"type":     "object",
						"required": []any{"forProvider"},
						"properties": map[string]any{
							"deletionPolicy": map[string]any{"type": "string", "enum": []any{"Orphan", "Delete"}},
							"forProvider": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"region": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}
	return append(testCRDsWithNative(t), strict)
}

// A managed resource must be validated as thoroughly as a native one: its
// metadata is checked, and a key that is not part of the resource's shape is
// reported rather than ignored.
func TestCF458ValidateRenderedChecksManagedRootAndMetadata(t *testing.T) {
	crds := cf458CRDs(t)

	t.Run("metadata is validated on managed resources", func(t *testing.T) {
		stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  labels:
    invalid: [1, 2, 3]
spec:
  forProvider:
    region: eu-north-1
`
		err := ValidateRendered([]byte(stream), crds)
		if err == nil {
			t.Fatal("metadata.labels holds a sequence, which is not a map of strings; want an error, got nil")
		}
		if !strings.Contains(err.Error(), "labels") {
			t.Errorf("error does not mention metadata.labels: %v", err)
		}
	})

	t.Run("an unknown root field is reported", func(t *testing.T) {
		stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
unknownRootField: true
spec:
  forProvider:
    region: eu-north-1
`
		err := ValidateRendered([]byte(stream), crds)
		if err == nil {
			t.Fatal("unknownRootField is not part of the resource; want an error, got nil")
		}
		if !strings.Contains(err.Error(), "unknownRootField") {
			t.Errorf("error does not name the offending field: %v", err)
		}
	})

	t.Run("a misspelled spec is reported", func(t *testing.T) {
		stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
sec:
  forProvider:
    region: eu-north-1
`
		err := ValidateRendered([]byte(stream), crds)
		if err == nil {
			t.Fatal("'sec' is a typo for 'spec'; want an error, got nil")
		}
		if !strings.Contains(err.Error(), "sec") {
			t.Errorf("error does not name the offending field: %v", err)
		}
	})

	t.Run("a required spec property that is missing is reported", func(t *testing.T) {
		stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: StrictQueue
metadata:
  annotations:
    crossplane.io/composition-resource-name: strict-queue
spec:
  deletionPolicy: Delete
`
		err := ValidateRendered([]byte(stream), crds)
		if err == nil {
			t.Fatal("spec.forProvider is required by the CRD but absent; want an error, got nil")
		}
		if !strings.Contains(err.Error(), "forProvider") {
			t.Errorf("error does not name the missing property: %v", err)
		}
	})

	t.Run("a well-formed managed resource still validates clean", func(t *testing.T) {
		stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  generateName: render-check-
  labels:
    app: demo
spec:
  deletionPolicy: Delete
  providerConfigRef:
    kind: ClusterProviderConfig
    name: aws-provider
  forProvider:
    region: eu-north-1
    visibilityTimeoutSeconds: 45
    fifoQueue: true
    tags:
      env: dev
`
		if err := ValidateRendered([]byte(stream), crds); err != nil {
			t.Fatalf("valid managed resource rejected: %v", err)
		}
	})
}
