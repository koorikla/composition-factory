package blueprint

import (
	"strings"
	"testing"
)

func TestRenameResourceRewritesStatusWires(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if b.Spec.Resources[0].Name != "primary-queue" {
		t.Errorf("resource name = %q, want primary-queue", b.Spec.Resources[0].Name)
	}
	got := b.Spec.Resources[1].Fields["queueUrl"].From
	if got != "resources.primary-queue.status.atProvider.url" {
		t.Errorf("wire From = %q, want it rewritten to the new resource name", got)
	}
}

// The rewrite is prefix-exact: renaming "main" must not touch a wire into
// "main-queue" whose name merely shares a prefix.
func TestRenameResourceDoesNotRewritePrefixSharingNames(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
	})
	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	got := b.Spec.Resources[1].Fields["queueUrl"].From
	if got != "resources.main-queue.status.atProvider.url" {
		t.Errorf("wire From = %q, want the main-queue wire left untouched", got)
	}
}

// Params references are a different namespace: renaming a resource must
// never rewrite a params.<name> reference, even one spelled identically.
func TestRenameResourceLeavesParamsRefsAlone(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["mainQueue"] = Parameter{Type: "string"}
		b.Spec.Resources[1].Fields["policy"] = Field{From: "params.mainQueue"}
	})
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if got := b.Spec.Resources[1].Fields["policy"].From; got != "params.mainQueue" {
		t.Errorf("params ref = %q, want it untouched by a resource rename", got)
	}
}

func TestRenameResourceUnknownFromErrors(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("nope", "other"); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}

// from == to is a no-op success, matching RenameParameter: a blur-submit UI
// resubmits unchanged names routinely.
func TestRenameResourceSameNameIsANoOp(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "main-queue"); err != nil {
		t.Fatalf("RenameResource(same, same) = %v, want nil", err)
	}
	if err := b.RenameResource("absent", "absent"); err == nil {
		t.Fatal("renaming an unknown resource to itself must still error: there is nothing to rename")
	}
}

func TestRenameResourceCollisionErrors(t *testing.T) {
	b := wiredBlueprint()
	err := b.RenameResource("main-queue", "queue-policy")
	if err == nil || !strings.Contains(err.Error(), "queue-policy") {
		t.Fatalf("err = %v, want a collision error naming the existing resource", err)
	}
	// The receiver is untouched on failure.
	if b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resource name = %q after a failed rename, want main-queue", b.Spec.Resources[0].Name)
	}
}

func TestRenameResourceToInvalidNameLeavesReceiverUntouched(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "Not_A_DNS_Label"); err == nil {
		t.Fatal("RenameResource to a non-DNS-label name must fail Validate")
	}
	if b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resource name = %q after a failed rename, want main-queue", b.Spec.Resources[0].Name)
	}
	if got := b.Spec.Resources[1].Fields["queueUrl"].From; got != "resources.main-queue.status.atProvider.url" {
		t.Errorf("wire From = %q after a failed rename, want it untouched", got)
	}
}

func TestDeleteResourceRefusesWhileStatusWiresReferenceIt(t *testing.T) {
	b := wiredBlueprint()
	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want a refusal while queue-policy still wires from its status")
	}
	if !strings.Contains(err.Error(), "queue-policy") {
		t.Errorf("err = %v, want it to name every referencing resource", err)
	}
	if len(b.Spec.Resources) != 2 {
		t.Errorf("resources = %d after a refused delete, want 2", len(b.Spec.Resources))
	}
}

func TestDeleteResourceSucceedsOnceUnreferenced(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "https://example.test/q"}
	})
	if err := b.DeleteResource("main-queue"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "queue-policy" {
		t.Errorf("resources = %+v, want only queue-policy left", b.Spec.Resources)
	}
}

func TestDeleteResourceUnknownNameErrors(t *testing.T) {
	b := wiredBlueprint()
	if err := b.DeleteResource("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}
