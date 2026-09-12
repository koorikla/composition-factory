package catalogue

import "testing"

func TestSearchRejectsInvalidType(t *testing.T) {
	entries, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, invalid := range []string{"bogus", "providers", "functions", "invalid"} {
		results := Search(entries, "", invalid)
		if len(results) != 0 {
			t.Errorf("Search(entries, %q, %q) returned %d results, want 0", "", invalid, len(results))
		}
	}
}

func TestSearchValidTypes(t *testing.T) {
	entries, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	all := Search(entries, "", "")
	if len(all) != len(entries) {
		t.Errorf("Search(entries, \"\", \"\") = %d, want %d", len(all), len(entries))
	}
	providers := Search(entries, "", "provider")
	if len(providers) == 0 || len(providers) >= len(entries) {
		t.Errorf("Search(entries, \"\", \"provider\") returned unexpected count: %d", len(providers))
	}
	functions := Search(entries, "", "function")
	if len(functions) == 0 || len(functions) >= len(entries) {
		t.Errorf("Search(entries, \"\", \"function\") returned unexpected count: %d", len(functions))
	}
	if len(providers)+len(functions) != len(entries) {
		t.Errorf("providers (%d) + functions (%d) != total (%d)", len(providers), len(functions), len(entries))
	}
}
