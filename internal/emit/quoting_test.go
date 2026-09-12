package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"sigs.k8s.io/yaml"
)

// quotingTestCRDs provides native k8s CRDs (ConfigMap) plus wireCRDs (Queue with status schema).
func quotingTestCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	return append(native, wireCRDs(t)...)
}

// CF-074: Verify that status wires into string fields (such as ConfigMap data.*
// or schema fields with target type string) are properly quoted / cast across
// Go-template, KCL, and Python engines, so string status values that resemble
// booleans ("true") or numbers ("8080") do not change type.
func TestStatusWireIntoStringFieldQuotingAcrossEngines(t *testing.T) {
	crds := quotingTestCRDs(t)

	makeBP := func(engine string) *blueprint.Blueprint {
		b := testBlueprint()
		b.Spec.Emit = &blueprint.Emit{Engine: engine}
		b.Spec.Resources = []blueprint.Resource{
			{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
				},
			},
			{
				Name: "cm", Kind: "ConfigMap", Provider: blueprint.NativeProvider,
				Fields: map[string]blueprint.Field{
					"data[endpoint]": {From: "resources.main-queue.status.atProvider.url"},
				},
			},
		}
		return b
	}

	// 1. Go-template: must emit | quote and preserve string type when rendered with "true"
	t.Run("go-template", func(t *testing.T) {
		b := makeBP(blueprint.EngineGoTemplating)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(go-template): %v", err)
		}
		tmplBody := extractTemplate(t, out)
		if !strings.Contains(tmplBody, `.status.atProvider.url | quote }}`) {
			t.Errorf("expected status wire to be piped through quote in template:\n%s", tmplBody)
		}

		// Render with observed value "true" (a string that looks like a bool, cf-002 repro)
		observed := map[string]any{
			"main-queue": map[string]any{
				"resource": map[string]any{
					"status": map[string]any{
						"atProvider": map[string]any{
							"url": "true",
						},
					},
				},
			},
		}
		rendered, err := renderTemplateObserved(t, tmplBody, map[string]any{"providerName": "test"}, observed)
		if err != nil {
			t.Fatalf("renderTemplateObserved: %v", err)
		}

		var cmDoc map[string]any
		for _, docText := range strings.Split(rendered, "\n---\n") {
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
				continue
			}
			if doc["kind"] == "ConfigMap" {
				cmDoc = doc
				break
			}
		}
		if cmDoc == nil {
			t.Fatalf("ConfigMap document not found in rendered output:\n%s", rendered)
		}
		data, ok := cmDoc["data"].(map[string]any)
		if !ok {
			t.Fatalf("data is not a map: %T (%v)", cmDoc["data"], cmDoc["data"])
		}
		val, exists := data["endpoint"]
		if !exists {
			t.Fatalf("endpoint not found in data: %v", data)
		}
		if sVal, ok := val.(string); !ok || sVal != "true" {
			t.Errorf("endpoint = %v (%T), want string \"true\" (not bool)", val, val)
		}
	})

	// 2. KCL: must emit str(ocds?...)
	t.Run("kcl", func(t *testing.T) {
		b := makeBP(blueprint.EngineKCL)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(kcl): %v", err)
		}
		s := string(out)
		wantCast := `str(ocds?["main-queue"]?.Resource?.status?.atProvider?.url)`
		if !strings.Contains(s, wantCast) {
			t.Errorf("KCL expected status wire cast with %q, got:\n%s", wantCast, s)
		}
	})

	// 3. Python: must emit str(ocds.get(...))
	t.Run("python", func(t *testing.T) {
		b := makeBP(blueprint.EnginePython)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(python): %v", err)
		}
		s := string(out)
		wantCast := `str(ocds.get("main-queue", {}).get("resource", {}).get("status", {}).get("atProvider", {}).get("url"))`
		if !strings.Contains(s, wantCast) {
			t.Errorf("Python expected status wire cast with %q, got:\n%s", wantCast, s)
		}
	})
}

// CF-080: Verify that integer parameters wired into string map fields
// (such as ConfigMap data[PORT]: {from: params.port}) render as strings
// across Go-template, KCL, and Python engines.
func TestIntegerParamIntoStringMapAcrossEngines(t *testing.T) {
	crds := quotingTestCRDs(t)

	makeBP := func(engine string) *blueprint.Blueprint {
		b := testBlueprint()
		b.Spec.Emit = &blueprint.Emit{Engine: engine}
		b.Spec.XRD.Parameters = map[string]blueprint.Parameter{
			"providerName": {Type: "string", Required: true},
			"port":         {Type: "integer", Default: "8080"},
		}
		b.Spec.Resources = []blueprint.Resource{
			{
				Name: "config", Kind: "ConfigMap", Provider: blueprint.NativeProvider,
				Fields: map[string]blueprint.Field{
					"data[PORT]": {From: "params.port"},
				},
			},
		}
		return b
	}

	// 1. Go-template: emits PORT: {{ $spec.port | quote }} and renders as string "8080"
	t.Run("go-template", func(t *testing.T) {
		b := makeBP(blueprint.EngineGoTemplating)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(go-template): %v", err)
		}
		tmplBody := extractTemplate(t, out)
		if !strings.Contains(tmplBody, `{{ $spec.port | quote }}`) {
			t.Errorf("Go-template expected $spec.port | quote, got:\n%s", tmplBody)
		}

		rendered, err := renderTemplateObserved(t, tmplBody, map[string]any{
			"providerName": "test",
			"port":         8080,
		}, nil)
		if err != nil {
			t.Fatalf("renderTemplateObserved: %v", err)
		}

		var cmDoc map[string]any
		for _, docText := range strings.Split(rendered, "\n---\n") {
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
				continue
			}
			if doc["kind"] == "ConfigMap" {
				cmDoc = doc
				break
			}
		}
		if cmDoc == nil {
			t.Fatalf("ConfigMap document not found in rendered output:\n%s", rendered)
		}
		data, ok := cmDoc["data"].(map[string]any)
		if !ok {
			t.Fatalf("data is not a map: %T (%v)", cmDoc["data"], cmDoc["data"])
		}
		val, exists := data["PORT"]
		if !exists {
			t.Fatalf("PORT not found in data: %v", data)
		}
		if sVal, ok := val.(string); !ok || sVal != "8080" {
			t.Errorf("PORT = %v (%T), want string \"8080\" (not number)", val, val)
		}
	})

	// 2. KCL: emits "PORT" = str(_spec?.port) or PORT = str(_spec?.port)
	t.Run("kcl", func(t *testing.T) {
		b := makeBP(blueprint.EngineKCL)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(kcl): %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `str(_spec?.port)`) {
			t.Errorf("KCL expected str(_spec?.port), got:\n%s", s)
		}
	})

	// 3. Python: emits "PORT": _str(spec.get("port"))
	t.Run("python", func(t *testing.T) {
		b := makeBP(blueprint.EnginePython)
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition(python): %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `"PORT": _str(spec.get("port"))`) {
			t.Errorf("Python expected \"PORT\": _str(spec.get(\"port\")), got:\n%s", s)
		}
	})
}

// CF-080: Specifically verify that the shipped k8s-workload starter example
// renders PORT in ConfigMap as a string across all three engines.
func TestK8sWorkloadConfigMapPortQuotedAcrossEngines(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	loadWorkload := func(engine string) *blueprint.Blueprint {
		b, err := blueprint.Load("../examples/k8s-workload.cf.yaml")
		if err != nil {
			t.Fatalf("Load(k8s-workload.cf.yaml): %v", err)
		}
		b.Spec.Emit = &blueprint.Emit{Engine: engine}
		return b
	}

	t.Run("go-template", func(t *testing.T) {
		b := loadWorkload(blueprint.EngineGoTemplating)
		out, err := Composition(b, native)
		if err != nil {
			t.Fatalf("Composition(go-template): %v", err)
		}
		tmpl := extractTemplate(t, out)
		if !strings.Contains(tmpl, `PORT: {{ $spec.port | quote }}`) {
			t.Errorf("Go-template expected PORT: {{ $spec.port | quote }}, got:\n%s", tmpl)
		}
	})

	t.Run("kcl", func(t *testing.T) {
		b := loadWorkload(blueprint.EngineKCL)
		out, err := Composition(b, native)
		if err != nil {
			t.Fatalf("Composition(kcl): %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `str(_spec?.port)`) {
			t.Errorf("KCL expected str(_spec?.port), got:\n%s", s)
		}
	})

	t.Run("python", func(t *testing.T) {
		b := loadWorkload(blueprint.EnginePython)
		out, err := Composition(b, native)
		if err != nil {
			t.Fatalf("Composition(python): %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `"PORT": _str(spec.get("port"))`) {
			t.Errorf("Python expected \"PORT\": _str(spec.get(\"port\")), got:\n%s", s)
		}
	})
}

func TestCF114PythonEngineIntegerParamStringFormatting(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	b, err := blueprint.Load("../examples/k8s-workload.cf.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}

	out, err := Composition(b, native)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)

	// An integer parameter wired into a string target must not be converted via plain str(val)
	// because MessageToDict delivers numbers as floats (8080.0 -> "8080.0").
	if strings.Contains(s, `"PORT": str(spec.get("port"))`) {
		t.Errorf("Python engine emitted uncoerced str(spec.get(\"port\")), which formats float as 8080.0:\n%s", s)
	}
	if !strings.Contains(s, `_str(spec.get("port"))`) && !strings.Contains(s, `str(int(spec.get("port")))`) {
		t.Errorf("Python engine expected integer-safe string conversion for port, got:\n%s", s)
	}
}

// CF-344: Verify formatYAMLKey quotes YAML keywords and special characters.
func TestFormatYAMLKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"on", "'on'"},
		{"off", "'off'"},
		{"yes", "'yes'"},
		{"no", "'no'"},
		{"true", "'true'"},
		{"false", "'false'"},
		{"null", "'null'"},
		{"~", "'~'"},
		{"y", "'y'"},
		{"n", "'n'"},
		{"ON", "'ON'"},
		{"Off", "'Off'"},
		{"YES", "'YES'"},
		{"No", "'No'"},
		{"True", "'True'"},
		{"FALSE", "'FALSE'"},
		{"Null", "'Null'"},
		{"Y", "'Y'"},
		{"N", "'N'"},
		{"providerName", "providerName"},
		{"myParam", "myParam"},
		{"replicaCount", "replicaCount"},
		{"foo/bar", "'foo/bar'"},
		{"-leadingDash", "'-leadingDash'"},
		{"has:colon", "'has:colon'"},
		{"", "''"},
	}

	for _, tc := range tests {
		got := formatYAMLKey(tc.input)
		if got != tc.want {
			t.Errorf("formatYAMLKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
