package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// annotated is editable() plus a second resource carrying every annotation
// form the grammar admits, optionally mutated.
func annotated(mutate func(*Blueprint)) *Blueprint {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "sa", Kind: "ServiceAccount", Provider: NativeProvider,
		Annotations: map[string]Field{
			"eks.amazonaws.com/role-arn": {From: "resources.main-queue.status.atProvider.arn"},
			"example.com/size":           {From: "params.maxMessageSize"},
			"team":                       {Value: "platform"},
			"example.com/audit":          {Raw: `"true"`},
		},
	})
	if mutate != nil {
		mutate(b)
	}
	return b
}

func TestValidateAcceptsAnnotationEntries(t *testing.T) {
	if err := annotated(nil).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The key grammar is the Kubernetes qualified-name shape, NOT the camelCase
// path grammar: dashes, dots and one slash are the annotation idiom.
func TestValidateAcceptsIdiomaticAnnotationKeys(t *testing.T) {
	for _, key := range []string{
		"eks.amazonaws.com/role-arn",
		"role-arn",
		"a",
		"kubernetes.io/change-cause",
		"my_key.v1",
		"crossplane.io/external-name", // reserved is ONLY composition-resource-name
		strings.Repeat("n", 63),
	} {
		t.Run(key, func(t *testing.T) {
			b := annotated(func(b *Blueprint) {
				b.Spec.Resources[1].Annotations = map[string]Field{key: {Value: "v"}}
			})
			if err := b.Validate(); err != nil {
				t.Fatalf("Validate rejected legal key %q: %v", key, err)
			}
		})
	}
}

func TestValidateRejectsMalformedAnnotationKeys(t *testing.T) {
	cases := []struct{ key, wantErr string }{
		{"", "annotation key is empty"},
		{"a/b/c", "at most one '/'"},
		{"/name", "prefix before '/' is empty"},
		{"Example.com/name", "not a DNS subdomain"},
		{"example.com/", "name after '/' is empty"},
		{"example.com/" + strings.Repeat("n", 64), "at most 63"},
		{strings.Repeat("p", 254) + "/name", "at most 253"},
		{"has space", "not a valid annotation name"},
		{"-leading", "not a valid annotation name"},
		{"trailing-", "not a valid annotation name"},
		{"colon:name", "not a valid annotation name"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			b := annotated(func(b *Blueprint) {
				b.Spec.Resources[1].Annotations = map[string]Field{tc.key: {Value: "v"}}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate(%q) = %v, want %q", tc.key, err, tc.wantErr)
			}
		})
	}
}

// A newline in a key would escape the Composition's block scalar — the
// checkScalar class — and must be named as such, not as a shape problem.
func TestValidateRejectsControlCharactersInAnnotationKey(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.Resources[1].Annotations = map[string]Field{"bad\nkey": {Value: "v"}}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("Validate = %v, want the control-character refusal", err)
	}
}

func TestValidateRejectsControlCharactersInAnnotationForms(t *testing.T) {
	for _, f := range []Field{
		{Value: "line\nbreak"},
		{Raw: "line\nbreak"},
		{From: "params.\nx"},
	} {
		b := annotated(func(b *Blueprint) {
			b.Spec.Resources[1].Annotations = map[string]Field{"team": f}
		})
		err := b.Validate()
		if err == nil || !strings.Contains(err.Error(), "control character") {
			t.Fatalf("Validate(%+v) = %v, want the control-character refusal", f, err)
		}
	}
}

// Both spellings of the composition-resource-name annotation are the
// generator's own key — node identity — and a blueprint entry would silently
// collide with the function-set value.
func TestValidateRejectsReservedAnnotationKeys(t *testing.T) {
	for _, key := range []string{
		"crossplane.io/composition-resource-name",
		"gotemplating.fn.crossplane.io/composition-resource-name",
	} {
		t.Run(key, func(t *testing.T) {
			b := annotated(func(b *Blueprint) {
				b.Spec.Resources[1].Annotations = map[string]Field{key: {Value: "v"}}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "setResourceNameAnnotation") {
				t.Fatalf("Validate(%q) = %v, want the reserved-key refusal", key, err)
			}
		})
	}
}

func TestValidateRejectsAnnotationWithTwoFormsOrNone(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    Field
		want string
	}{
		{"two", Field{Value: "v", Raw: "r"}, "(got 2)"},
		{"none", Field{}, "(got 0)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := annotated(func(b *Blueprint) {
				b.Spec.Resources[1].Annotations["team"] = tc.f
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "exactly one of from, value, raw or template") ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want the exactly-one-of rule with %s", err, tc.want)
			}
		})
	}
}

func TestValidateRejectsAnnotationFromUnknownParameter(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.Resources[1].Annotations["team"] = Field{From: "params.nonexistent"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown parameter "nonexistent"`) {
		t.Fatalf("Validate = %v, want the unknown-parameter refusal", err)
	}
}

// A composite behind an annotation wire is the fmt-of-a-map class with a
// twist: the quote pipe would faithfully quote "map[k:v]" as the annotation
// string — legal, silently wrong — so the type is refused at the source.
func TestValidateRejectsAnnotationFromCompositeParameter(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.XRD.Parameters["labels"] = Parameter{Type: "object"}
		b.Spec.Resources[1].Annotations["team"] = Field{From: "params.labels"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "can only carry a scalar") {
		t.Fatalf("Validate = %v, want the composite refusal", err)
	}
}

func TestValidateRejectsAnnotationUnknownTemplate(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.Resources[1].Annotations["team"] = Field{Template: "nonexistent"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown template "nonexistent"`) {
		t.Fatalf("Validate = %v, want the unknown-template refusal", err)
	}
}

// Unlike a native FIELD, a native annotation may call a template: every
// annotation sits at one fixed column, so the fields rule's mechanical
// reason (re-indenting to the forProvider column) does not apply.
func TestValidateAcceptsTemplateAnnotationOnNativeResource(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.Templates = map[string]string{"team": "platform-{{ .resource }}"}
		b.Spec.Resources[1].Annotations["team"] = Field{Template: "team"}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The annotation status-wire grammar gets exactly the field wire's document-
// level checks, each error naming the annotation, not a "field".
func TestValidateAnnotationStatusWireChecks(t *testing.T) {
	cases := []struct {
		name, from, want string
	}{
		{"self", "resources.sa.status.atProvider.arn", "references its own status"},
		{"unknown", "resources.ghost.status.atProvider.arn", `references unknown resource "ghost"`},
		{"grammar", "resources.main-queue.statusatProvider", "must be resources.<name>.status.<path>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := annotated(func(b *Blueprint) {
				b.Spec.Resources[1].Annotations["eks.amazonaws.com/role-arn"] = Field{From: tc.from}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), `annotation "eks.amazonaws.com/role-arn"`) {
				t.Errorf("err = %v, want it to name the annotation, not a field", err)
			}
		})
	}
}

func TestValidateRejectsAnnotationWireFromLoopedResource(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer", Default: "2"}
		b.Spec.Resources[0].ForEach = "params.instanceCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "is looped") {
		t.Fatalf("Validate = %v, want the looped-target refusal", err)
	}
}

// An empty annotations map authors nothing and is legal — the same collapse
// ruling as Spec.Pipeline: omitempty folds it into an absent key on persist,
// and nothing is lost because the two mean the same thing.
func TestValidateAcceptsEmptyAnnotationsMap(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		b.Spec.Resources[1].Annotations = map[string]Field{}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The document round trip: annotations survive marshal -> unmarshal exactly,
// and a resource that never declared the key never gains `annotations:`.
func TestAnnotationsRoundTripExactly(t *testing.T) {
	b := annotated(nil)
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Blueprint
	if err := yaml.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(b.Spec.Resources[1].Annotations, back.Spec.Resources[1].Annotations); diff != "" {
		t.Errorf("annotations changed across the round trip (-want +got):\n%s", diff)
	}
	if back.Spec.Resources[0].Annotations != nil {
		t.Errorf("annotation-free resource gained annotations = %v across the round trip",
			back.Spec.Resources[0].Annotations)
	}
	if strings.Contains(strings.Split(string(body), "sa")[0], "annotations") {
		t.Errorf("annotation-free resource marshals with an annotations key:\n%s", body)
	}
}

// deepCopy discipline: a REJECTED edit must not have mutated the receiver's
// annotations through an aliased map.
func TestFailedEditLeavesAnnotationsUntouched(t *testing.T) {
	b := annotated(nil)
	want := map[string]Field{}
	for k, v := range b.Spec.Resources[1].Annotations {
		want[k] = v
	}
	// Renaming maxMessageSize to an illegal name fails Validate AFTER the
	// copy's annotation rewrite ran (example.com/size wires from it).
	if err := b.RenameParameter("maxMessageSize", "not valid"); err == nil {
		t.Fatal("rename to an illegal name should fail")
	}
	if diff := cmp.Diff(want, b.Spec.Resources[1].Annotations); diff != "" {
		t.Errorf("rejected rename mutated the receiver's annotations (-want +got):\n%s", diff)
	}
}

func TestRenameParameterRewritesAnnotationReferences(t *testing.T) {
	b := annotated(nil)
	if err := b.RenameParameter("maxMessageSize", "sizeLimit"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	got := b.Spec.Resources[1].Annotations["example.com/size"].From
	if got != "params.sizeLimit" {
		t.Errorf("annotation from = %q, want params.sizeLimit", got)
	}
}

func TestDeleteParameterRefusesWhenAnnotationReferences(t *testing.T) {
	b := annotated(func(b *Blueprint) {
		// Drop the field reference so the annotation is the ONLY reference —
		// otherwise the refusal could come from the field scan alone.
		b.Spec.Resources[0].Fields = map[string]Field{"region": {Value: "eu-north-1"}}
	})
	err := b.DeleteParameter("maxMessageSize")
	if err == nil || !strings.Contains(err.Error(), `still referenced by resources "sa"`) {
		t.Fatalf("DeleteParameter = %v, want the still-referenced refusal naming sa", err)
	}
}

func TestRenameResourceRewritesAnnotationStatusWires(t *testing.T) {
	b := annotated(nil)
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	got := b.Spec.Resources[1].Annotations["eks.amazonaws.com/role-arn"].From
	if got != "resources.primary-queue.status.atProvider.arn" {
		t.Errorf("annotation from = %q, want the rewritten resource name", got)
	}
}

func TestDeleteResourceRefusesWhenAnnotationWiresFromIt(t *testing.T) {
	err := annotated(nil).DeleteResource("main-queue")
	if err == nil || !strings.Contains(err.Error(), `still wired into resources "sa"`) {
		t.Fatalf("DeleteResource = %v, want the still-wired refusal naming sa", err)
	}
}
