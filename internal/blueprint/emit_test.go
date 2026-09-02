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
