package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// envelopeBlueprint is editable() plus a secretName parameter and an
// envelope wiring it, optionally mutated.
func envelopeBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := editable()
	b.Spec.XRD.Parameters["secretName"] = Parameter{Type: "string", Required: true}
	b.Spec.Resources[0].Envelope = map[string]Field{
		"writeConnectionSecretToRef.name": {From: "params.secretName"},
		"managementPolicies":              {Value: "Observe, Create"},
	}
	if mutate != nil {
		mutate(b)
	}
	return b
}

func TestValidateAcceptsEnvelopeEntries(t *testing.T) {
	if err := envelopeBlueprint(nil).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsEnvelopeWithTwoForms(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Envelope["managementPolicies"] = Field{Value: "Observe", Raw: "['*']"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "exactly one of from, value or raw") {
		t.Fatalf("err = %v, want the exactly-one-of rule", err)
	}
	if !strings.Contains(err.Error(), "managementPolicies") || !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to name the resource and the envelope path", err)
	}
}

func TestValidateRejectsEnvelopeWithNoForm(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Envelope["managementPolicies"] = Field{}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "(got 0)") {
		t.Fatalf("err = %v, want the exactly-one-of rule with a zero count", err)
	}
}

// providerConfigRef is derived from providerName — one source of truth. Both
// the bare key and any child path are refused.
func TestValidateRejectsProviderConfigRefViaEnvelope(t *testing.T) {
	for _, path := range []string{"providerConfigRef", "providerConfigRef.name"} {
		t.Run(path, func(t *testing.T) {
			b := envelopeBlueprint(func(b *Blueprint) {
				b.Spec.Resources[0].Envelope = map[string]Field{path: {Value: "other"}}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "providerName") {
				t.Fatalf("err = %v, want a refusal explaining providerConfigRef is derived from providerName", err)
			}
		})
	}
}

func TestValidateRejectsEnvelopePathSegments(t *testing.T) {
	cases := []struct{ name, path string }{
		{"empty segment", "writeConnectionSecretToRef..name"},
		{"indexed segment", "policies[0]"},
		{"yaml keyword segment", "no"},
		{"colon in segment", "a:b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := envelopeBlueprint(func(b *Blueprint) {
				b.Spec.Resources[0].Envelope = map[string]Field{tc.path: {Value: "x"}}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "envelope") {
				t.Fatalf("err = %v, want an envelope key-shape error", err)
			}
		})
	}
}

// "a" (whole node) and "a.b" (one child) define the same node twice; the
// emitter would have to pick one silently.
func TestValidateRejectsEnvelopePrefixConflict(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Envelope = map[string]Field{
			"writeConnectionSecretToRef":      {Raw: "{name: fixed}"},
			"writeConnectionSecretToRef.name": {From: "params.secretName"},
		}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want a prefix-conflict error", err)
	}
}

func TestValidateRejectsEnvelopeFromUnknownParameter(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"] = Field{From: "params.nope"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}

func TestValidateRejectsEnvelopeFromWithoutParamsPrefix(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"] = Field{From: "secretName"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "params.") {
		t.Fatalf("err = %v, want the params. grammar rule", err)
	}
}

// A cross-resource status wire is a fields:-only grammar in v1: the envelope
// planner (internal/emit/envelope.go) models a wire's guard as a hasKey over
// $spec — a parameter presence check — not the observed-resources hasKey
// chain a status reference needs, so admitting the grammar here would emit a
// guard that can never be true. Refused with the limitation named, not the
// generic prefix complaint.
func TestValidateRejectsStatusWireInEnvelope(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "other-queue", Kind: "Queue",
			Fields: map[string]Field{"region": {Value: "eu-north-1"}},
		})
		b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"] =
			Field{From: "resources.other-queue.status.atProvider.url"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate accepted a cross-resource status wire in an envelope entry")
	}
	for _, want := range []string{"params.", "status wires", "fields:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestValidateRejectsEnvelopeFromCompositeParameter(t *testing.T) {
	b := envelopeBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["tags"] = Parameter{Type: "object"}
		b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"] = Field{From: "params.tags"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "composite") {
		t.Fatalf("err = %v, want the composite-parameter refusal", err)
	}
}

func TestValidateRejectsControlCharactersInEnvelopeForms(t *testing.T) {
	for _, form := range []Field{
		{Value: "Observe\ninjected: true"},
		{Raw: "['*']\ninjected: true"},
	} {
		b := envelopeBlueprint(func(b *Blueprint) {
			b.Spec.Resources[0].Envelope["managementPolicies"] = form
		})
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "control character") {
			t.Fatalf("form %+v: err = %v, want the control-character refusal", form, err)
		}
	}
}

// The envelope key must survive a marshal/unmarshal round trip exactly, for
// the same reason forEach must: the HTTP API persists the whole document by
// re-marshaling the Go struct.
func TestEnvelopeRoundTripsExactly(t *testing.T) {
	b := envelopeBlueprint(nil)
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-marshaled +reloaded):\n%s", diff)
	}
	if got := reloaded.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"].From; got != "params.secretName" {
		t.Errorf("envelope wire after round trip = %q, want params.secretName", got)
	}
}

// omitempty: a resource that never declared envelope must not gain a literal
// `envelope: null` when the document is persisted back.
func TestEnvelopeFreeResourceMarshalsWithoutEnvelopeKey(t *testing.T) {
	body, err := yaml.Marshal(editable())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(body), "envelope") {
		t.Errorf("an envelope-free blueprint marshaled an envelope key:\n%s", body)
	}
}

// --- edit-layer referencer discipline ---

func TestRenameParameterRewritesEnvelopeReferences(t *testing.T) {
	b := envelopeBlueprint(nil)
	if err := b.RenameParameter("secretName", "connSecretName"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"].From; got != "params.connSecretName" {
		t.Errorf("envelope wire = %q, want params.connSecretName", got)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("blueprint invalid after rename: %v", err)
	}
}

func TestDeleteParameterRefusesWhenEnvelopeReferences(t *testing.T) {
	b := envelopeBlueprint(nil)
	err := b.DeleteParameter("secretName")
	if err == nil || !strings.Contains(err.Error(), "main-queue") {
		t.Fatalf("err = %v, want a still-referenced refusal naming main-queue", err)
	}
	if _, exists := b.Spec.XRD.Parameters["secretName"]; !exists {
		t.Error("a refused delete removed the parameter anyway")
	}
}

// A rejected edit must not mutate the receiver's envelope through an aliased
// map — the deepCopy maintenance note's exact failure mode, exercised for
// the new map-typed field.
func TestFailedRenameLeavesEnvelopeUntouched(t *testing.T) {
	b := envelopeBlueprint(nil)
	// providerName exists, so this rename collides and must fail.
	if err := b.RenameParameter("secretName", "providerName"); err == nil {
		t.Fatal("want a collision error")
	}
	if got := b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"].From; got != "params.secretName" {
		t.Errorf("a failed rename mutated the envelope wire to %q", got)
	}
}
