package blueprint

import (
	"strings"
	"testing"
)

func TestValidateXRDDirect(t *testing.T) {
	valid := XRD{
		Group:   "platform.sparky.ee",
		Kind:    "XQueue",
		Plural:  "xqueues",
		Version: "v1alpha1",
		Scope:   "Namespaced",
	}

	if err := validateXRD(valid); err != nil {
		t.Fatalf("valid XRD failed validateXRD: %v", err)
	}

	missingGroup := valid
	missingGroup.Group = ""
	if err := validateXRD(missingGroup); err == nil || !strings.Contains(err.Error(), "spec.xrd.group is required") {
		t.Errorf("missing group err = %v", err)
	}

	missingMulti := valid
	missingMulti.Kind = ""
	missingMulti.Plural = ""
	if err := validateXRD(missingMulti); err == nil || !strings.Contains(err.Error(), "spec.xrd needs") {
		t.Errorf("missing multi err = %v", err)
	}

	invalidScope := valid
	invalidScope.Scope = "Cluster"
	if err := validateXRD(invalidScope); err == nil || !strings.Contains(err.Error(), "Cluster is not supported in M1") {
		t.Errorf("cluster scope err = %v", err)
	}
}
