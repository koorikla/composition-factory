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
