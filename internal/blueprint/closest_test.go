package blueprint

import (
	"testing"
)

func TestClosestPathShortIdentifiers(t *testing.T) {
	tests := []struct {
		target     string
		candidates []string
		want       string
	}{
		{
			target:     "spce",
			candidates: []string{"spec", "status", "metadata"},
			want:       "spec",
		},
		{
			target:     "teir",
			candidates: []string{"tier", "region", "zone"},
			want:       "tier",
		},
		{
			target:     "spc",
			candidates: []string{"spec", "status", "metadata"},
			want:       "spec",
		},
		{
			target:     "evn",
			candidates: []string{"env", "region", "account"},
			want:       "env",
		},
		{
			target:     "regino",
			candidates: []string{"region", "tier", "account"},
			want:       "region",
		},
		{
			target:     "spec.selector.machLabels",
			candidates: []string{"spec.selector.matchLabels", "spec.template"},
			want:       "spec.selector.matchLabels",
		},
		{
			target:     "completelydifferent",
			candidates: []string{"spec", "status"},
			want:       "",
		},
	}

	for _, tt := range tests {
		got := ClosestPath(tt.target, tt.candidates)
		if got != tt.want {
			t.Errorf("ClosestPath(%q, %v) = %q, want %q", tt.target, tt.candidates, got, tt.want)
		}
	}
}
