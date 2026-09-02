package api

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestPackageDownload(t *testing.T) {
	h, _, _, _ := testServerParts(t)

	req := httptest.NewRequest("GET", "/api/package", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".xpkg") {
		t.Errorf("Content-Disposition %q lacks an .xpkg filename", cd)
	}

	// the body is a loadable xpkg tarball whose stream carries the meta,
	// the XRD and the Composition
	path := filepath.Join(t.TempDir(), "dl.xpkg")
	if err := os.WriteFile(path, rec.Body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		t.Fatalf("download is not a valid image tarball: %v", err)
	}
	stream, err := xpkg.PackageStream(img)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kind: Configuration", "kind: CompositeResourceDefinition", "kind: Composition"} {
		if !bytes.Contains(stream, []byte(want)) {
			t.Errorf("package stream missing %q", want)
		}
	}
}

func TestPackageDownloadYAML(t *testing.T) {
	h, _, _, _ := testServerParts(t)

	req := httptest.NewRequest("GET", "/api/package?format=yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "package.yaml") {
		t.Errorf("Content-Disposition %q lacks package.yaml", cd)
	}
	s := rec.Body.String()
	for _, want := range []string{"kind: Configuration", "kind: CompositeResourceDefinition", "kind: Composition"} {
		if !strings.Contains(s, want) {
			t.Errorf("yaml stream missing %q", want)
		}
	}

	// the yaml form and the xpkg's embedded package.yaml are the same bytes
	req2 := httptest.NewRequest("GET", "/api/package", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	path := filepath.Join(t.TempDir(), "dl.xpkg")
	if err := os.WriteFile(path, rec2.Body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := xpkg.PackageStream(img)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream, rec.Body.Bytes()) {
		t.Error("format=yaml differs from the xpkg's package.yaml")
	}
}
