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

func TestValidateXRDRejectsNameOver63Chars(t *testing.T) {
	x := XRD{
		Group:   "verylongdomainname.infrastructure.platform.sparky.ee",
		Kind:    "XQueue",
		Plural:  "xmessagequeues",
		Version: "v1alpha1",
		Scope:   "Namespaced",
	}
	// len("xmessagequeues.verylongdomainname.infrastructure.platform.sparky.ee") = 67
	if err := validateXRD(x); err == nil {
		t.Fatalf("validateXRD() succeeded, want error refusing name > 63 characters")
	}
}

func TestValidateXRDNameLengthBoundary(t *testing.T) {
	// Base XRD with plural "xqueues" (7 chars) + "." (1 char) = 8 chars.
	// 63 - 8 = 55 chars needed for group to make total name exactly 63 chars.
	group55 := "a." + strings.Repeat("b", 53)
	x63 := XRD{
		Group:   group55,
		Kind:    "XQueue",
		Plural:  "xqueues",
		Version: "v1alpha1",
		Scope:   "Namespaced",
	}
	if gotLen := len(x63.Plural + "." + x63.Group); gotLen != 63 {
		t.Fatalf("test setup error: want name length 63, got %d", gotLen)
	}
	if err := validateXRD(x63); err != nil {
		t.Fatalf("validateXRD() failed for 63-char name: %v", err)
	}

	// 64-char name: append one char to group
	x64 := x63
	x64.Group = "a." + strings.Repeat("b", 54)
	if gotLen := len(x64.Plural + "." + x64.Group); gotLen != 64 {
		t.Fatalf("test setup error: want name length 64, got %d", gotLen)
	}
	err := validateXRD(x64)
	if err == nil {
		t.Fatalf("validateXRD() succeeded for 64-char name, want error")
	}
	wantSubstr := "must be at most 63 characters; Crossplane labels CompositionRevisions"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}
