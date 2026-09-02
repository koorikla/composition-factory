package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseFromParamsForm(t *testing.T) {
	ref, err := ParseFrom("params.maxMessageSize")
	if err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	if ref.Param != "maxMessageSize" || ref.Resource != "" || ref.StatusPath != nil {
		t.Errorf("ref = %+v, want a pure params ref", ref)
	}
}

func TestParseFromResourcesStatusForm(t *testing.T) {
	ref, err := ParseFrom("resources.main-queue.status.atProvider.url")
	if err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	want := FromRef{Resource: "main-queue", StatusPath: []string{"atProvider", "url"}}
	if diff := cmp.Diff(want, ref); diff != "" {
		t.Errorf("ref mismatch (-want +got):\n%s", diff)
	}
}

// A single-segment status path is legal: a native-shaped resource may carry
// a scalar directly under status.
func TestParseFromSingleSegmentStatusPath(t *testing.T) {
	ref, err := ParseFrom("resources.thing.status.observedGeneration")
	if err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	want := FromRef{Resource: "thing", StatusPath: []string{"observedGeneration"}}
	if diff := cmp.Diff(want, ref); diff != "" {
		t.Errorf("ref mismatch (-want +got):\n%s", diff)
	}
}

func TestParseFromRejectsMalformedRefs(t *testing.T) {
	cases := []struct {
		name, from, wantIn string
	}{
		// The exact string "must start with params." is asserted elsewhere
		// too (TestValidateRejectsFromWithoutParamsPrefix): it is part of the
		// error's contract, not just its wording.
		{"no known prefix", "maxMessageSize", "must start with params."},
		{"resources with no status", "resources.main-queue", "resources.<name>.status.<path>"},
		{"resources with empty path", "resources.main-queue.status.", "resources.<name>.status.<path>"},
		{"resources with no name", "resources..status.url", "not a valid resource name"},
		{"spec not status", "resources.main-queue.spec.forProvider.region", "resources.<name>.status.<path>"},
		// Go template field access ($x.seg) only admits identifier-shaped
		// keys, so a dashed or indexed segment cannot be dereferenced by the
		// emitted template. Arrays are the loudest case: Leaves addresses
		// array elements as conditions[0].type, and [0] is not a template
		// identifier -- status wires cannot cross an array in M-scope.
		{"dashed segment", "resources.main-queue.status.at-provider.url", "identifier"},
		{"indexed segment", "resources.main-queue.status.conditions[0].type", "identifier"},
		{"empty middle segment", "resources.main-queue.status.atProvider..url", "identifier"},
		{"bare resources.", "resources.", "resources.<name>.status.<path>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFrom(tc.from)
			if err == nil {
				t.Fatalf("ParseFrom(%q) = nil error, want a refusal", tc.from)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantIn)
			}
		})
	}
}

// A resource name is a DNS label and never contains a dot, so the first
// ".status." unambiguously splits name from path -- even when the status
// path itself begins with a property literally named "status".
func TestParseFromStatusPropertyNamedStatus(t *testing.T) {
	ref, err := ParseFrom("resources.q.status.status.reason")
	if err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	want := FromRef{Resource: "q", StatusPath: []string{"status", "reason"}}
	if diff := cmp.Diff(want, ref); diff != "" {
		t.Errorf("ref mismatch (-want +got):\n%s", diff)
	}
}

// --- Validate over the resources.<name>.status.<path> form ---

// wiredBlueprint is scalarBlueprint plus a second resource wired from the
// first one's observed status.
func wiredBlueprint(mut ...func(*Blueprint)) *Blueprint {
	b := scalarBlueprint(func(*Blueprint) {})
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "queue-policy",
		Kind: "QueuePolicy",
		Fields: map[string]Field{
			"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
		},
	})
	for _, m := range mut {
		m(b)
	}
	return b
}

func TestValidateAcceptsStatusWireToDeclaredResource(t *testing.T) {
	if err := wiredBlueprint().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a status wire to a declared resource to be accepted", err)
	}
}

func TestValidateRejectsStatusWireToUnknownResource(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{From: "resources.no-such-queue.status.atProvider.url"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want a status wire to an undeclared resource to be refused")
	}
	if !strings.Contains(err.Error(), "no-such-queue") {
		t.Errorf("err = %v, want it to name the missing resource", err)
	}
}

// A wire from a resource's own status back into its own spec is a
// degenerate self-loop on the canvas. Crossplane could technically resolve
// it on a later reconcile, but no T1 pattern needs it and admitting it
// silently would let the GUI draw a node pointing at itself, so it is
// refused with an error that says so.
func TestValidateRejectsSelfStatusWire(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{From: "resources.queue-policy.status.atProvider.id"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want a self-referencing status wire to be refused")
	}
	if !strings.Contains(err.Error(), "own status") {
		t.Errorf("err = %v, want it to explain the self-reference", err)
	}
}

// A forward reference is legal: observed.resources is keyed by name at
// render time, so declaration order carries no meaning for status wires.
func TestValidateAcceptsForwardStatusWire(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		// Wire the FIRST resource from the SECOND one's status.
		b.Spec.Resources[0].Fields["policy"] = Field{From: "resources.queue-policy.status.atProvider.policy"}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a forward status wire to be accepted", err)
	}
}

// resources.<name> must be unambiguous for a status wire to mean anything
// (and the composition-resource-name annotation is node identity, spec §7),
// so duplicate resource names are refused at the source.
func TestValidateRefsRejectsDuplicateResourceNames(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Name = "main-queue"
		b.Spec.Resources[1].Fields = map[string]Field{}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want duplicate resource names to be refused")
	}
	if !strings.Contains(err.Error(), "main-queue") || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("err = %v, want it to name the duplicated resource name", err)
	}
}

func TestParseFromResourcesMetadataNameForm(t *testing.T) {
	ref, err := ParseFrom("resources.sa.metadata.name")
	if err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	want := FromRef{Resource: "sa", MetadataPath: "name"}
	if diff := cmp.Diff(want, ref); diff != "" {
		t.Errorf("ref mismatch (-want +got):\n%s", diff)
	}
	if !ref.IsMetadataName() {
		t.Errorf("IsMetadataName() = false, want true")
	}
}

func TestValidateAcceptsMetadataNameWire(t *testing.T) {
	b := scalarBlueprint(func(*Blueprint) {})
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "sa",
		Kind: "ServiceAccount",
		Fields: map[string]Field{
			"automountServiceAccountToken": {Value: "true"},
		},
	}, Resource{
		Name: "app",
		Kind: "Deployment",
		Fields: map[string]Field{
			"spec.template.spec.serviceAccountName": {From: "resources.sa.metadata.name"},
		},
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want metadata.name wire to be accepted", err)
	}
}

func TestValidateRejectsMetadataNameWireToSelf(t *testing.T) {
	b := scalarBlueprint(func(*Blueprint) {})
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "sa",
		Kind: "ServiceAccount",
		Fields: map[string]Field{
			"spec.template.spec.serviceAccountName": {From: "resources.sa.metadata.name"},
		},
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want self-referencing metadata.name to be refused")
	}
	if !strings.Contains(err.Error(), "own metadata.name") {
		t.Errorf("err = %v, want it to explain self-reference", err)
	}
}

func TestValidateRejectsMetadataNameWireToUnknownResource(t *testing.T) {
	b := scalarBlueprint(func(*Blueprint) {})
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "app",
		Kind: "Deployment",
		Fields: map[string]Field{
			"spec.template.spec.serviceAccountName": {From: "resources.unknown-sa.metadata.name"},
		},
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want unknown resource metadata.name wire to be refused")
	}
	if !strings.Contains(err.Error(), "unknown-sa") {
		t.Errorf("err = %v, want it to name missing resource", err)
	}
}
