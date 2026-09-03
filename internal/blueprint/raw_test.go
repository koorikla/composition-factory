package blueprint

import (
	"testing"
)

func TestIsBareGoTemplateExpr(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`printf "%s-subnet-%d" $xr $i`, true},
		{`printf "10.0.%d.0/24" $i`, true},
		{`$spec.region`, true},
		{`$xr`, true},
		{`$xrMeta.namespace`, true},
		{`$observed.resources`, true},
		{`$env.region`, true},
		{`$i`, true},
		{`quote $spec.name`, true},
		{`int $spec.count`, true},
		{`default "fallback" $spec.name`, true},
		{`{{ printf "%s" $xr }}`, false},
		{`{{ $spec.region }}`, false},
		{`{app: web}`, false},
		{`[ReadWriteOnce]`, false},
		{`"us-east-1"`, false},
		{`80`, false},
		{`true`, false},
		{``, false},
	}

	for _, tt := range tests {
		got := IsBareGoTemplateExpr(tt.input)
		if got != tt.want {
			t.Errorf("IsBareGoTemplateExpr(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeRawGoTemplate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`printf "%s-subnet-%d" $xr $i`, `{{ printf "%s-subnet-%d" $xr $i }}`},
		{`$spec.region`, `{{ $spec.region }}`},
		{`{{ $spec.region }}`, `{{ $spec.region }}`},
		{`{app: web}`, `{app: web}`},
		{`[ReadWriteOnce]`, `[ReadWriteOnce]`},
		{`"hello"`, `"hello"`},
	}

	for _, tt := range tests {
		got := NormalizeRawGoTemplate(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeRawGoTemplate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
