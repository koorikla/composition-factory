package blueprint

import (
	"testing"
)

func TestValidateParametersYAMLKeywordResilience(t *testing.T) {
	// "n", "y", "yes", "no" must be accepted as parameter names under YAML 1.2 semantics
	for _, name := range []string{"n", "y", "yes", "no"} {
		t.Run(name, func(t *testing.T) {
			bp := validParamBlueprint(name)
			if err := bp.validateParameters(); err != nil {
				t.Fatalf("validateParameters rejected valid parameter name %q: %v", name, err)
			}
		})
	}

	// "true", "false", "null" must be rejected
	for _, name := range []string{"true", "false", "null"} {
		t.Run(name, func(t *testing.T) {
			bp := validParamBlueprint(name)
			if err := bp.validateParameters(); err == nil {
				t.Fatalf("validateParameters accepted boolean/null keyword %q as parameter name", name)
			}
		})
	}
}

func TestValidateObjectParameterMembersYAMLKeywordResilience(t *testing.T) {
	for _, name := range []string{"n", "y", "yes", "no", "on", "off"} {
		t.Run(name, func(t *testing.T) {
			bp := validParamBlueprint("settings")
			bp.Spec.XRD.Parameters["settings"] = Parameter{
				Type: "object",
				Properties: map[string]Parameter{
					name: {Type: "string"},
				},
			}
			if err := bp.Validate(); err != nil {
				t.Fatalf("Validate rejected valid object member name %q: %v", name, err)
			}
		})
	}
}

func TestValidateObjectParameterMembersReservedKeywordsRejected(t *testing.T) {
	for _, name := range []string{"true", "false", "null"} {
		t.Run(name, func(t *testing.T) {
			bp := validParamBlueprint("settings")
			bp.Spec.XRD.Parameters["settings"] = Parameter{
				Type: "object",
				Properties: map[string]Parameter{
					name: {Type: "string"},
				},
			}
			if err := bp.Validate(); err == nil {
				t.Fatalf("Validate accepted boolean/null keyword %q as object member name", name)
			}
		})
	}
}
