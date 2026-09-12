package adopt

import (
	"testing"
)

func TestForwardStatusRefUniqueName(t *testing.T) {
	compYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-worker-queue
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: worker-queue
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-consumer
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: consumer
          spec:
            forProvider:
              redrivePolicy: '{{ (index $.observed.resources "worker_queue").resource.status.atProvider.arn }}'
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-worker-queue-dup
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: worker_queue
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	consumer := bp.ResourceNamed("consumer")
	if consumer == nil {
		t.Fatalf("expected resource consumer to exist")
	}

	wire := consumer.Fields["redrivePolicy"].From
	expected := "resources.worker-queue-2.status.atProvider.arn"
	if wire != expected {
		t.Errorf("got wire %q, expected %q", wire, expected)
	}
}

func TestForwardStatusRef_PreservesEarlierResourceReference(t *testing.T) {
	compYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-disjoint
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-worker-queue
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: worker-queue
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-consumer-primary
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: consumer-primary
          spec:
            forProvider:
              redrivePolicy: '{{ (index $.observed.resources "worker-queue").resource.status.atProvider.arn }}'
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-consumer-secondary
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: consumer-secondary
          spec:
            forProvider:
              redrivePolicy: '{{ (index $.observed.resources "worker_queue").resource.status.atProvider.arn }}'
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            name: {{ $xr }}-worker-queue-dup
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: worker_queue
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	primary := bp.ResourceNamed("consumer-primary")
	if primary == nil {
		t.Fatalf("expected resource consumer-primary to exist")
	}
	expectedPrimary := "resources.worker-queue.status.atProvider.arn"
	if wire := primary.Fields["redrivePolicy"].From; wire != expectedPrimary {
		t.Errorf("primary got wire %q, expected %q", wire, expectedPrimary)
	}

	secondary := bp.ResourceNamed("consumer-secondary")
	if secondary == nil {
		t.Fatalf("expected resource consumer-secondary to exist")
	}
	expectedSecondary := "resources.worker-queue-2.status.atProvider.arn"
	if wire := secondary.Fields["redrivePolicy"].From; wire != expectedSecondary {
		t.Errorf("secondary got wire %q, expected %q", wire, expectedSecondary)
	}
}

func TestClassicComposition_UniqueName_SynchronizedInNameMapping(t *testing.T) {
	compYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-classic-duplicate
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
  - name: my_queue
    base:
      apiVersion: sqs.aws.upbound.io/v1beta1
      kind: Queue
      metadata:
        name: my-queue
  - name: my_queue
    base:
      apiVersion: sqs.aws.upbound.io/v1beta1
      kind: Queue
      metadata:
        name: my-queue-dup
  - name: consumer
    base:
      apiVersion: sqs.aws.upbound.io/v1beta1
      kind: Queue
      metadata:
        annotations:
          example.org/arn: '{{ (index $.observed.resources "my_queue").resource.status.atProvider.arn }}'
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	if len(bp.Spec.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(bp.Spec.Resources))
	}

	if bp.Spec.Resources[0].Name != "my-queue" {
		t.Errorf("expected resource 0 name my-queue, got %q", bp.Spec.Resources[0].Name)
	}
	if bp.Spec.Resources[1].Name != "my-queue-2" {
		t.Errorf("expected resource 1 name my-queue-2, got %q", bp.Spec.Resources[1].Name)
	}

	consumer := bp.ResourceNamed("consumer")
	if consumer == nil {
		t.Fatalf("expected consumer resource to exist")
	}
	wire := consumer.Annotations["example.org/arn"].From
	expected := "resources.my-queue-2.status.atProvider.arn"
	if wire != expected {
		t.Errorf("got wire %q, expected %q", wire, expected)
	}
}
