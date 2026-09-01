package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func editable() *Blueprint {
	return &Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			Sources: []Source{{Provider: "ghcr.io/x/provider-aws-sqs:v2.7.0"}},
			XRD: XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName":   {Type: "string", Required: true},
					"maxMessageSize": {Type: "integer"},
				},
			},
			Resources: []Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]Field{"maxMessageSize": {From: "params.maxMessageSize"}},
			}},
		},
	}
}

// gated is editable plus a defaulted string parameter and a resource whose
// when condition references it.
func gated() *Blueprint {
	b := editable()
	b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string", Default: "standard", Enum: []string{"standard", "pro"}}
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "audit-queue", Kind: "Queue",
		When:   `params.tier == "pro"`,
		Fields: map[string]Field{"region": {Value: "eu-north-1"}},
	})
	return b
}

// A when condition is a params.<name> reference exactly like a field's From
// and a forEach loop bound, and gets the same rewrite discipline: a dangling
// when emits a Composition whose condition dereferences a parameter that no
// longer exists, which under missingkey=error can never render.
func TestRenameParameterRewritesWhenReferences(t *testing.T) {
	cases := []struct{ name, when, want string }{
		{"comparison form", `params.tier == "pro"`, `params.level == "pro"`},
		{"inequality form", `params.tier != "standard"`, `params.level != "standard"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := gated()
			b.Spec.Resources[1].When = tc.when
			if err := b.RenameParameter("tier", "level"); err != nil {
				t.Fatalf("RenameParameter: %v", err)
			}
			if got := b.Spec.Resources[1].When; got != tc.want {
				t.Errorf("When = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenameParameterRewritesBareWhenReference(t *testing.T) {
	b := gated()
	b.Spec.XRD.Parameters["auditEnabled"] = Parameter{Type: "boolean", Default: "false"}
	b.Spec.Resources[1].When = "params.auditEnabled"
	if err := b.RenameParameter("auditEnabled", "auditOn"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[1].When; got != "params.auditOn" {
		t.Errorf("When = %q, want params.auditOn", got)
	}
}

func TestDeleteParameterRefusesWhenWhenReferences(t *testing.T) {
	b := gated()
	err := b.DeleteParameter("tier")
	if err == nil {
		t.Fatal("want an error deleting a parameter a when condition still references")
	}
	if !strings.Contains(err.Error(), "audit-queue") {
		t.Errorf("err = %v, want it to name the gated resource", err)
	}
	if _, ok := b.Spec.XRD.Parameters["tier"]; !ok {
		t.Error("parameter was deleted despite the error")
	}
}

// wired is editable plus a second resource whose queueUrl is a
// cross-resource status reference to main-queue.
func wired() *Blueprint {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "queue-policy", Kind: "QueuePolicy",
		Fields: map[string]Field{
			"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
		},
	})
	return b
}

func TestRenameResourceRewritesStatusReferences(t *testing.T) {
	b := wired()
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if got := b.Spec.Resources[0].Name; got != "primary-queue" {
		t.Errorf("resource name = %q, want primary-queue", got)
	}
	if got := b.Spec.Resources[1].Fields["queueUrl"].From; got != "resources.primary-queue.status.atProvider.url" {
		t.Errorf("reference = %q, want it rewritten to the new name -- a dangling reference "+
			"emits a guard chain over an observed key that can never exist again", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after rename: %v", err)
	}
}

func TestRenameResourceRejectsCollisionAndChangesNothing(t *testing.T) {
	b := wired()
	want := wired()
	if err := b.RenameResource("main-queue", "queue-policy"); err == nil {
		t.Fatal("want a collision error renaming onto an existing resource name")
	}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("failed rename mutated the receiver:\n%s", diff)
	}
}

func TestRenameUnknownResourceErrors(t *testing.T) {
	b := wired()
	if err := b.RenameResource("nope", "whatever"); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}

func TestRenameResourceToSameNameIsANoOp(t *testing.T) {
	b := wired()
	want := wired()
	if err := b.RenameResource("main-queue", "main-queue"); err != nil {
		t.Fatalf("RenameResource to same name: %v, want a no-op success", err)
	}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("no-op rename mutated the receiver:\n%s", diff)
	}
}

func TestRenameResourceRejectsInvalidNameAndChangesNothing(t *testing.T) {
	b := wired()
	want := wired()
	if err := b.RenameResource("main-queue", "Not_A_DNS_Label"); err == nil {
		t.Fatal("want a validation error renaming to a non-DNS-label name")
	}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("failed rename mutated the receiver:\n%s", diff)
	}
}

func TestDeleteResourceRefusesWhenStatusReferenced(t *testing.T) {
	b := wired()
	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("want an error deleting a resource whose status is still referenced")
	}
	if !strings.Contains(err.Error(), "queue-policy") {
		t.Errorf("err = %v, want it to name the referencing resource", err)
	}
	if len(b.Spec.Resources) != 2 {
		t.Error("resource was deleted despite the error")
	}
}

func TestDeleteResourceSucceedsWhenUnreferenced(t *testing.T) {
	b := wired()
	if err := b.DeleteResource("queue-policy"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resources = %+v, want only main-queue left", b.Spec.Resources)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after delete: %v", err)
	}
}

func TestDeleteUnknownResourceErrors(t *testing.T) {
	b := wired()
	if err := b.DeleteResource("nope"); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}

func TestAddParameter(t *testing.T) {
	b := editable()
	if err := b.AddParameter("location", Parameter{Type: "string", Required: true, Enum: []string{"EU", "US"}}); err != nil {
		t.Fatalf("AddParameter: %v", err)
	}
	if got := b.Spec.XRD.Parameters["location"]; got.Type != "string" || !got.Required || len(got.Enum) != 2 {
		t.Errorf("added parameter = %+v", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after a valid add: %v", err)
	}
}

func TestAddParameterRejectsDuplicate(t *testing.T) {
	b := editable()
	err := b.AddParameter("providerName", Parameter{Type: "string"})
	if err == nil || !strings.Contains(err.Error(), "providerName") {
		t.Fatalf("err = %v, want a duplicate error naming providerName", err)
	}
}

// An invalid add must leave the blueprint untouched, not partially applied.
func TestAddParameterRejectsInvalidAndChangesNothing(t *testing.T) {
	b := editable()
	before := len(b.Spec.XRD.Parameters)
	if err := b.AddParameter("not a valid name", Parameter{Type: "string"}); err == nil {
		t.Fatal("want an error for an invalid parameter name")
	}
	if err := b.AddParameter("zones", Parameter{Type: "array"}); err == nil {
		t.Fatal("want an error: array parameters are unsupported")
	}
	if len(b.Spec.XRD.Parameters) != before {
		t.Errorf("parameter count changed to %d after failed adds; edits must be atomic", len(b.Spec.XRD.Parameters))
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint left invalid after failed adds: %v", err)
	}
}

// The rename must rewrite every reference, or generation breaks.
func TestRenameParameterRewritesReferences(t *testing.T) {
	b := editable()
	if err := b.RenameParameter("maxMessageSize", "maxBytes"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if _, still := b.Spec.XRD.Parameters["maxMessageSize"]; still {
		t.Error("old parameter name still present")
	}
	if _, ok := b.Spec.XRD.Parameters["maxBytes"]; !ok {
		t.Fatal("new parameter name absent")
	}
	got := b.Spec.Resources[0].Fields["maxMessageSize"].From
	if got != "params.maxBytes" {
		t.Errorf("reference = %q, want params.maxBytes — a dangling reference emits a Composition that cannot render", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after rename: %v", err)
	}
}

func TestRenameParameterRejectsCollisionAndChangesNothing(t *testing.T) {
	b := editable()
	want := b.Spec.Resources[0].Fields["maxMessageSize"].From
	if err := b.RenameParameter("maxMessageSize", "providerName"); err == nil {
		t.Fatal("want an error renaming onto an existing parameter")
	}
	if _, ok := b.Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("original parameter was removed by a failed rename")
	}
	if got := b.Spec.Resources[0].Fields["maxMessageSize"].From; got != want {
		t.Errorf("reference mutated by a failed rename: %q", got)
	}
}

func TestRenameUnknownParameterErrors(t *testing.T) {
	b := editable()
	if err := b.RenameParameter("nope", "other"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}

// A blur-submit UI routinely resubmits an unchanged name; renaming a
// parameter onto its own current name must succeed as a no-op rather than
// erroring as a collision, or every caller would have to special-case
// "did the name actually change" before calling this.
func TestRenameParameterToSameNameIsANoOp(t *testing.T) {
	b := editable()
	want := editable()
	if err := b.RenameParameter("maxMessageSize", "maxMessageSize"); err != nil {
		t.Fatalf("RenameParameter(x, x): %v", err)
	}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("blueprint changed by a no-op self-rename (-want +got):\n%s", diff)
	}
}

// The from == to short-circuit must not bypass the from-must-exist check:
// there is nothing to rename when from is unknown, even if to == from.
func TestRenameUnknownParameterToSameNameStillErrors(t *testing.T) {
	b := editable()
	if err := b.RenameParameter("nope", "nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter even when from == to", err)
	}
}

func TestSetParameterReplacesInPlace(t *testing.T) {
	b := editable()
	if err := b.SetParameter("maxMessageSize", Parameter{Type: "integer", Default: "2048", Description: "Max size."}); err != nil {
		t.Fatalf("SetParameter: %v", err)
	}
	got := b.Spec.XRD.Parameters["maxMessageSize"]
	if got.Default != "2048" || got.Description != "Max size." {
		t.Errorf("parameter = %+v", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after set: %v", err)
	}
}

func TestSetParameterRejectsInvalidAndChangesNothing(t *testing.T) {
	b := editable()
	before := b.Spec.XRD.Parameters["maxMessageSize"]
	if err := b.SetParameter("maxMessageSize", Parameter{Type: "integer", Default: "not-a-number"}); err == nil {
		t.Fatal("want an error: an integer default must parse")
	}
	if diff := cmp.Diff(before, b.Spec.XRD.Parameters["maxMessageSize"]); diff != "" {
		t.Errorf("parameter mutated by a failed set (-before +after):\n%s", diff)
	}
}

// SetParameter is replace-only, not upsert: Task 6 maps it to PUT with
// 404-for-unknown, since POST already owns creation (AddParameter). The
// unknown-name path is what that 404 depends on.
func TestSetParameterRejectsUnknownParameter(t *testing.T) {
	b := editable()
	want := editable()
	err := b.SetParameter("neverDeclared", Parameter{Type: "string"})
	if err == nil || !strings.Contains(err.Error(), "neverDeclared") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("blueprint changed by a failed set on an unknown name (-want +got):\n%s", diff)
	}
}

// Deleting a parameter something references must be refused, not cascade.
// Two resources reference maxMessageSize here so the error is verified to
// name every referencing resource, not just the first one found.
func TestDeleteParameterRefusesWhenReferenced(t *testing.T) {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "second-queue", Kind: "Queue",
		Fields: map[string]Field{"maxMessageSize": {From: "params.maxMessageSize"}},
	})
	err := b.DeleteParameter("maxMessageSize")
	if err == nil {
		t.Fatal("want an error deleting a referenced parameter")
	}
	if !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to name the resource still referencing the parameter", err)
	}
	if !strings.Contains(err.Error(), "second-queue") {
		t.Errorf("err = %v, want it to name EVERY resource still referencing the parameter, not just the first", err)
	}
	if _, ok := b.Spec.XRD.Parameters["maxMessageSize"]; !ok {
		t.Error("parameter was deleted despite the error")
	}
}

func TestDeleteParameterSucceedsWhenUnreferenced(t *testing.T) {
	b := editable()
	if err := b.AddParameter("spare", Parameter{Type: "string"}); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteParameter("spare"); err != nil {
		t.Fatalf("DeleteParameter: %v", err)
	}
	if _, still := b.Spec.XRD.Parameters["spare"]; still {
		t.Error("parameter still present after delete")
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after delete: %v", err)
	}
}

func TestDeleteProviderNameIsRefusedForNamespacedScope(t *testing.T) {
	b := editable()
	if err := b.DeleteParameter("providerName"); err == nil {
		t.Fatal("want an error: a Namespaced XRD requires providerName")
	}
}

// --- forEach references ---

// editableWithForEach is editable() plus an integer count parameter and a
// resource repeated over it, for the rename/delete referencer rules below.
func editableWithForEach() *Blueprint {
	b := editable()
	b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer", Default: "2"}
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "replica-queue", Kind: "Queue",
		ForEach: "params.instanceCount",
		Fields:  map[string]Field{"region": {Value: "eu-north-1"}},
	})
	return b
}

// A rename must rewrite forEach references with the same discipline as field
// From references: a dangling forEach emits a Composition whose loop bound
// dereferences a parameter that no longer exists, which under
// missingkey=error can never render.
func TestRenameParameterRewritesForEachReferences(t *testing.T) {
	b := editableWithForEach()
	if err := b.RenameParameter("instanceCount", "replicas"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[1].ForEach; got != "params.replicas" {
		t.Errorf("ForEach = %q, want params.replicas", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after rename: %v", err)
	}
}

func TestRenameParameterFailedRenameLeavesForEachUntouched(t *testing.T) {
	b := editableWithForEach()
	if err := b.RenameParameter("instanceCount", "providerName"); err == nil {
		t.Fatal("want an error renaming onto an existing parameter")
	}
	if got := b.Spec.Resources[1].ForEach; got != "params.instanceCount" {
		t.Errorf("ForEach mutated by a failed rename: %q", got)
	}
}

// Deleting a parameter a forEach still references must be refused, and the
// error must name the looping resource — the same one-round-trip discipline
// DeleteParameter already gives field From references.
func TestDeleteParameterRefusesWhenForEachReferences(t *testing.T) {
	b := editableWithForEach()
	err := b.DeleteParameter("instanceCount")
	if err == nil {
		t.Fatal("want an error deleting a forEach-referenced parameter")
	}
	if !strings.Contains(err.Error(), "replica-queue") {
		t.Errorf("err = %v, want it to name the resource whose forEach still references the parameter", err)
	}
	if _, ok := b.Spec.XRD.Parameters["instanceCount"]; !ok {
		t.Error("parameter was deleted despite the error")
	}
}

// A resource that references the parameter through BOTH a field's from and
// its own forEach must appear once in the error, not twice.
func TestDeleteParameterNamesDualReferencerOnce(t *testing.T) {
	b := editableWithForEach()
	b.Spec.Resources[1].Fields["maxMessageSize"] = Field{From: "params.instanceCount"}
	err := b.DeleteParameter("instanceCount")
	if err == nil {
		t.Fatal("want an error deleting a referenced parameter")
	}
	if got := strings.Count(err.Error(), "replica-queue"); got != 1 {
		t.Errorf("err = %v: names replica-queue %d times, want exactly once", err, got)
	}
}
