package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestKindsCmd_MissingExplicitBlueprintWithDocName(t *testing.T) {
	cmd := &KindsCmd{
		Blueprint: filepath.Join(t.TempDir(), "nonexistent", "doc.cf.yaml"),
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatal("KindsCmd.Run = nil, want error on nonexistent explicit blueprint path ending in doc.cf.yaml")
	}
	if !strings.Contains(err.Error(), "read blueprint") {
		t.Fatalf("KindsCmd.Run = %v, want error reading blueprint", err)
	}
}

func TestKindsCmd_DefaultBlueprintOptionalWhenMissing(t *testing.T) {
	cmd := &KindsCmd{
		Blueprint: "doc.cf.yaml",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err != nil {
		t.Fatalf("KindsCmd.Run with default blueprint returned unexpected error: %v", err)
	}
}
