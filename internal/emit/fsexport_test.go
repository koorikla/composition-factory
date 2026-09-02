package emit

import (
	"bytes"
	"strings"
	"testing"
)

// The one invariant that makes the FileSystem export safe: the function's
// FileSystem source concatenates all files with "\n---\n" (lexical order)
// and parses the result as ONE template — so the joined files must be
// byte-identical to the inline template body.
func TestFSTemplateFilesConcatEqualsInlineBody(t *testing.T) {
	b, crds := nestedFixture()
	files, err := TemplateFiles(b, crds)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(b.Spec.Resources)+1 {
		t.Fatalf("%d files for %d resources, want head + one per resource", len(files), len(b.Spec.Resources))
	}
	if files[0].Name != "00-head.yaml.tmpl" {
		t.Errorf("first file %q, want 00-head.yaml.tmpl", files[0].Name)
	}
	if !strings.Contains(files[1].Name, b.Spec.Resources[0].Name) {
		t.Errorf("file %q does not carry its resource's name", files[1].Name)
	}

	parts := make([][]byte, len(files))
	for i, f := range files {
		parts[i] = bytes.TrimSuffix(f.Body, []byte("\n"))
	}
	joined := append(bytes.Join(parts, []byte("\n---\n")), '\n')

	inline, err := inlineTemplateBody(b, crds)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(joined, inline) {
		t.Fatalf("joined files differ from the inline body:\n--- joined ---\n%s\n--- inline ---\n%s", joined, inline)
	}
}

func TestFSCompositionShape(t *testing.T) {
	b, crds := nestedFixture()
	got, err := CompositionFileSystem(b, crds, "/templates/xnesteds.platform.example.org")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"source: FileSystem",
		"fileSystem:",
		"dirPath: /templates/xnesteds.platform.example.org",
		`options: ["missingkey=error"]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "inline:") || strings.Contains(s, "template: |") {
		t.Errorf("filesystem composition still carries an inline template:\n%s", s)
	}
}

func TestFSGenerateTree(t *testing.T) {
	b, crds := nestedFixture()
	outputs, err := GenerateFS(b, crds, "")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	var fns []byte
	for _, o := range outputs {
		paths[o.Path] = true
		if o.Path == "functions.yaml" {
			fns = o.Body
		}
	}
	for _, want := range []string{
		"compositions/xnesteds.platform.example.org.yaml",
		"xrds/xnesteds.platform.example.org.yaml",
		"functions.yaml",
		"templates/xnesteds.platform.example.org/00-head.yaml.tmpl",
		"configmaps/xnesteds.platform.example.org-templates-0.yaml",
		"runtimeconfigs/xnesteds.platform.example.org.yaml",
	} {
		if !paths[want] {
			t.Errorf("missing output %s (have %v)", want, paths)
		}
	}
	// the installed Function must mount the templates: runtimeConfigRef
	if !bytes.Contains(fns, []byte("runtimeConfigRef:")) {
		t.Errorf("functions.yaml lacks runtimeConfigRef:\n%s", fns)
	}
}

func TestFSConfigMapSplitting(t *testing.T) {
	old := templateCMCap
	templateCMCap = 200 // force a split with tiny cap
	defer func() { templateCMCap = old }()

	b, crds := nestedFixture()
	outputs, err := GenerateFS(b, crds, "")
	if err != nil {
		t.Fatal(err)
	}
	cms := 0
	var rc []byte
	for _, o := range outputs {
		if strings.HasPrefix(o.Path, "configmaps/") {
			cms++
		}
		if strings.HasPrefix(o.Path, "runtimeconfigs/") {
			rc = o.Body
		}
	}
	if cms < 2 {
		t.Fatalf("cap 200 bytes produced %d configmaps, want a split", cms)
	}
	// every configmap gets its own volume + mount under its own subdir, so
	// lexical WalkDir order preserves file order across the split
	if !bytes.Contains(rc, []byte("templates-1")) {
		t.Errorf("runtimeconfig does not mount the second configmap:\n%s", rc)
	}
	if !bytes.Contains(rc, []byte("/templates/xnesteds.platform.example.org/1")) {
		t.Errorf("second configmap not mounted under its ordered subdir:\n%s", rc)
	}
}
