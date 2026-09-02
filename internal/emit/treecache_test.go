package emit

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// manyWireCRDs is a Queue whose status carries a realistically wide
// atProvider block, plus a Consumer with twenty settable strings — enough
// status wires for the per-field schema work in the emitter to dominate.
func manyWireCRDs(tb testing.TB) []schema.CRD {
	tb.Helper()
	var atProvider, consumerFields strings.Builder
	for i := range 40 {
		fmt.Fprintf(&atProvider, "                  attr%02d: {type: string, description: Observed attribute %d.}\n", i, i)
	}
	for i := range 20 {
		fmt.Fprintf(&consumerFields, "                  f%02d: {type: string}\n", i)
	}
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
                  maxMessageSize: {type: integer}
          status:
            properties:
              atProvider:
                properties:
                  url: {type: string}
` + atProvider.String() + `
              conditions:
                type: array
                items:
                  properties:
                    type: {type: string}
                    status: {type: string}
                    reason: {type: string}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: consumers.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Consumer, plural: consumers, categories: [managed]}
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
` + consumerFields.String() + `
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		tb.Fatal(err)
	}
	return crds
}

// manyWireBlueprint wires every one of the Consumer's twenty fields from
// the Queue's observed status.
func manyWireBlueprint(wires int) *blueprint.Blueprint {
	b := testBlueprint()
	fields := map[string]blueprint.Field{"region": {Value: "eu-north-1"}}
	for i := range wires {
		fields[fmt.Sprintf("f%02d", i)] = blueprint.Field{From: fmt.Sprintf("resources.main-queue.status.atProvider.attr%02d", i)}
	}
	b.Spec.Resources = []blueprint.Resource{
		{Name: "main-queue", Kind: "Queue", Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}}},
		{Name: "consumer", Kind: "Consumer", Fields: fields},
	}
	return b
}

// BenchmarkGenerateStatusWires is the emitter's hot path for schema work:
// Generate on a resource with twenty status wires, each of which resolves
// the source kind's status tree (checkStatusRefs and statusWire both do).
func BenchmarkGenerateStatusWires(b *testing.B) {
	crds := manyWireCRDs(b)
	bp := manyWireBlueprint(20)
	if _, err := Generate(bp, crds, "out"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Generate(bp, crds, "out"); err != nil {
			b.Fatal(err)
		}
	}
}

// Memoised trees are shared by every caller for the life of the CRD, so no
// caller may write to one. Proof by consequence: generating twice with two
// different blueprints over the same CRDs, then again with the first, must
// give exactly the bytes a fresh parse gives — and the status tree itself
// must deep-compare equal to a fresh parse's after all that traffic.
func TestCachedTreesAreNotMutatedByGenerate(t *testing.T) {
	crds := manyWireCRDs(t)
	first := manyWireBlueprint(20)
	second := manyWireBlueprint(3)
	second.Spec.Resources[1].Fields["f19"] = blueprint.Field{Value: "literal"}

	gen := func(bp *blueprint.Blueprint, c []schema.CRD) []byte {
		t.Helper()
		outs, err := Generate(bp, c, "out")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		for _, o := range outs {
			buf.WriteString(o.Path)
			buf.Write(o.Body)
		}
		return buf.Bytes()
	}

	before := gen(first, crds)
	gen(second, crds)
	gen(second, crds)
	after := gen(first, crds)
	fresh := gen(first, manyWireCRDs(t))

	if !bytes.Equal(before, after) {
		t.Error("the same blueprint over the same CRDs produced different bytes after other generates ran")
	}
	if !bytes.Equal(after, fresh) {
		t.Error("output over long-lived CRDs differs from a fresh parse's output")
	}

	// The Queue is crds[0] in both parses: its status tree after all the
	// traffic above must still equal what a fresh parse builds.
	freshCRDs := manyWireCRDs(t)
	used, _ := crds[0].Status()
	pristine, _ := freshCRDs[0].Status()
	if diff := cmp.Diff(pristine, used); diff != "" {
		t.Errorf("Queue status tree was mutated by the emitter (-fresh +used):\n%s", diff)
	}
	usedFP, _ := crds[1].FieldTree()
	pristineFP, _ := freshCRDs[1].FieldTree()
	if diff := cmp.Diff(pristineFP, usedFP); diff != "" {
		t.Errorf("Consumer field tree was mutated by the emitter (-fresh +used):\n%s", diff)
	}
}
