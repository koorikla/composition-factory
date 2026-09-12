package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

func cf432Blueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-env-defaults"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"enabled": {Type: "boolean", Default: "true"},
				"retries": {Type: "integer", Default: "3"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "main-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					When:     "env.enabled",
					Fields: map[string]blueprint.Field{
						"maxMessageSize": {From: "env.retries"},
					},
				},
			},
		},
	}
}

// cf432Template returns the Go-template body from whichever pipeline step
// carries one. A blueprint with an environment block gets an
// environment-configs step injected ahead of the go-templating step, so the
// body is not always at pipeline[0].
func cf432Template(t *testing.T, doc []byte) string {
	t.Helper()
	var parsed struct {
		Spec struct {
			Pipeline []struct {
				Input struct {
					Inline struct {
						Template string `json:"template"`
					} `json:"inline"`
				} `json:"input"`
			} `json:"pipeline"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("emitted document is not valid YAML: %v\n---\n%s", err, doc)
	}
	for _, step := range parsed.Spec.Pipeline {
		if step.Input.Inline.Template != "" {
			return step.Input.Inline.Template
		}
	}
	t.Fatalf("no pipeline step carries an inline template\n---\n%s", doc)
	return ""
}

// cf432Render executes the emitted template body with an environment context,
// the way function-go-templating does: $env comes from
// .context["apiextensions.crossplane.io/environment"].
func cf432Render(t *testing.T, tmplBody string, env map[string]any) (string, error) {
	t.Helper()
	data := map[string]any{
		"observed": map[string]any{
			"composite": map[string]any{
				"resource": map[string]any{
					"metadata": map[string]any{"name": "my-xapp"},
					"spec": map[string]any{
						"providerName": "aws-provider",
					},
				},
			},
		},
		"context": map[string]any{
			"apiextensions.crossplane.io/environment": env,
		},
	}
	return renderTemplateData(t, tmplBody, data)
}

// An environment key that declares a default must still honour an explicit
// zero value supplied by the EnvironmentConfig: boolean false must disable a
// `when` gate, and integer 0 must reach the field as 0.
func TestCF432EnvDefaultPreservesExplicitZeroValues(t *testing.T) {
	got, err := Composition(cf432Blueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := cf432Template(t, got)

	t.Run("explicit false disables the when gate", func(t *testing.T) {
		rendered, err := cf432Render(t, tmplBody, map[string]any{
			"enabled": false,
			"retries": 0,
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		if strings.Contains(rendered, "kind: Queue") {
			t.Errorf("env.enabled is explicitly false but the guarded resource was rendered\n---\n%s", rendered)
		}
	})

	t.Run("explicit zero reaches the field", func(t *testing.T) {
		rendered, err := cf432Render(t, tmplBody, map[string]any{
			"enabled": true,
			"retries": 0,
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		if !strings.Contains(rendered, "maxMessageSize: 0") {
			t.Errorf("env.retries is explicitly 0; want maxMessageSize: 0\n---\n%s", rendered)
		}
	})

	t.Run("absent keys still fall back to the declared defaults", func(t *testing.T) {
		rendered, err := cf432Render(t, tmplBody, map[string]any{})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		if !strings.Contains(rendered, "kind: Queue") {
			t.Errorf("env.enabled absent; default true must render the resource\n---\n%s", rendered)
		}
		if !strings.Contains(rendered, "maxMessageSize: 3") {
			t.Errorf("env.retries absent; default 3 must reach the field\n---\n%s", rendered)
		}
	})
}
