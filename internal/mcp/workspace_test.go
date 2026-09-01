package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCheckRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	ws, err := newWorkspace(root)
	if err != nil {
		t.Fatalf("newWorkspace: %v", err)
	}

	allowed := []string{
		root,
		filepath.Join(root, "functions.yaml"),
		filepath.Join(root, "xrds", "xqueues.platform.hooli.tech.yaml"),
		// Dot segments that still RESOLVE inside are fine — the gate judges
		// where a path lands, not how it is spelled.
		filepath.Join(root, "xrds", "..", "functions.yaml"),
	}
	for _, p := range allowed {
		if err := ws.check(p); err != nil {
			t.Errorf("check(%q) = %v, want allowed", p, err)
		}
	}

	refused := []string{
		// Plain traversal out of the workspace.
		filepath.Join(root, "..", "escape.yaml"),
		// Traversal buried below a legitimate-looking prefix.
		filepath.Join(root, "xrds", "..", "..", "escape.yaml"),
		// An absolute path with no relation to the workspace at all.
		"/etc/cron.d/escape",
		// A sibling whose name shares the workspace path as a string prefix —
		// the classic bytes-prefix-without-separator bug.
		root + "-evil/escape.yaml",
		// The workspace's own parent.
		filepath.Dir(root),
	}
	for _, p := range refused {
		err := ws.check(p)
		if err == nil {
			t.Errorf("check(%q) = nil, want a refusal", p)
			continue
		}
		if !strings.Contains(err.Error(), "outside the declared --out workspace") {
			t.Errorf("check(%q) error %q does not say what was violated", p, err)
		}
	}
}

// TestWriteOutputsRefusesTraversalWithoutPartialWrites proves the gate at
// the seam every generated-file write goes through: one escaping path
// anywhere in the batch means NO file is written, including the legitimate
// ones listed before it.
func TestWriteOutputsRefusesTraversalWithoutPartialWrites(t *testing.T) {
	root := t.TempDir()
	ws, err := newWorkspace(root)
	if err != nil {
		t.Fatalf("newWorkspace: %v", err)
	}
	s := &server{ws: ws}

	inside := filepath.Join(root, "xrds", "ok.yaml")
	escape := filepath.Join(root, "xrds", "..", "..", "escape.yaml")
	err = s.writeOutputs([]generateOutput{
		{Path: inside, Body: "kind: fine\n"},
		{Path: escape, Body: "kind: hostile\n"},
	})
	if err == nil {
		t.Fatal("writeOutputs accepted a traversal path")
	}
	if !strings.Contains(err.Error(), "outside the declared --out workspace") {
		t.Errorf("error %q does not name the workspace violation", err)
	}
	if _, statErr := os.Stat(inside); !os.IsNotExist(statErr) {
		t.Errorf("the in-workspace file was written before the batch was gated (stat: %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Clean(escape)); !os.IsNotExist(statErr) {
		t.Errorf("the escaping file exists (stat: %v)", statErr)
	}
}

func TestWriteOutputsWritesInsideTheWorkspace(t *testing.T) {
	root := t.TempDir()
	ws, err := newWorkspace(root)
	if err != nil {
		t.Fatalf("newWorkspace: %v", err)
	}
	s := &server{ws: ws}

	path := filepath.Join(root, "compositions", "x.yaml")
	if err := s.writeOutputs([]generateOutput{{Path: path, Body: "kind: ok\n"}}); err != nil {
		t.Fatalf("writeOutputs: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "kind: ok\n" {
		t.Errorf("content = %q, want the body given", got)
	}
}
