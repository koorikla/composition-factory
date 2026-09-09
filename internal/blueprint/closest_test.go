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

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "b", 1},
		{"same", "same", 0},
		{"spce", "spec", 2},
		{"kitten", "sitting", 3},
		{"spec.selector.machLabels", "spec.selector.matchLabels", 1},
		{"hello world", "hello world", 0},
		{"café", "cafe", 1},
		{"日本語", "日本", 1},
		{
			"a-very-long-field-name-that-exceeds-the-stack-buffer-size-of-sixty-four-characters-alpha",
			"a-very-long-field-name-that-exceeds-the-stack-buffer-size-of-sixty-four-characters-beta",
			4, // "alpha" vs "beta" distance: 'a'->'b', 'l'->'e', 'p'->'t', 'h' deleted, 'a' matches = 4
		},
	}
	for _, tt := range tests {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		// Symmetrical check
		if got := editDistance(tt.b, tt.a); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.b, tt.a, got, tt.want)
		}
	}
}

func BenchmarkEditDistance(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		editDistance("spec.selector.machLabels", "spec.selector.matchLabels")
		editDistance("spce", "spec")
		editDistance("completelydifferent", "spec")
	}
}
