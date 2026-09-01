package blueprint

import (
	"strings"
	"testing"
)

// pipelined returns the shared valid fixture plus a syntactically valid
// spec.pipeline, which each test then breaks in exactly one way.
func pipelined(steps ...PipelineStep) string {
	var sb strings.Builder
	sb.WriteString(valid)
	sb.WriteString("  pipeline:\n")
	for _, s := range steps {
		sb.WriteString("    - name: " + s.Name + "\n")
		sb.WriteString("      functionRef: " + s.FunctionRef + "\n")
		sb.WriteString("      package: " + s.Package + "\n")
		if s.Position != "" {
			sb.WriteString("      position: " + s.Position + "\n")
		}
		if s.Input != "" {
			sb.WriteString("      input: |\n")
			for _, line := range strings.Split(strings.TrimRight(s.Input, "\n"), "\n") {
				sb.WriteString("        " + line + "\n")
			}
		}
	}
	return sb.String()
}

func autoReadyStep() PipelineStep {
	return PipelineStep{
		Name:        "auto-ready",
		FunctionRef: "function-auto-ready",
		Package:     "xpkg.crossplane.io/crossplane-contrib/function-auto-ready",
	}
}

func TestLoadBlueprintWithPipelineAutoReady(t *testing.T) {
	b, err := Load(write(t, pipelined(autoReadyStep())))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(b.Spec.Pipeline) != 1 {
		t.Fatalf("pipeline = %+v, want one step", b.Spec.Pipeline)
	}
	s := b.Spec.Pipeline[0]
	if s.Name != "auto-ready" || s.FunctionRef != "function-auto-ready" {
		t.Errorf("step = %+v", s)
	}
	if s.Position != "" {
		t.Errorf("position = %q, want empty (defaulted to after by the emitter, not the loader)", s.Position)
	}
}

// TestLoadBlueprintWithPipelineInputKeepsItVerbatim pins the round-trip
// contract: the raw input string is stored exactly as written — no parse,
// re-marshal or trim on load — so a loaded blueprint marshals back
// byte-for-byte. The emitter, not the loader, owns normalisation.
func TestLoadBlueprintWithPipelineInputKeepsItVerbatim(t *testing.T) {
	raw := "kind: Input\napiVersion: fn.example.org/v1beta1\nzeta: first\nalpha: last\n"
	step := PipelineStep{
		Name:        "prep",
		FunctionRef: "function-example",
		Package:     "xpkg.crossplane.io/crossplane-contrib/function-example:v1.0.0",
		Position:    "before",
		Input:       raw,
	}
	b, err := Load(write(t, pipelined(step, autoReadyStep())))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Spec.Pipeline[0].Input; got != raw {
		t.Errorf("input was not kept verbatim:\ngot  %q\nwant %q", got, raw)
	}
	if got := b.Spec.Pipeline[0].Position; got != "before" {
		t.Errorf("position = %q, want before", got)
	}
}

func TestPipelineValidateRejections(t *testing.T) {
	cases := []struct {
		name    string
		steps   []PipelineStep
		wantErr string
	}{
		{
			"missing name",
			[]PipelineStep{{FunctionRef: "function-x", Package: "example.org/fn-x:v1"}},
			"spec.pipeline[0].name is required",
		},
		{
			"name not a DNS label",
			[]PipelineStep{{Name: "Auto_Ready", FunctionRef: "function-x", Package: "example.org/fn-x:v1"}},
			"spec.pipeline[0].name",
		},
		{
			"name is a YAML keyword",
			[]PipelineStep{{Name: "no", FunctionRef: "function-x", Package: "example.org/fn-x:v1"}},
			"spec.pipeline[0].name",
		},
		{
			"name collides with the templating step",
			[]PipelineStep{{Name: TemplatingStepName, FunctionRef: "function-x", Package: "example.org/fn-x:v1"}},
			TemplatingStepName,
		},
		{
			"duplicate step name",
			[]PipelineStep{
				{Name: "twice", FunctionRef: "function-x", Package: "example.org/fn-x:v1"},
				{Name: "twice", FunctionRef: "function-y", Package: "example.org/fn-y:v1"},
			},
			`duplicate step name "twice"`,
		},
		{
			"missing functionRef",
			[]PipelineStep{{Name: "step-a", Package: "example.org/fn-x:v1"}},
			"spec.pipeline[0].functionRef is required",
		},
		{
			"functionRef not a DNS label",
			[]PipelineStep{{Name: "step-a", FunctionRef: "Function.X", Package: "example.org/fn-x:v1"}},
			"spec.pipeline[0].functionRef",
		},
		{
			"functionRef is the built-in templating function",
			[]PipelineStep{{Name: "step-a", FunctionRef: TemplatingFunctionName, Package: "example.org/fn-x:v1"}},
			TemplatingFunctionName,
		},
		{
			"missing package",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x"}},
			"spec.pipeline[0].package is required",
		},
		{
			"package is not an OCI reference",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "not an oci ref"}},
			"spec.pipeline[0].package",
		},
		{
			"same functionRef with two different packages",
			[]PipelineStep{
				{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1"},
				{Name: "step-b", FunctionRef: "function-x", Package: "example.org/fn-x:v2"},
			},
			"different package",
		},
		{
			"unknown position",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1", Position: "around"}},
			"spec.pipeline[0].position",
		},
		{
			"input does not parse",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
				Input: "{broken\n"}},
			"spec.pipeline[0].input",
		},
		{
			"input is a list, not a mapping",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
				Input: "- one\n- two\n"}},
			"spec.pipeline[0].input",
		},
		{
			"input missing apiVersion",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
				Input: "kind: Input\nfoo: bar\n"}},
			"apiVersion",
		},
		{
			"input missing kind",
			[]PipelineStep{{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
				Input: "apiVersion: fn.example.org/v1\nfoo: bar\n"}},
			"kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, pipelined(tc.steps...)))
			if err == nil {
				t.Fatalf("Load accepted an invalid pipeline (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// A control character inside an input scalar VALUE is rejected the same way
// every other user-controlled scalar is (checkScalar), even though the
// emitter re-encodes the parsed input rather than pasting the raw string:
// the blueprint's own rules stay uniform, and the emitted document stays a
// one-line-per-value construct.
func TestPipelineInputScalarWithControlCharacterIsRejected(t *testing.T) {
	// The RAW input string carries no control character — `\t` inside a
	// double-quoted YAML scalar is two plain bytes there — but the PARSED
	// input value does, which is exactly the layer the check must run at.
	step := PipelineStep{
		Name:        "step-a",
		FunctionRef: "function-x",
		Package:     "example.org/fn-x:v1",
		Input:       "apiVersion: fn.example.org/v1\nkind: Input\nvalue: \"a\\tb\"\n",
	}
	_, err := Load(write(t, pipelined(step)))
	if err == nil {
		t.Fatal("Load accepted a control character inside an input scalar value")
	}
	if !strings.Contains(err.Error(), "spec.pipeline[0].input") {
		t.Errorf("err = %v, want it to name spec.pipeline[0].input", err)
	}
}

// Same functionRef twice with the SAME package is legal — two steps may run
// the same function with different inputs — and yields one Function in
// functions.yaml (asserted in internal/emit's tests).
func TestPipelineSameFunctionSamePackageIsAccepted(t *testing.T) {
	steps := []PipelineStep{
		{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
			Input: "apiVersion: fn.example.org/v1\nkind: Input\nmode: a\n"},
		{Name: "step-b", FunctionRef: "function-x", Package: "example.org/fn-x:v1",
			Input: "apiVersion: fn.example.org/v1\nkind: Input\nmode: b\n"},
	}
	if _, err := Load(write(t, pipelined(steps...))); err != nil {
		t.Fatalf("Load: %v — the same function may back two steps when the package agrees", err)
	}
}

func TestPipelinePositionsAreAccepted(t *testing.T) {
	steps := []PipelineStep{
		{Name: "step-a", FunctionRef: "function-x", Package: "example.org/fn-x:v1", Position: "before"},
		{Name: "step-b", FunctionRef: "function-y", Package: "example.org/fn-y:v1", Position: "after"},
		autoReadyStep(), // no position: defaults to after
	}
	if _, err := Load(write(t, pipelined(steps...))); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
