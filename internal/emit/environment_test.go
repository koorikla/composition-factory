package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func TestEnvironmentEmission_GoTemplating(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
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
				"region":          {Type: "string", Default: "us-east-1"},
				"auditEnabled":    {Type: "boolean"},
				"instanceCount":   {Type: "integer"},
				"environmentTier": {Type: "string"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "main-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					When:     `env.environmentTier == "prod"`,
					ForEach:  "env.instanceCount",
					Fields: map[string]blueprint.Field{
						"region": {From: "env.region"},
					},
					Annotations: map[string]blueprint.Field{
						"example.org/tier": {From: "env.environmentTier"},
					},
					Envelope: map[string]blueprint.Field{
						"providerConfigRef.name": {From: "env.region"},
					},
				},
			},
		},
	}

	// 1. Check Composition emission
	compBytes, err := Composition(bp, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition() failed: %v", err)
	}
	compStr := string(compBytes)

	// Environment keys annotation on metadata
	if !strings.Contains(compStr, blueprint.EnvironmentKeysAnnotation) {
		t.Errorf("expected %s in Composition metadata annotations", blueprint.EnvironmentKeysAnnotation)
	}

	// Auto-injected function-environment-configs step
	if !strings.Contains(compStr, "- step: environment-configs") {
		t.Errorf("expected auto-injected environment-configs step in composition")
	}
	if !strings.Contains(compStr, "name: function-environment-configs") {
		t.Errorf("expected functionRef function-environment-configs in composition")
	}

	// Preamble env dict extraction
	if !strings.Contains(compStr, `{{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}`) {
		t.Errorf("expected $env definition in template preamble")
	}

	// When condition
	if !strings.Contains(compStr, `{{- if and (hasKey $env "environmentTier") (eq $env.environmentTier "prod") }}`) {
		t.Errorf("expected guarded when condition for env.environmentTier == prod, got:\n%s", compStr)
	}

	// ForEach loop
	if !strings.Contains(compStr, `{{- if hasKey $env "instanceCount" }}`) {
		t.Errorf("expected hasKey guard for forEach env.instanceCount")
	}
	if !strings.Contains(compStr, `{{- range $i := until (int $env.instanceCount) }}`) {
		t.Errorf("expected until loop for forEach env.instanceCount")
	}

	// Field guard and value
	if !strings.Contains(compStr, `{{- if hasKey $env "region" }}`) {
		t.Errorf("expected hasKey guard for field region")
	}
	if !strings.Contains(compStr, `region: {{ $env.region | quote }}`) {
		t.Errorf("expected quoted env wire for field region")
	}

	// 2. Check functions.yaml emission
	fnBytes, err := Functions(bp)
	if err != nil {
		t.Fatalf("Functions() failed: %v", err)
	}
	fnStr := string(fnBytes)
	if !strings.Contains(fnStr, "name: function-environment-configs") {
		t.Errorf("expected function-environment-configs in functions.yaml, got:\n%s", fnStr)
	}
	if !strings.Contains(fnStr, "package: "+blueprint.EnvironmentConfigsFunctionPackage) {
		t.Errorf("expected package %s in functions.yaml", blueprint.EnvironmentConfigsFunctionPackage)
	}
}

func TestEnvironmentEmission_Python(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
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
				"region":  {Type: "string"},
				"enabled": {Type: "boolean"},
				"count":   {Type: "integer"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					When:     "env.enabled",
					ForEach:  "env.count",
					Fields: map[string]blueprint.Field{
						"region": {From: "env.region"},
					},
				},
			},
		},
	}

	body, err := pythonTemplateBody(bp, testCRDs(t))
	if err != nil {
		t.Fatalf("pythonTemplateBody failed: %v", err)
	}

	if !strings.Contains(body, `ctx = MessageToDict(req.context)`) {
		t.Errorf("expected req.context extraction in python body")
	}
	if !strings.Contains(body, `env = ctx.get("apiextensions.crossplane.io/environment", {})`) {
		t.Errorf("expected env extraction in python body")
	}
	if !strings.Contains(body, `if bool(env.get("enabled")):`) {
		t.Errorf("expected env.get(enabled) condition in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `for _i in range(int(env.get("count", 0))):`) {
		t.Errorf("expected range on env.get(count) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `"region": env.get("region")`) {
		t.Errorf("expected env.get(region) for field region in python body, got:\n%s", body)
	}
}

func TestEnvironmentEmission_KCLRefusal(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
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
				"region": {Type: "string"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					Fields: map[string]blueprint.Field{
						"region": {From: "env.region"},
					},
				},
			},
		},
	}

	_, err := kclTemplateBody(bp, testCRDs(t))
	if err == nil {
		t.Fatal("expected error for KCL engine with spec.environment, got nil")
	}
	if !strings.Contains(err.Error(), "does not support spec.environment") {
		t.Errorf("err = %q, want containing does not support spec.environment", err.Error())
	}
}
