package blueprint

import (
	"strings"
	"testing"
)

func TestValidateRejectsYamlKeywordResourceName(t *testing.T) {
	keywords := []string{"true", "false", "yes", "no", "on", "off", "null", "y", "n"}
	for _, kw := range keywords {
		body := strings.Replace(valid, "name: main-queue", `name: "`+kw+`"`, 1)
		_, err := Load(write(t, body))
		if err == nil {
			t.Fatalf("expected Validate to reject resource name %q, got nil", kw)
		}
		if !strings.Contains(err.Error(), "invalid resource name") {
			t.Errorf("expected error mentioning invalid resource name for %q, got: %v", kw, err)
		}
	}
}

func TestValidateRejectsControlCharacterInResourceName(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.Spec.Resources[0].Name = "main\nqueue"
	err = b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("err = %v, want the checkScalar control-character rejection", err)
	}
	if !strings.Contains(err.Error(), "spec.resources[0].name") {
		t.Errorf("err = %v, want it to name spec.resources[0].name", err)
	}
}
