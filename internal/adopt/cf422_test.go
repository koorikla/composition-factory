package adopt

import (
	"testing"
)

func TestCF422_AdoptStatusBacktickQuote(t *testing.T) {
	t.Run("wire_backtick", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-backticks
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: worker-queue
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: QueueConsumer
          metadata:
            annotations:
              crossplane.io/composition-resource-name: consumer
          spec:
            forProvider:
              arn: "{{ (index .observed.resources ` + "`" + `worker-queue` + "`" + `).resource.status.atProvider.arn }}"
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		consumer := bp.ResourceNamed("consumer")
		if consumer == nil {
			t.Fatalf("expected consumer resource")
		}
		wire := consumer.Fields["arn"].From
		expected := "resources.worker-queue.status.atProvider.arn"
		if wire != expected {
			t.Errorf("got wire %q, expected %q", wire, expected)
		}
	})

	t.Run("foreach_backtick", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-backticks-loop
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: worker-queue
          ---
          {{- range $i := until (int (index .observed.resources ` + "`" + `worker-queue` + "`" + `).resource.status.count) }}
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              vpcId: vpc-123
          {{- end }}
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		subnet := bp.ResourceNamed("subnet")
		if subnet == nil {
			t.Fatalf("expected subnet resource")
		}
		expected := "resources.worker-queue.status.count"
		if subnet.ForEach != expected {
			t.Errorf("got ForEach %q, expected %q", subnet.ForEach, expected)
		}
	})
}
