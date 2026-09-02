package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// nestedFixture: an object parameter with members nested two levels deep —
// the "openapi spec editor" shape.
func nestedFixture() (*blueprint.Blueprint, []schema.CRD) {
	crds := []schema.CRD{{
		Group: "sqs.aws.m.upbound.io", Kind: "Queue", Plural: "queues",
		Scope: "Namespaced", Categories: []string{"managed"},
		Versions: []schema.Version{{
			Name: "v1beta1", Served: true, Storage: true,
			Properties: map[string]any{
				"spec": map[string]any{
					"properties": map[string]any{
						"forProvider": map[string]any{
							"properties": map[string]any{
								"region":         map[string]any{"type": "string"},
								"maxMessageSize": map[string]any{"type": "integer"},
							},
						},
					},
				},
			},
		}},
	}}
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xnested"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{Provider: "example.org/provider-test:v2"}},
			XRD: blueprint.XRD{
				Group: "platform.example.org", Kind: "XNested", Plural: "xnesteds",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"network": {Type: "object", Required: true, Properties: map[string]blueprint.Parameter{
						"cidr": {Type: "string", Required: true},
						"nat": {Type: "object", Properties: map[string]blueprint.Parameter{
							"gatewayRegion": {Type: "string", Required: true},
							"hops":          {Type: "integer"},
						}},
					}},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "q", Kind: "Queue",
				Provider: "example.org/provider-test:v2",
				Fields: map[string]blueprint.Field{
					"region": {From: "params.network.nat.gatewayRegion"},
				},
			}},
		},
	}
	return b, crds
}

func TestNestedObjectParameterValidates(t *testing.T) {
	b, _ := nestedFixture()
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestNestedObjectParameterXRDSchema(t *testing.T) {
	b, _ := nestedFixture()
	got, err := XRD(b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"network:", "cidr:", "nat:", "gatewayRegion:", "hops:",
		"required: [cidr]",          // level-1 required list
		"required: [gatewayRegion]", // level-2 required list
	} {
		if !strings.Contains(s, want) {
			t.Errorf("XRD missing %q:\n%s", want, s)
		}
	}
}

func TestNestedMemberWireRendersGuardedChain(t *testing.T) {
	b, crds := nestedFixture()
	got, err := Composition(b, crds)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "{{ $spec.network.nat.gatewayRegion }}") {
		t.Errorf("nested member dereference missing:\n%s", s)
	}
	// network is required, nat is optional: the guard chain starts at nat
	// and stays conservative through the leaf
	if !strings.Contains(s, `and (hasKey $spec.network "nat") (hasKey $spec.network.nat "gatewayRegion")`) {
		t.Errorf("conservative guard chain missing:\n%s", s)
	}
}

func TestNestedMemberUnknownLeafFails(t *testing.T) {
	b, _ := nestedFixture()
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{From: "params.network.nat.gatewayRegoin"}
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "gatewayRegoin") {
		t.Fatalf("unknown nested member not refused: %v", err)
	}
}

func TestNestedArrayMemberStillRefused(t *testing.T) {
	b, _ := nestedFixture()
	p := b.Spec.XRD.Parameters["network"]
	p.Properties["ports"] = blueprint.Parameter{Type: "array"}
	b.Spec.XRD.Parameters["network"] = p
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "array") {
		t.Fatalf("array member not refused: %v", err)
	}
}
