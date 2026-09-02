package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestPackageWritesXpkg(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "xqueue.xpkg")
	cmd := &PackageCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := cmd.Run(&buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	img, err := tarball.ImageFromPath(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := xpkg.PackageStream(img)
	if err != nil {
		t.Fatal(err)
	}
	s := string(stream)
	for _, want := range []string{
		"kind: Configuration",
		"factory.crossplane.io/blueprint: |", // the source travels with the package
		"kind: CompositeResourceDefinition",
		"kind: Composition",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("package.yaml missing %q", want)
		}
	}
	// functions.yaml and providerconfigs are render/bootstrap helpers, not
	// package content — the stream is exactly meta + XRD + Composition
	if docs := strings.Count(s, "\n---\n") + 1; docs != 3 {
		t.Errorf("package.yaml has %d documents, want 3 (meta, XRD, Composition)", docs)
	}
}

func TestPackageDeterministic(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	a := filepath.Join(dir, "a.xpkg")
	b := filepath.Join(dir, "b.xpkg")
	for _, out := range []string{a, b} {
		cmd := &PackageCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
		var buf bytes.Buffer
		if err := cmd.Run(&buf); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	ab, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if !bytes.Equal(ab, bb) {
		t.Fatal("two cf package runs produced different bytes")
	}
}

func TestPushPushesToRegistry(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "xqueue.xpkg")
	pkg := &PackageCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := pkg.Run(&buf); err != nil {
		t.Fatalf("package: %v", err)
	}

	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	ref := host + "/acme/xqueue:v0.1.0"

	push := &PushCmd{Ref: ref, Package: out}
	if err := push.Run(&buf); err != nil {
		t.Fatalf("push: %v", err)
	}

	// our own fetch path reads the pushed package back whole
	got, err := xpkg.Fetch(context.Background(), ref)
	if err != nil {
		t.Fatalf("pushed package not readable: %v", err)
	}
	found := false
	for _, d := range got.Docs {
		if bytes.Contains(d, []byte("kind: Configuration")) {
			found = true
		}
	}
	if !found {
		t.Fatal("pushed package stream lacks the Configuration meta doc")
	}
}
