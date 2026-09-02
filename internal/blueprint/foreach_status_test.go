// Tests for the observed-count forEach form: `forEach:
// resources.<name>.status.<path>`, the loop bound read at render time from
// another composed resource's observed status. Validate owns the grammar and
// document-level rules pinned here (target declared, not self, not itself
// looped, clean path segments); the CRD-schema half — the path resolves to an
// integer/number status leaf — belongs to internal/emit, which holds the CRDs.
package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// statusForEachBlueprint is a valid blueprint whose node-group resource fans
// out over the cluster resource's observed status integer. Mutations build
// every rejection case from this known-good baseline, so each test pins
// exactly one rule.
func statusForEachBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		Metadata: Metadata{Name: "xcluster"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XCluster", Plural: "xclusters",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []Resource{
				{
					Name: "cluster", Kind: "Cluster",
					Fields: map[string]Field{"region": {Value: "eu-north-1"}},
				},
				{
					Name: "node-group", Kind: "NodeGroup",
					ForEach: "resources.cluster.status.atProvider.nodeCount",
					Fields:  map[string]Field{"region": {Value: "eu-north-1"}},
				},
			},
		},
	}
	mutate(b)
	return b
}

func TestValidateAcceptsStatusForEach(t *testing.T) {
	if err := statusForEachBlueprint(func(*Blueprint) {}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a forEach over another resource's observed status accepted", err)
	}
}

// The status-bound forEach key must survive a marshal/unmarshal round trip
// exactly, for the same reason the params form must: the HTTP API persists
// the whole document by re-marshaling the Go struct, so a key the struct
// dropped or rewrote would be silently erased on the first edit.
func TestStatusForEachRoundTripsExactly(t *testing.T) {
	const doc = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xcluster
spec:
  xrd:
    group: platform.sparky.ee
    kind: XCluster
    plural: xclusters
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  resources:
    - name: cluster
      kind: Cluster
      fields:
        region: {value: eu-north-1}
    - name: node-group
      kind: NodeGroup
      forEach: resources.cluster.status.atProvider.nodeCount
      fields:
        region: {value: eu-north-1}
`
	b, err := Load(write(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Spec.Resources[1].ForEach; got != "resources.cluster.status.atProvider.nodeCount" {
		t.Fatalf("ForEach = %q, want the status reference loaded verbatim", got)
	}
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-loaded +reloaded):\n%s", diff)
	}
}

func TestValidateRejectsStatusForEachToUnknownResource(t *testing.T) {
	b := statusForEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].ForEach = "resources.no-such-cluster.status.atProvider.nodeCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "no-such-cluster") {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
	if !strings.Contains(err.Error(), "node-group") {
		t.Errorf("err = %v, want it to name the offending resource", err)
	}
}

// A resource cannot fan out over its own observed status: its own document
// count would depend on a value only its own instances can report — a
// bootstrap knot with no first observation to untie it.
func TestValidateRejectsStatusForEachToSelf(t *testing.T) {
	b := statusForEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].ForEach = "resources.node-group.status.atProvider.nodeCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "own") {
		t.Fatalf("err = %v, want a refusal of a self-referential loop bound", err)
	}
}

// A looped target's observed keys are indexed (<name>-0, <name>-1, ...), so
// the un-indexed key the reference names never appears in the observed map —
// the same rule status wires enforce, for the same reason.
func TestValidateRejectsStatusForEachToLoopedResource(t *testing.T) {
	for _, targetLoop := range []string{
		"params.instanceCount",
		"resources.cluster.status.atProvider.nodeCount",
	} {
		t.Run(targetLoop, func(t *testing.T) {
			b := statusForEachBlueprint(func(b *Blueprint) {
				b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer", Default: "2"}
				b.Spec.Resources = append(b.Spec.Resources, Resource{
					Name: "replica", Kind: "Queue",
					ForEach: targetLoop,
					Fields:  map[string]Field{"region": {Value: "eu-north-1"}},
				})
				b.Spec.Resources[1].ForEach = "resources.replica.status.atProvider.nodeCount"
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "looped") {
				t.Fatalf("err = %v, want a refusal naming the looped target", err)
			}
			if !strings.Contains(err.Error(), "replica-0") {
				t.Errorf("err = %v, want it to explain the indexed observed keys", err)
			}
		})
	}
}

// The status wire rule and the forEach rule compose: a resource looped by an
// observed count is itself an invalid status-wire TARGET, exactly like a
// params-looped one.
func TestValidateRejectsStatusWireIntoStatusLoopedResource(t *testing.T) {
	b := statusForEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Fields["endpoint"] = Field{
			From: "resources.node-group.status.atProvider.endpoint",
		}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "looped") {
		t.Fatalf("err = %v, want the status wire into a looped resource refused", err)
	}
}

func TestValidateRejectsStatusForEachBrokenGrammar(t *testing.T) {
	cases := []struct{ name, forEach string }{
		{"no .status. separator", "resources.cluster.nodeCount"},
		{"empty path", "resources.cluster.status."},
		{"empty name", "resources..status.atProvider.nodeCount"},
		{"dashed segment", "resources.cluster.status.atProvider.node-count"},
		{"underscore-led segment", "resources.cluster.status._private.nodeCount"},
		{"neither grammar", "observed.cluster.nodeCount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := statusForEachBlueprint(func(b *Blueprint) {
				b.Spec.Resources[1].ForEach = tc.forEach
			})
			if err := b.Validate(); err == nil {
				t.Fatalf("Validate() accepted forEach %q", tc.forEach)
			}
		})
	}
}

// The params form keeps its own rules bit-for-bit: a status-shaped forEach
// must not loosen the required-or-defaulted integer rule next door.
func TestValidateStatusForEachLeavesParamsFormRulesIntact(t *testing.T) {
	b := statusForEachBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer"} // optional, no default
		b.Spec.Resources[1].ForEach = "params.instanceCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v, want the params-form required-or-default rule still enforced", err)
	}
}

// --- referencer discipline ---

// wiredForEachBlueprint is wiredBlueprint with the queue-policy resource
// fanned out by main-queue's observed status instead of (in addition to) a
// field wire, so the rename/delete referencer scans have a forEach status
// reference to track.
func wiredForEachBlueprint() *Blueprint {
	return wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].ForEach = "resources.main-queue.status.atProvider.messageCount"
		b.Spec.Resources[1].Fields = map[string]Field{"region": {Value: "eu-north-1"}}
	})
}

func TestRenameResourceRewritesForEachStatusRefs(t *testing.T) {
	b := wiredForEachBlueprint()
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	got := b.Spec.Resources[1].ForEach
	if got != "resources.primary-queue.status.atProvider.messageCount" {
		t.Errorf("ForEach = %q, want the loop bound rewritten to the new resource name", got)
	}
}

// The rewrite is prefix-exact, matching the field-wire discipline: renaming
// "main" must not touch a loop bound over "main-queue".
func TestRenameResourceDoesNotRewriteForEachOfPrefixSharingName(t *testing.T) {
	b := wiredForEachBlueprint()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "main", Kind: "Queue", Fields: map[string]Field{},
	})
	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if got := b.Spec.Resources[1].ForEach; got != "resources.main-queue.status.atProvider.messageCount" {
		t.Errorf("ForEach = %q, want the main-queue loop bound left untouched", got)
	}
}

func TestDeleteResourceRefusesWhileForEachReferencesItsStatus(t *testing.T) {
	b := wiredForEachBlueprint()
	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want a refusal while queue-policy's forEach reads its status")
	}
	if !strings.Contains(err.Error(), "queue-policy") {
		t.Errorf("err = %v, want it to name the referencing resource", err)
	}
	if len(b.Spec.Resources) != 2 {
		t.Errorf("resources = %d after a refused delete, want 2", len(b.Spec.Resources))
	}
}

// A params-form forEach references a PARAMETER, never a resource: deleting an
// unrelated resource next to it must stay legal, and renaming a parameter
// must keep rewriting the params form while leaving the status form alone.
func TestRenameParameterLeavesStatusForEachAlone(t *testing.T) {
	b := wiredForEachBlueprint()
	b.Spec.XRD.Parameters["mainQueue"] = Parameter{Type: "string", Required: true}
	if err := b.RenameParameter("mainQueue", "primaryQueue"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[1].ForEach; got != "resources.main-queue.status.atProvider.messageCount" {
		t.Errorf("ForEach = %q, want a parameter rename to leave a status loop bound untouched", got)
	}
}
