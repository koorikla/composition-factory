// internal/examples/roundtrip_fixture_test.go
package examples

import (
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// The Lane C round-trip gate is only as strong as the blueprint it carries through
// the API server. This pins the feature coverage of that fixture, so a later edit
// cannot quietly narrow it back to a shape that round-trips trivially.
func TestRoundTripFixtureCoversTheLossyFeatures(t *testing.T) {
	data, err := os.ReadFile("testdata/roundtrip-full.cf.yaml")
	if err != nil {
		t.Fatalf("read round-trip fixture: %v", err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var managed, statusWire, envelope, forEach int
	for _, r := range b.Spec.Resources {
		if r.Provider != blueprint.NativeProvider {
			managed++
			envelope += len(r.Envelope)
		}
		if r.ForEach != "" {
			forEach++
		}
		for _, f := range r.Fields {
			if strings.Contains(f.From, ".status.atProvider.") {
				statusWire++
			}
		}
	}

	for _, c := range []struct {
		name string
		got  int
	}{
		{"managed resources", managed},
		{"atProvider status wires", statusWire},
		{"envelope entries on a managed resource", envelope},
		{"forEach resources", forEach},
		{"spec.environment keys", len(b.Spec.Environment)},
	} {
		if c.got == 0 {
			t.Errorf("round-trip fixture has no %s; the gate cannot fail on that loss", c.name)
		}
	}
}
