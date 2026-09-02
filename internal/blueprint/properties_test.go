// Tests for typed object parameters: an object parameter that declares
// typed members under properties, and the params.<name>.<member> reference
// grammar those members make legal in fields and envelope entries.
package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// validTyped is a complete valid blueprint whose tuning parameter declares
// typed members covering all four member types plus required/default/enum/
// description, wired into fields through the member reference grammar.
const validTyped = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.hooli.tech
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      tuning:
        type: object
        properties:
          maxSize: {type: integer, default: "2048", description: "Maximum message size."}
          mode: {type: string, required: true, enum: [standard, fifo]}
          delayed: {type: boolean}
          jitter: {type: number}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {from: params.tuning.maxSize}
`

// typedBlueprint loads validTyped and applies mutate, so each rejection test
// states only its own delta.
func typedBlueprint(t *testing.T, mutate func(*Blueprint)) *Blueprint {
	t.Helper()
	b, err := Load(write(t, validTyped))
	if err != nil {
		t.Fatalf("Load(validTyped): %v", err)
	}
	if mutate != nil {
		mutate(b)
	}
	return b
}

func TestLoadValidTypedObjectParameter(t *testing.T) {
	b := typedBlueprint(t, nil)
	p := b.Spec.XRD.Parameters["tuning"]
	if len(p.Properties) != 4 {
		t.Fatalf("tuning.Properties has %d members, want 4: %+v", len(p.Properties), p.Properties)
	}
	if m := p.Properties["maxSize"]; m.Type != "integer" || m.Default != "2048" {
		t.Errorf("maxSize = %+v, want an integer with default 2048", m)
	}
	if m := p.Properties["mode"]; !m.Required || len(m.Enum) != 2 {
		t.Errorf("mode = %+v, want required with 2 enum values", m)
	}
}

// The properties key must survive a marshal/unmarshal round trip exactly:
// the HTTP API persists the whole document by re-marshaling the Go struct,
// so a key the struct dropped would be erased on the first edit anyone makes
// through the API. Byte-exactness of the re-marshal is asserted too — the
// same document must serialize identically every time, or `cf gen --check`
// on a persisted blueprint drifts.
func TestPropertiesRoundTripExactly(t *testing.T) {
	b := typedBlueprint(t, nil)
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
	again, err := yaml.Marshal(&reloaded)
	if err != nil {
		t.Fatalf("Marshal (second): %v", err)
	}
	if string(body) != string(again) {
		t.Errorf("re-marshal is not byte-identical:\n-- first --\n%s\n-- second --\n%s", body, again)
	}
}

// A parameter that declares no properties must not gain a properties key on
// marshal: the HTTP API re-marshals every parameter on every edit, and a
// sudden `properties: null` on every scalar parameter would rewrite every
// blueprint the first time anyone touched it.
func TestPropertylessParameterMarshalsWithoutPropertiesKey(t *testing.T) {
	body, err := yaml.Marshal(Parameter{Type: "string"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(body), "properties") {
		t.Errorf("a propertyless parameter marshaled a properties key:\n%s", body)
	}
}

func TestValidateRejectsPropertiesOnNonObjectType(t *testing.T) {
	for _, typ := range []string{"string", "integer", "number", "boolean"} {
		t.Run(typ, func(t *testing.T) {
			b := typedBlueprint(t, func(b *Blueprint) {
				p := b.Spec.XRD.Parameters["tuning"]
				p.Type = typ
				p.Properties = map[string]Parameter{"x": {Type: "string"}}
				b.Spec.XRD.Parameters["tuning"] = p
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "properties") || !strings.Contains(err.Error(), "object") {
				t.Fatalf("err = %v, want a complaint that properties is only valid on type object", err)
			}
			if !strings.Contains(err.Error(), "tuning") {
				t.Errorf("err = %v, want it to name the parameter", err)
			}
		})
	}
}

// Members nest to arbitrary depth (the openapi-editor shape); properties on
// a SCALAR member stay refused with an error naming the member's type.
func TestValidateAcceptsNestedMemberProperties(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["nested"] = Parameter{
			Type:       "object",
			Properties: map[string]Parameter{"deep": {Type: "string"}},
		}
		b.Spec.XRD.Parameters["tuning"] = p
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("nested object member refused: %v", err)
	}
}

func TestValidateRejectsPropertiesOnScalarMember(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["odd"] = Parameter{
			Type:       "string",
			Properties: map[string]Parameter{"deep": {Type: "string"}},
		}
		b.Spec.XRD.Parameters["tuning"] = p
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "only valid on type: object") {
		t.Fatalf("err = %v, want the properties-on-scalar refusal", err)
	}
}

func TestValidateRejectsArrayMembers(t *testing.T) {
	// object members are now real nested schemas; array stays refused
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["bad"] = Parameter{Type: "array"}
		b.Spec.XRD.Parameters["tuning"] = p
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "scalar") {
		t.Fatalf("err = %v, want a complaint that array members are refused", err)
	}
	if !strings.Contains(err.Error(), `.bad`) {
		t.Errorf("err = %v, want it to name the member", err)
	}
}

func TestValidateRejectsUnknownMemberType(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["bad"] = Parameter{Type: "strnig"}
		b.Spec.XRD.Parameters["tuning"] = p
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"strnig"`) {
		t.Fatalf("err = %v, want an error naming the unknown type", err)
	}
}

// Member names become raw YAML map keys in the emitted XRD schema, exactly
// like parameter names, so they get the same identifier discipline.
func TestValidateRejectsInvalidMemberNames(t *testing.T) {
	for _, name := range []string{"has space", "true", "colon:bad"} {
		t.Run(name, func(t *testing.T) {
			b := typedBlueprint(t, func(b *Blueprint) {
				p := b.Spec.XRD.Parameters["tuning"]
				p.Properties[name] = Parameter{Type: "string"}
				b.Spec.XRD.Parameters["tuning"] = p
			})
			if err := b.Validate(); err == nil {
				t.Fatalf("Validate accepted member name %q", name)
			}
		})
	}
}

// Members get the same default-vs-type and scalar-content rules top-level
// parameters get — the same code path, not a copy.
func TestValidateAppliesTopLevelRulesToMembers(t *testing.T) {
	cases := []struct {
		name   string
		member Parameter
		want   string
	}{
		{"bad integer default", Parameter{Type: "integer", Default: "abc"}, "not a valid integer"},
		{"bad boolean default", Parameter{Type: "boolean", Default: "maybe"}, "not a valid boolean"},
		{"bad number default", Parameter{Type: "number", Default: "1.2.3"}, "not a valid number"},
		{"control character in description", Parameter{Type: "string", Description: "line\nbreak"}, "control character"},
		{"control character in enum", Parameter{Type: "string", Enum: []string{"a\nb"}}, "control character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := typedBlueprint(t, func(b *Blueprint) {
				p := b.Spec.XRD.Parameters["tuning"]
				p.Properties["probe"] = tc.member
				b.Spec.XRD.Parameters["tuning"] = p
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "probe") {
				t.Errorf("err = %v, want it to name the member", err)
			}
		})
	}
}

// --- the params.<name>.<member> reference grammar ---

func TestValidateAcceptsMemberReferenceInField(t *testing.T) {
	if err := typedBlueprint(t, nil).Validate(); err != nil {
		t.Fatalf("Validate: %v — params.tuning.maxSize wires a declared member", err)
	}
}

func TestValidateRejectsUnknownMemberReference(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.Resources[0].Fields["maxMessageSize"] = Field{From: "params.tuning.nope"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "tuning") {
		t.Fatalf("err = %v, want an error naming the unknown member and its parameter", err)
	}
	// The declared members are listed so the fix is one round-trip.
	if !strings.Contains(err.Error(), "maxSize") {
		t.Errorf("err = %v, want it to list the declared members", err)
	}
}

func TestValidateRejectsMemberReferenceOnPropertylessObject(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.XRD.Parameters["bag"] = Parameter{Type: "object"}
		b.Spec.Resources[0].Fields["maxMessageSize"] = Field{From: "params.bag.key"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "properties") {
		t.Fatalf("err = %v, want a complaint that the parameter declares no properties", err)
	}
}

func TestValidateRejectsMemberReferenceOnScalarParameter(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.Resources[0].Fields["maxMessageSize"] = Field{From: "params.providerName.x"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"string"`) {
		t.Fatalf("err = %v, want a complaint naming the parameter's scalar type", err)
	}
}

func TestValidateRejectsChainThroughScalarMember(t *testing.T) {
	// deep chains are legal now, but only through object members — a chain
	// that descends through a scalar names the scalar's type
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.Resources[0].Fields["maxMessageSize"] = Field{From: "params.tuning.maxSize.deeper"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "not \"object\"") {
		t.Fatalf("err = %v, want the chain-through-scalar refusal", err)
	}
}

// A bare from: on a typed object still cannot render (it is a composite),
// but the error must now point at the member grammar instead of a dead end.
func TestValidateBareTypedObjectFromSuggestsMembers(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.Resources[0].Fields["maxMessageSize"] = Field{From: "params.tuning"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate accepted a bare from: on an object parameter")
	}
	if !strings.Contains(err.Error(), "params.tuning.<member>") {
		t.Errorf("err = %v, want it to suggest the member reference grammar", err)
	}
}

// --- forEach and when stay top-level in v1 ---

func TestValidateRejectsForEachOverObjectMember(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["replicas"] = Parameter{Type: "integer", Default: "2"}
		b.Spec.XRD.Parameters["tuning"] = p
		b.Spec.Resources[0].ForEach = "params.tuning.replicas"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "top-level") {
		t.Fatalf("err = %v, want a complaint that forEach loop bounds stay top-level in v1", err)
	}
	if !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to name the resource", err)
	}
}

func TestValidateRejectsWhenOverObjectMember(t *testing.T) {
	for _, expr := range []string{
		"params.tuning.delayed",
		`params.tuning.mode == "fifo"`,
	} {
		t.Run(expr, func(t *testing.T) {
			b := typedBlueprint(t, func(b *Blueprint) {
				b.Spec.Resources[0].When = expr
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "top-level") {
				t.Fatalf("err = %v, want a complaint that when conditions stay top-level in v1", err)
			}
		})
	}
}

// --- envelope entries take the same member grammar as fields ---

func TestValidateAcceptsMemberReferenceInEnvelope(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["secretName"] = Parameter{Type: "string"}
		b.Spec.XRD.Parameters["tuning"] = p
		b.Spec.Resources[0].Envelope = map[string]Field{
			"writeConnectionSecretToRef.name": {From: "params.tuning.secretName"},
		}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v — an envelope entry may wire a declared member", err)
	}
}

func TestValidateRejectsUnknownMemberReferenceInEnvelope(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.Resources[0].Envelope = map[string]Field{
			"writeConnectionSecretToRef.name": {From: "params.tuning.nope"},
		}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown member", err)
	}
	if !strings.Contains(err.Error(), "envelope") {
		t.Errorf("err = %v, want it to name the envelope entry", err)
	}
}

// --- ParamRef, the shared reference splitter ---

func TestParamRef(t *testing.T) {
	cases := []struct {
		ref, param, member string
		ok                 bool
	}{
		{"params.size", "size", "", true},
		{"params.obj.member", "obj", "member", true},
		{"params.obj.a.b", "obj", "a.b", true},
		{"resources.q.status.x", "", "", false},
		{"size", "", "", false},
	}
	for _, tc := range cases {
		param, member, ok := ParamRef(tc.ref)
		if param != tc.param || member != tc.member || ok != tc.ok {
			t.Errorf("ParamRef(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.ref, param, member, ok, tc.param, tc.member, tc.ok)
		}
	}
}

// --- edit.go referencer discipline ---

// RenameParameter rewrites the params.<from> PREFIX, so member references
// follow the parameter to its new name — there are no member-level rename
// routes in v1, the parameter-level rename is the one rewrite.
func TestRenameParameterRewritesMemberReferences(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["tuning"]
		p.Properties["secretName"] = Parameter{Type: "string"}
		b.Spec.XRD.Parameters["tuning"] = p
		b.Spec.Resources[0].Envelope = map[string]Field{
			"writeConnectionSecretToRef.name": {From: "params.tuning.secretName"},
		}
	})
	if err := b.RenameParameter("tuning", "settings"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[0].Fields["maxMessageSize"].From; got != "params.settings.maxSize" {
		t.Errorf("field From = %q, want params.settings.maxSize", got)
	}
	if got := b.Spec.Resources[0].Envelope["writeConnectionSecretToRef.name"].From; got != "params.settings.secretName" {
		t.Errorf("envelope From = %q, want params.settings.secretName", got)
	}
}

// A rename must not rewrite a parameter whose name merely shares the prefix
// as a string: params.tuningX is not a member reference to tuning.
func TestRenameParameterLeavesPrefixLookalikesAlone(t *testing.T) {
	b := typedBlueprint(t, func(b *Blueprint) {
		b.Spec.XRD.Parameters["tuningX"] = Parameter{Type: "integer"}
		b.Spec.Resources[0].Fields["delaySeconds"] = Field{From: "params.tuningX"}
	})
	if err := b.RenameParameter("tuning", "settings"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[0].Fields["delaySeconds"].From; got != "params.tuningX" {
		t.Errorf("field From = %q, want params.tuningX untouched", got)
	}
}

// DeleteParameter refuses while member references stand, naming the
// referencing resource — a dangling member reference would be an
// unrenderable Composition, the same failure a dangling params.<name> is.
func TestDeleteParameterRefusesWhileMemberReferenced(t *testing.T) {
	b := typedBlueprint(t, nil)
	err := b.DeleteParameter("tuning")
	if err == nil || !strings.Contains(err.Error(), "main-queue") {
		t.Fatalf("err = %v, want a refusal naming the referencing resource", err)
	}
	if _, still := b.Spec.XRD.Parameters["tuning"]; !still {
		t.Error("a refused delete must leave the receiver unchanged")
	}
}

// A failed edit must leave the receiver's Properties untouched — deepCopy
// has to copy the member map, or the "rejected" candidate's mutations leak
// through the aliased backing store.
func TestFailedEditLeavesPropertiesUnchanged(t *testing.T) {
	b := typedBlueprint(t, nil)
	// "on" is a YAML keyword, so the candidate fails Validate after the
	// copy's maps were already rewritten.
	if err := b.RenameParameter("tuning", "true"); err == nil {
		t.Fatal("RenameParameter accepted a YAML-keyword name")
	}
	if got := b.Spec.Resources[0].Fields["maxMessageSize"].From; got != "params.tuning.maxSize" {
		t.Errorf("field From = %q after a failed rename, want params.tuning.maxSize", got)
	}
	if _, ok := b.Spec.XRD.Parameters["tuning"].Properties["maxSize"]; !ok {
		t.Error("tuning.Properties lost maxSize across a failed edit")
	}
}
