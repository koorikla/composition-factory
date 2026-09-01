package blueprint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// templatedBlueprint is a valid blueprint carrying two user templates, a
// convention binding each, and one resource that calls one explicitly.
// Mutations build every rejection case from this known-good baseline.
func templatedBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		Metadata: Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Templates: map[string]string{
				"cf.name": "{{ .xr }}-{{ .resource }}",
				"cf.tags": "managed-by: crossplane\nxr: {{ .xr | quote }}",
			},
			Conventions: []Convention{
				{Match: "name", Template: "cf.name"},
				{Match: "tags", Template: "cf.tags"},
			},
			Resources: []Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]Field{
					"region": {Value: "eu-north-1"},
					"name":   {Template: "cf.name"},
				},
			}},
		},
	}
	mutate(b)
	return b
}

func TestValidateAcceptsTemplatesAndConventions(t *testing.T) {
	b := templatedBlueprint(func(*Blueprint) {})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want templates, conventions and a template field accepted", err)
	}
}

// A body may use the whole rendering function set: sprig, and
// function-go-templating's own additions.
func TestValidateAcceptsEngineFunctionCalls(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Templates["cf.rich"] = `{{ .spec | toYaml }}{{ include "cf.name" . }}{{ .xr | upper | quote }}`
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want sprig and engine functions to parse", err)
	}
}

func TestValidateRejectsInvalidTemplateName(t *testing.T) {
	for _, name := range []string{"", "1name", "has space", `has"quote`, "-leading"} {
		t.Run(name, func(t *testing.T) {
			b := templatedBlueprint(func(b *Blueprint) {
				b.Spec.Templates[name] = "x"
			})
			if err := b.Validate(); err == nil {
				t.Fatalf("Validate accepted template name %q", name)
			}
		})
	}
}

func TestValidateRejectsEmptyTemplateBody(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) { b.Spec.Templates["cf.empty"] = "" })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "cf.empty") {
		t.Fatalf("err = %v, want the empty body refused by name", err)
	}
}

// The real-engine parse: an unbalanced action or an unknown function fails
// at generation time, not at render time.
func TestValidateRejectsUnparseableTemplateBody(t *testing.T) {
	cases := []struct{ name, body string }{
		{"unbalanced action", "{{ .xr "},
		{"unknown function", "{{ noSuchFunction .xr }}"},
		{"unclosed if", "{{ if .xr }}yes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := templatedBlueprint(func(b *Blueprint) { b.Spec.Templates["cf.bad"] = tc.body })
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "cf.bad") {
				t.Fatalf("err = %v, want the unparseable body refused by name", err)
			}
		})
	}
}

// function-go-templating deletes sprig's env and expandenv (information
// leakage), so a body calling either must fail HERE exactly as it would fail
// the render — accepting it would validate a body the engine then rejects.
func TestValidateRejectsEnvFunctions(t *testing.T) {
	for _, body := range []string{`{{ env "HOME" }}`, `{{ expandenv "$HOME" }}`} {
		t.Run(body, func(t *testing.T) {
			b := templatedBlueprint(func(b *Blueprint) { b.Spec.Templates["cf.leak"] = body })
			if err := b.Validate(); err == nil {
				t.Fatalf("Validate accepted %q; the engine deletes env/expandenv, so the render would fail", body)
			}
		})
	}
}

// The injection case: a body that closes its own define block and re-opens a
// balanced one PARSES cleanly, and everything between would land as
// top-level template text of the Composition — injected into every rendered
// document. The whitespace-only-root check must refuse it.
func TestValidateRejectsBodyEscapingItsDefineBlock(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Templates["cf.escape"] = "inside\n{{- end }}\ninjected: content\n{{- define \"cf.reopened\" }}\ntail"
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate accepted a body that escapes its define block; the escaped content " +
			"would be injected verbatim into every rendered document")
	}
	if !strings.Contains(err.Error(), "cf.escape") {
		t.Errorf("err = %v, want it to name the offending template", err)
	}
}

// A define nested INSIDE a body is already a parse error ("unexpected
// <define> in command"), so the reachable extra-define shape is the sneaky
// one: close the block, open a new define, leave the root whitespace-only.
// The whitespace check cannot catch it — the Templates() enumeration must.
func TestValidateRejectsBodyDefiningExtraTemplates(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Templates["cf.outer"] = "x\n{{- end }}\n{{- define \"cf.helper\" }}\ny"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "cf.helper") {
		t.Fatalf("err = %v, want the extra define refused and named -- a helper belongs in "+
			"spec.templates under its own name, where it gets the same validation", err)
	}
}

func TestValidateRejectsCarriageReturnInTemplateBody(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Templates["cf.crlf"] = "line one\r\nline two"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("err = %v, want the carriage return refused (emit normalizes CRLF, which would "+
			"silently change the body)", err)
	}
}

func TestValidateAcceptsNewlinesAndTabsInTemplateBody(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Templates["cf.multi"] = "a: 1\nb:\t{{ .xr }}\nc: 3"
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want newlines and tabs accepted in a template body", err)
	}
}

func TestValidateRejectsConventionProblems(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Blueprint)
		want   string
	}{
		{"empty match", func(b *Blueprint) {
			b.Spec.Conventions = append(b.Spec.Conventions, Convention{Template: "cf.name"})
		}, "match is required"},
		{"invalid match", func(b *Blueprint) {
			b.Spec.Conventions = append(b.Spec.Conventions, Convention{Match: "na-me", Template: "cf.name"})
		}, "field-name suffix"},
		{"empty template", func(b *Blueprint) {
			b.Spec.Conventions = append(b.Spec.Conventions, Convention{Match: "name"})
		}, "template is required"},
		{"unknown template", func(b *Blueprint) {
			b.Spec.Conventions = append(b.Spec.Conventions, Convention{Match: "name", Template: "cf.nope"})
		}, `"cf.nope"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := templatedBlueprint(tc.mutate)
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateRejectsFieldReferencingUnknownTemplate(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Fields["name"] = Field{Template: "cf.nope"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"cf.nope"`) {
		t.Fatalf("err = %v, want the unknown template reference refused", err)
	}
}

func TestValidateRejectsFieldWithTemplateAndValue(t *testing.T) {
	b := templatedBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].Fields["name"] = Field{Template: "cf.name", Value: "x"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want the exactly-one-of rule to cover template", err)
	}
}

// Templates (a multi-line body included), conventions and a template field
// must survive a marshal/unmarshal round trip exactly: the HTTP API persists
// the whole document by re-marshaling the Go struct.
func TestTemplatesRoundTripExactly(t *testing.T) {
	b := templatedBlueprint(func(*Blueprint) {})
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-original +reloaded):\n%s", diff)
	}
	// Byte-exact, not merely equivalent: the API persists by re-marshaling,
	// so a second marshal of the reloaded document must reproduce the first
	// byte for byte or every no-op edit would churn the file on disk.
	again, err := yaml.Marshal(&reloaded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(again) != string(body) {
		t.Errorf("marshal -> unmarshal -> marshal is not byte-exact:\n--- first ---\n%s\n--- second ---\n%s", body, again)
	}
}

// deepCopy must not alias the new Templates map or Conventions slice: a
// rejected edit's copy would otherwise mutate the receiver through the
// shared backing store — exactly the failure mode deepCopy's own
// maintenance note warns about when a field is missed.
func TestDeepCopyDoesNotAliasTemplatesOrConventions(t *testing.T) {
	b := templatedBlueprint(func(*Blueprint) {})
	want := templatedBlueprint(func(*Blueprint) {})

	cp := b.deepCopy()
	cp.Spec.Templates["cf.name"] = "changed"
	cp.Spec.Conventions[0].Match = "changed"

	if diff := cmp.Diff(want, b); diff != "" {
		t.Errorf("mutating a deep copy changed the original (-want +got):\n%s", diff)
	}
}
