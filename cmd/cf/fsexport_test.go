package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedFileSystem is seed with spec.emit.templateSource: FileSystem appended
// to the blueprint.
func seedFileSystem(t *testing.T) (dir, bpPath, cacheDir string) {
	t.Helper()
	dir, bpPath, cacheDir = seed(t)
	body := genBlueprint + "  emit:\n    templateSource: FileSystem\n"
	if err := os.WriteFile(bpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, bpPath, cacheDir
}

func TestGenFileSystemWritesTemplateFolderAndRuntime(t *testing.T) {
	dir, bp, cacheDir := seedFileSystem(t)
	out := filepath.Join(dir, "out")
	cmd := &GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := cmd.Run(&buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range []string{
		"compositions/xqueues.platform.sparky.ee.yaml",
		"functions.yaml",
		"runtime/xqueues.platform.sparky.ee.yaml",
		"templates/xqueues.platform.sparky.ee/000-context.yaml",
		"templates/xqueues.platform.sparky.ee/001-main-queue.yaml",
		"xrds/xqueues.platform.sparky.ee.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	comp, err := os.ReadFile(filepath.Join(out, "compositions", "xqueues.platform.sparky.ee.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(comp), "source: FileSystem") {
		t.Errorf("composition should use the FileSystem source:\n%s", comp)
	}

	// --check right after gen is in sync, template files included
	chk := &GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}
	buf.Reset()
	code, err := chk.run(&buf)
	if err != nil || code != 0 {
		t.Fatalf("--check after gen: code %d err %v\n%s", code, err, buf.String())
	}
	// and a drifted template file is reported by path
	tpl := filepath.Join(out, "templates", "xqueues.platform.sparky.ee", "001-main-queue.yaml")
	if err := os.WriteFile(tpl, []byte("# edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	code, err = chk.run(&buf)
	if err != nil || code != 2 {
		t.Fatalf("--check with a drifted template: code %d err %v", code, err)
	}
	if !strings.Contains(buf.String(), "drift: "+tpl) {
		t.Errorf("--check output should name the drifted template file:\n%s", buf.String())
	}
}

func TestPackageRefusesFileSystemMode(t *testing.T) {
	dir, bp, cacheDir := seedFileSystem(t)
	out := filepath.Join(dir, "xqueue.xpkg")
	cmd := &PackageCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatal("cf package must refuse a FileSystem-mode blueprint")
	}
	for _, want := range []string{"templateSource", "FileSystem", "Inline"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("no package file may be written on refusal")
	}
}
