package blueprint

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// withEmit appends a spec.emit block to the shared valid fixture.
func withEmit(templateSource string) string {
	return valid + "  emit:\n    templateSource: " + templateSource + "\n"
}

func TestLoadBlueprintEmitFileSystem(t *testing.T) {
	b, err := Load(write(t, withEmit("FileSystem")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Spec.Emit == nil || b.Spec.Emit.TemplateSource != TemplateSourceFileSystem {
		t.Fatalf("emit = %+v, want templateSource FileSystem", b.Spec.Emit)
	}
	if got := b.TemplateSource(); got != TemplateSourceFileSystem {
		t.Errorf("TemplateSource() = %q, want FileSystem", got)
	}
}

func TestLoadBlueprintEmitInlineIsExplicitDefault(t *testing.T) {
	b, err := Load(write(t, withEmit("Inline")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.TemplateSource(); got != TemplateSourceInline {
		t.Errorf("TemplateSource() = %q, want Inline", got)
	}
}

// TestEmitAbsentIsInlineAndStaysAbsent pins two halves of one contract: a
// blueprint that never declared spec.emit renders inline (today's output,
// byte for byte), and persisting it back — the HTTP API re-marshals the
// whole document on every edit — must not grow a literal `emit:` key.
func TestEmitAbsentIsInlineAndStaysAbsent(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.TemplateSource(); got != TemplateSourceInline {
		t.Errorf("TemplateSource() = %q, want Inline when spec.emit is absent", got)
	}
	out, err := yaml.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "emit:") {
		t.Errorf("an absent spec.emit must marshal back absent, got:\n%s", out)
	}
}

func TestValidateRejectsUnknownTemplateSource(t *testing.T) {
	_, err := Load(write(t, withEmit("Environment")))
	if err == nil {
		t.Fatal("expected an error for templateSource: Environment")
	}
	for _, want := range []string{"spec.emit.templateSource", "Environment", "Inline", "FileSystem"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestLoadBlueprintEmitEngineKCL(t *testing.T) {
	doc := valid + "  emit:\n    engine: kcl\n"
	b, err := Load(write(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Engine(); got != EngineKCL {
		t.Errorf("Engine() = %q, want %q", got, EngineKCL)
	}
}

func TestLoadBlueprintEmitEngineDefaultGoTemplating(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Engine(); got != EngineGoTemplating {
		t.Errorf("Engine() = %q, want %q", got, EngineGoTemplating)
	}
}

func TestLoadBlueprintEmitEnginePython(t *testing.T) {
	doc := valid + "  emit:\n    engine: python\n"
	b, err := Load(write(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Engine(); got != EnginePython {
		t.Errorf("Engine() = %q, want %q", got, EnginePython)
	}
}

func TestValidateRejectsUnknownEngine(t *testing.T) {
	doc := valid + "  emit:\n    engine: ruby\n"
	_, err := Load(write(t, doc))
	if err == nil {
		t.Fatal("expected an error for engine: ruby")
	}
	for _, want := range []string{"spec.emit.engine", "ruby", "go-templating", "kcl", "python"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestValidateRejectsFileSystemWithKCLEngine(t *testing.T) {
	doc := valid + "  emit:\n    templateSource: FileSystem\n    engine: kcl\n"
	_, err := Load(write(t, doc))
	if err == nil {
		t.Fatal("expected an error for templateSource: FileSystem with engine: kcl")
	}
	for _, want := range []string{"spec.emit.templateSource", "FileSystem", "go-templating", "kcl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestValidateRejectsFileSystemWithPythonEngine(t *testing.T) {
	doc := valid + "  emit:\n    templateSource: FileSystem\n    engine: python\n"
	_, err := Load(write(t, doc))
	if err == nil {
		t.Fatal("expected an error for templateSource: FileSystem with engine: python")
	}
	for _, want := range []string{"spec.emit.templateSource", "FileSystem", "go-templating", "python"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestValidateRejectsGoTemplateFeaturesWithNonGoEngines(t *testing.T) {
	baseNoRes := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
`
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "kcl with templates",
			yaml:    valid + "  emit:\n    engine: kcl\n  templates:\n    cf.tag: \"value\"\n",
			wantErr: `spec.templates: engine "kcl" does not support template: blocks`,
		},
		{
			name:    "python with conventions",
			yaml:    valid + "  emit:\n    engine: python\n  templates:\n    cf.tag: \"value\"\n  conventions:\n    - match: tags\n      template: cf.tag\n",
			wantErr: `spec.conventions: engine "python" does not support template: conventions`,
		},
		{
			name:    "kcl with environment",
			yaml:    valid + "  emit:\n    engine: kcl\n  environment:\n    vpcId:\n      type: string\n",
			wantErr: `spec.environment: engine "kcl" does not support spec.environment`,
		},
		{
			name:    "python with field template",
			yaml:    baseNoRes + "  emit:\n    engine: python\n  resources:\n    - name: queue\n      kind: Queue\n      fields:\n        region:\n          template: cf.tag\n",
			wantErr: `engine "python" does not support template: fields`,
		},
		{
			name:    "kcl with annotation template",
			yaml:    baseNoRes + "  emit:\n    engine: kcl\n  resources:\n    - name: queue\n      kind: Queue\n      annotations:\n        example.com/tag:\n          template: cf.tag\n",
			wantErr: `engine "kcl" does not support template: fields`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(write(t, tt.yaml))
			if err == nil {
				t.Fatalf("Load() succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}
