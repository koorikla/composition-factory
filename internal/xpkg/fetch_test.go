package xpkg

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
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
	// A registry that serves a two-layer image: the base layer FIRST and a junk
	// runtime layer LAST. This ordering matters: it makes the test fail for an
	// implementation that picks a layer by position (e.g. "last layer wins")
	// instead of by its io.crossplane.xpkg label, which is the actual contract.
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	base := layerWithStream(t)
	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	junk, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("not a package")), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	img, err := mutate.AppendLayers(empty.Image, base, junk)
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

func TestFetchErrorsOnMultipleBaseLabels(t *testing.T) {
	// Layer selection must be deterministic: an image that labels more than one
	// layer "base" is malformed, and Fetch must reject it with an error naming
	// every matching digest rather than silently picking one (which, since
	// image config labels are read into a Go map, would otherwise depend on
	// map iteration order).
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	base1 := layerWithStream(t)
	base1Digest, err := base1.Digest()
	if err != nil {
		t.Fatal(err)
	}
	base2, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("also not a package, but also labelled base")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	base2Digest, err := base2.Digest()
	if err != nil {
		t.Fatal(err)
	}

	img, err := mutate.AppendLayers(empty.Image, base1, base2)
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
	cfg.Config.Labels["io.crossplane.xpkg:"+base1Digest.String()] = "base"
	cfg.Config.Labels["io.crossplane.xpkg:"+base2Digest.String()] = "base"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := name.ParseReference(u.Host + "/multibase:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}

	_, err = Fetch(context.Background(), ref.String())
	if err == nil {
		t.Fatal("want an error when the image has more than one io.crossplane.xpkg base label, got nil")
	}
	if !strings.Contains(err.Error(), base1Digest.String()) || !strings.Contains(err.Error(), base2Digest.String()) {
		t.Errorf("error %q does not mention both digests (%s, %s)", err.Error(), base1Digest, base2Digest)
	}
}

// TestValidateRef pins the network-free reference check POST /api/providers
// uses to answer 400 before its fetch seam ever runs: a ref name.ParseReference
// accepts passes, and one it rejects fails with the same "parse reference"
// wording Fetch itself uses — so the API's 400 body and a real Fetch's parse
// failure can never disagree about what an invalid ref looks like.
func TestValidateRef(t *testing.T) {
	valid := []string{
		"xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0",
		"ghcr.io/org/provider-name:v1.2.3",
		"registry:5000/repo@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	for _, ref := range valid {
		if err := ValidateRef(ref); err != nil {
			t.Errorf("ValidateRef(%q) = %v, want nil", ref, err)
		}
	}

	invalid := []string{
		"",
		"has spaces/provider:v1",
		"ghcr.io/org/provider:",
		"ghcr.io/org/provider@sha256:notahexdigest",
	}
	for _, ref := range invalid {
		err := ValidateRef(ref)
		if err == nil {
			t.Errorf("ValidateRef(%q) = nil, want an error", ref)
			continue
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("parse reference %q", ref)) {
			t.Errorf("ValidateRef(%q) error = %q, want it to carry the same "+
				"'parse reference' wording Fetch uses", ref, err.Error())
		}
	}
}
