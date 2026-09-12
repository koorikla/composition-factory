package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestFieldsCmd_MissingExplicitBlueprintWithDocName(t *testing.T) {
	cmd := &FieldsCmd{
		Blueprint: filepath.Join(t.TempDir(), "nonexistent", "doc.cf.yaml"),
		Kind:      "Queue",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil || !strings.Contains(err.Error(), "read blueprint") {
		t.Fatalf("FieldsCmd.Run = %v, want error reading blueprint on nonexistent explicit path ending in doc.cf.yaml", err)
	}
}

func TestFieldsCmd_MissingExplicitBlueprintWithDocName_NativeKind(t *testing.T) {
	cmd := &FieldsCmd{
		Blueprint: filepath.Join(t.TempDir(), "nonexistent", "doc.cf.yaml"),
		Kind:      "Deployment",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatal("FieldsCmd.Run = nil, want error on nonexistent explicit blueprint path ending in doc.cf.yaml")
	}
}

func TestFieldsCmd_DefaultBlueprintOptionalWhenMissing(t *testing.T) {
	cmd := &FieldsCmd{
		Blueprint: "doc.cf.yaml",
		Kind:      "Deployment",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err != nil {
		t.Fatalf("FieldsCmd.Run with default blueprint returned unexpected error: %v", err)
	}
}
