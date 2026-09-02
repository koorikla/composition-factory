package testfixture

import (
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// QueueCRDYAML is the standard Crossplane AWS SQS Queue CRD fixture.
const QueueCRDYAML = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.m.upbound.io
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  tags: {type: object, additionalProperties: {type: string}}
                  maxMessageSize: {type: integer}
                  messageRetentionSeconds: {type: integer}
                  kmsMasterKeyId: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties:
                  kind: {type: string}
                  name: {type: string}
`

// QueueClusterCRDYAML is the Cluster-scoped Queue CRD fixture.
const QueueClusterCRDYAML = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  region: {type: string}
              deletionPolicy: {type: string}
`

// QueueCRDs returns parsed CRD slice from QueueCRDYAML for testing.
func QueueCRDs(t testing.TB) []schema.CRD {
	t.Helper()
	crds, err := schema.ParseCRDs([][]byte{[]byte(QueueCRDYAML)})
	if err != nil {
		t.Fatalf("testfixture: parse QueueCRDYAML: %v", err)
	}
	return crds
}

// QueueBothCRDs returns parsed CRD slice containing both Namespaced and Cluster Queue CRDs.
func QueueBothCRDs(t testing.TB) []schema.CRD {
	t.Helper()
	crds, err := schema.ParseCRDs([][]byte{[]byte(QueueCRDYAML), []byte(QueueClusterCRDYAML)})
	if err != nil {
		t.Fatalf("testfixture: parse QueueBothCRDs: %v", err)
	}
	return crds
}
