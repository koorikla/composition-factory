package emit_test

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func cf449CRDs(t *testing.T) []schema.CRD {
	t.Helper()
	crdYAML := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.test.aws.upbound.io
spec:
  group: test.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region:
                    type: string
                  tags:
                    type: object
                    additionalProperties:
                      type: string
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdYAML})
	if err != nil {
		t.Fatalf("ParseCRDs failed: %v", err)
	}
	return crds
}

// cf449Blueprint wires an untyped object parameter into a map field that also
// carries an explicit bracket key — the CF-279 shape.
func cf449Blueprint(engine string) *blueprint.Blueprint {
	b := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-tags-drop"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"customTags":   {Type: "object"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"region":            {Value: "us-east-1"},
						"tags":              {From: "params.customTags"},
						"tags[Environment]": {Value: "prod"},
					},
				},
			},
		},
	}
	if engine != "" {
		b.Spec.Emit = &blueprint.Emit{Engine: engine}
	}
	return b
}

// An untyped object parameter wired into a map field must survive on every
// engine, not only go-templating: KCL and Python must merge it alongside the
// explicit bracket keys instead of dropping it.
func TestCF449MapStructuredParamSurvivesOnAllEngines(t *testing.T) {
	crds := cf449CRDs(t)

	t.Run("go-templating renders the parameter", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(""), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, "$spec.customTags") {
			t.Errorf("go-templating dropped customTags, got:\n%s", s)
		}
		if !strings.Contains(s, "Environment") {
			t.Errorf("go-templating dropped the explicit tag, got:\n%s", s)
		}
	})

	t.Run("kcl renders the parameter", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(blueprint.EngineKCL), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, "customTags") {
			t.Errorf("kcl dropped the customTags object parameter, got:\n%s", s)
		}
		if !strings.Contains(s, "Environment") {
			t.Errorf("kcl dropped the explicit tag, got:\n%s", s)
		}
	})

	t.Run("python renders the parameter", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(blueprint.EnginePython), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, "customTags") {
			t.Errorf("python dropped the customTags object parameter, got:\n%s", s)
		}
		if !strings.Contains(s, "Environment") {
			t.Errorf("python dropped the explicit tag, got:\n%s", s)
		}
	})
}

func TestCF449PrecedenceAndExactSyntax(t *testing.T) {
	crds := cf449CRDs(t)

	t.Run("kcl syntax and precedence", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(blueprint.EngineKCL), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		wantUnpack := "**(_spec?.customTags or {})"
		wantExplicit := "Environment = \"prod\""
		idxUnpack := strings.Index(s, wantUnpack)
		idxExplicit := strings.Index(s, wantExplicit)
		if idxUnpack == -1 {
			t.Fatalf("kcl missing unpack %q, got:\n%s", wantUnpack, s)
		}
		if idxExplicit == -1 {
			t.Fatalf("kcl missing explicit entry %q, got:\n%s", wantExplicit, s)
		}
		if idxUnpack > idxExplicit {
			t.Errorf("kcl unpack must precede explicit entry for precedence override; got unpack at %d, explicit at %d", idxUnpack, idxExplicit)
		}
	})

	t.Run("python syntax and precedence", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(blueprint.EnginePython), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		wantUnpack := "**(spec.get(\"customTags\") or {}),"
		wantExplicit := "\"Environment\": \"prod\","
		idxUnpack := strings.Index(s, wantUnpack)
		idxExplicit := strings.Index(s, wantExplicit)
		if idxUnpack == -1 {
			t.Fatalf("python missing unpack %q, got:\n%s", wantUnpack, s)
		}
		if idxExplicit == -1 {
			t.Fatalf("python missing explicit entry %q, got:\n%s", wantExplicit, s)
		}
		if idxUnpack > idxExplicit {
			t.Errorf("python unpack must precede explicit entry for precedence override; got unpack at %d, explicit at %d", idxUnpack, idxExplicit)
		}
	})

	t.Run("go-templating precedence", func(t *testing.T) {
		out, err := emit.Composition(cf449Blueprint(""), crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		wantRange := "range $k, $v := $spec.customTags"
		wantExplicit := "Environment: 'prod'"
		if !strings.Contains(s, wantExplicit) {
			wantExplicit = "Environment: prod"
		}
		idxRange := strings.Index(s, wantRange)
		idxExplicit := strings.Index(s, wantExplicit)
		if idxRange == -1 {
			t.Fatalf("go-templating missing range loop %q, got:\n%s", wantRange, s)
		}
		if idxExplicit == -1 {
			t.Fatalf("go-templating missing explicit entry %q, got:\n%s", wantExplicit, s)
		}
		if idxRange > idxExplicit {
			t.Errorf("go-templating range must precede explicit entry for precedence override; got range at %d, explicit at %d", idxRange, idxExplicit)
		}
	})
}

func TestCF449NestedParamMapStructured(t *testing.T) {
	crds := cf449CRDs(t)

	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-nested-tags"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"config": {
						Type: "object",
						Properties: map[string]blueprint.Parameter{
							"tags": {Type: "object"},
						},
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"region":     {Value: "us-east-1"},
						"tags":       {From: "params.config.tags"},
						"tags[Tier]": {Value: "backend"},
					},
				},
			},
		},
	}

	t.Run("kcl nested param", func(t *testing.T) {
		b := *bp
		b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
		out, err := emit.Composition(&b, crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		want := "**(_spec?.config?.tags or {})"
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in KCL output, got:\n%s", want, s)
		}
	})

	t.Run("python nested param", func(t *testing.T) {
		b := *bp
		b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
		out, err := emit.Composition(&b, crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		want := "**(_get(spec, \"config\", \"tags\") or {}),"
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in Python output, got:\n%s", want, s)
		}
	})
}

func TestCF449AllOptionalSubtreeGuards(t *testing.T) {
	crds := cf449CRDs(t)

	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-optional-tags"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"customTags":   {Type: "object"},
					"env":          {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"region":            {Value: "us-east-1"},
						"tags":              {From: "params.customTags"},
						"tags[Environment]": {From: "params.env"},
					},
				},
			},
		},
	}

	t.Run("kcl all optional wraps in guard", func(t *testing.T) {
		b := *bp
		b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
		out, err := emit.Composition(&b, crds)
		if err != nil {
			t.Fatalf("Composition emit failed: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, "_spec?.customTags != None") {
			t.Errorf("expected customTags guard in KCL output, got:\n%s", s)
		}
		if !strings.Contains(s, "_spec?.env != None") {
			t.Errorf("expected env guard in KCL output, got:\n%s", s)
		}
		if !strings.Contains(s, "if _spec?.customTags != None or _spec?.env != None:") {
			t.Errorf("expected combined guard wrapping tags in KCL output, got:\n%s", s)
		}
	})
}
