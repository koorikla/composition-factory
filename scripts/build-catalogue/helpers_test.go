package main

import (
	"os"
	"testing"
)

// writeFile is the one place every test in this package writes a fixture
// file to disk, so a failure to write always fails loudly via t.Fatalf
// rather than surfacing later as a confusing "file not found" from whatever
// tries to read it.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// readFile is writeFile's read-side counterpart.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
