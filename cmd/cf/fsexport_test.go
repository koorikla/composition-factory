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

func TestGenRefusesFileSystemWithNonGoEngine(t *testing.T) {
	for _, engine := range []string{"kcl", "python"} {
		t.Run(engine, func(t *testing.T) {
			dir, bp, cacheDir := seed(t)
			out := filepath.Join(dir, "out")
			cmd := &GenCmd{
				Blueprint:      bp,
				Out:            out,
				CacheDir:       cacheDir,
				Engine:         engine,
				TemplateSource: "filesystem",
			}
			var buf bytes.Buffer
			err := cmd.Run(&buf)
			if err == nil {
				t.Fatalf("cf gen must refuse --template-source filesystem with --engine %s", engine)
			}
			for _, want := range []string{"templateSource", "FileSystem", "go-templating", engine} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should mention %q", err, want)
				}
			}
			if _, statErr := os.Stat(filepath.Join(out, "runtime")); statErr == nil {
				t.Errorf("runtime directory should not be written on refusal")
			}
			if _, statErr := os.Stat(filepath.Join(out, "templates")); statErr == nil {
				t.Errorf("templates directory should not be written on refusal")
			}
		})
	}
}

func TestGenRefusesBlueprintWithFileSystemAndNonGoEngine(t *testing.T) {
	for _, engine := range []string{"kcl", "python"} {
		t.Run(engine, func(t *testing.T) {
			dir, bp, cacheDir := seed(t)
			body := genBlueprint + "  emit:\n    templateSource: FileSystem\n    engine: " + engine + "\n"
			if err := os.WriteFile(bp, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "out")
			cmd := &GenCmd{
				Blueprint: bp,
				Out:       out,
				CacheDir:  cacheDir,
			}
			var buf bytes.Buffer
			err := cmd.Run(&buf)
			if err == nil {
				t.Fatalf("cf gen must refuse blueprint with templateSource: FileSystem and engine: %s", engine)
			}
			for _, want := range []string{"templateSource", "FileSystem", "go-templating", engine} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should mention %q", err, want)
				}
			}
		})
	}
}

func TestGenRefusesBlueprintFileSystemWithEngineFlag(t *testing.T) {
	dir, bp, cacheDir := seedFileSystem(t)
	out := filepath.Join(dir, "out")
	cmd := &GenCmd{
		Blueprint: bp,
		Out:       out,
		CacheDir:  cacheDir,
		Engine:    "kcl",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatalf("cf gen must refuse --engine kcl when blueprint has templateSource: FileSystem")
	}
	for _, want := range []string{"templateSource", "FileSystem", "go-templating", "kcl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestGenRefusesBlueprintEngineWithTemplateSourceFlag(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	body := genBlueprint + "  emit:\n    engine: kcl\n"
	if err := os.WriteFile(bp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	cmd := &GenCmd{
		Blueprint:      bp,
		Out:            out,
		CacheDir:       cacheDir,
		TemplateSource: "filesystem",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatalf("cf gen must refuse --template-source filesystem when blueprint has engine: kcl")
	}
	for _, want := range []string{"templateSource", "FileSystem", "go-templating", "kcl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestAdoptFileSystemExportRoundTrip(t *testing.T) {
	dir, bp, cacheDir := seedFileSystem(t)
	out := filepath.Join(dir, "out")
	genCmd := &GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := genCmd.Run(&buf); err != nil {
		t.Fatalf("GenCmd: %v", err)
	}

	// Adopt the generated out directory
	adoptedPath := filepath.Join(dir, "adopted.yaml")
	adoptCmd := &AdoptCmd{
		Composition: out,
		Out:         adoptedPath,
		CacheDir:    cacheDir,
	}
	buf.Reset()
	code, err := adoptCmd.run(&buf)
	if err != nil || code != 0 {
		t.Fatalf("AdoptCmd run failed: code=%d, err=%v\n%s", code, err, buf.String())
	}

	adoptedBytes, err := os.ReadFile(adoptedPath)
	if err != nil {
		t.Fatalf("read adopted blueprint: %v", err)
	}
	if strings.Contains(string(adoptedBytes), "# adopt: dropped") {
		t.Errorf("unexpected drops in adopted blueprint:\n%s", string(adoptedBytes))
	}
	if !strings.Contains(string(adoptedBytes), "name: main-queue") {
		t.Errorf("adopted blueprint missing main-queue resource:\n%s", string(adoptedBytes))
	}
	if !strings.Contains(string(adoptedBytes), "templateSource: FileSystem") {
		t.Errorf("adopted blueprint should have templateSource: FileSystem:\n%s", string(adoptedBytes))
	}
}
