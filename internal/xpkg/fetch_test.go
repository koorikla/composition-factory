package xpkg

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const streamYAML = `apiVersion: meta.pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-test
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.test.m.example.org
spec:
  group: test.m.example.org
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
`

// layerWithStream builds a one-file tar layer holding the package stream.
func layerWithStream(t *testing.T) v1.Layer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte(streamYAML)
	if err := tw.WriteHeader(&tar.Header{Name: "package.yaml", Size: int64(len(body)), Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestFetchExtractsOnlyTheBaseLayer(t *testing.T) {
	// A registry that serves a two-layer image: one junk runtime layer, one base layer.
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	junk, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("not a package")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	base := layerWithStream(t)
	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}

	img, err := mutate.AppendLayers(empty.Image, junk, base)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg.DeepCopy()
	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	cfg.Config.Labels["io.crossplane.xpkg:"+baseDigest.String()] = "base"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := name.ParseReference(u.Host + "/provider-test:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}

	pkg, err := Fetch(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(pkg.Docs) != 2 {
		t.Fatalf("got %d documents, want 2", len(pkg.Docs))
	}
	if !bytes.Contains(pkg.Docs[1], []byte("widgets.test.m.example.org")) {
		t.Errorf("second document does not look like the CRD: %s", pkg.Docs[1])
	}
	if !strings.HasPrefix(pkg.Digest, "sha256:") {
		t.Errorf("Digest = %q, want a sha256: prefix", pkg.Digest)
	}
}

func TestFetchErrorsWhenNoBaseLabel(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	img, err := mutate.AppendLayers(empty.Image, layerWithStream(t))
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := name.ParseReference(u.Host + "/nolabel:v1")
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background(), ref.String()); err == nil {
		t.Fatal("want an error when the image has no io.crossplane.xpkg base label, got nil")
	}
}
