// Package xpkg fetches Crossplane package (xpkg) images over OCI and returns
// the YAML documents they carry. Only the package layer is downloaded: for a
// typical provider that is ~20 KB out of a ~271 MB image.
package xpkg

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// labelPrefix marks the layer holding the package stream. The full label key is
// "io.crossplane.xpkg:<layer-digest>" and its value is "base".
const labelPrefix = "io.crossplane.xpkg:"

// Package is an extracted Crossplane package.
type Package struct {
	// Ref is the reference as requested, e.g. xpkg.upbound.io/upbound/provider-aws-sqs:v2.
	Ref string
	// Digest is the resolved image digest, for lockfile pinning.
	Digest string
	// Docs are the YAML documents from the package stream, in order. The first
	// is the package meta object; the rest are CRDs.
	Docs [][]byte
}

// Fetch resolves ref, downloads only its package layer, and splits the stream
// into YAML documents. It requires no Docker daemon and no cluster.
func Fetch(ctx context.Context, ref string) (*Package, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parse reference %q: %w", ref, err)
	}
	desc, err := remote.Get(r,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", ref, err)
	}
	img, err := desc.Image()
	if err != nil {
		return nil, fmt.Errorf("resolve image %q: %w", ref, err)
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("digest %q: %w", ref, err)
	}
	layer, err := baseLayer(img)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	stream, err := readSingleFileLayer(layer)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	return &Package{Ref: ref, Digest: digest.String(), Docs: splitYAML(stream)}, nil
}

// baseLayer finds the layer the image config labels as the package base.
func baseLayer(img v1.Image) (v1.Layer, error) {
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config: %w", err)
	}
	for k, v := range cfg.Config.Labels {
		if v != "base" || !strings.HasPrefix(k, labelPrefix) {
			continue
		}
		h, err := v1.NewHash(strings.TrimPrefix(k, labelPrefix))
		if err != nil {
			return nil, fmt.Errorf("bad layer digest in label %q: %w", k, err)
		}
		return img.LayerByDigest(h)
	}
	return nil, fmt.Errorf("no %s...=base label found; this does not look like a Crossplane package", labelPrefix)
}

// readSingleFileLayer returns the contents of the first regular file in the layer.
func readSingleFileLayer(l v1.Layer) ([]byte, error) {
	rc, err := l.Uncompressed()
	if err != nil {
		return nil, fmt.Errorf("open layer: %w", err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("package layer contains no file")
		}
		if err != nil {
			return nil, fmt.Errorf("read layer tar: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		return io.ReadAll(tr)
	}
}

// splitYAML splits a multi-document YAML stream on "---" at column zero and
// drops empty documents.
func splitYAML(in []byte) [][]byte {
	var out [][]byte
	for _, part := range bytes.Split(in, []byte("\n---")) {
		trimmed := bytes.TrimSpace(bytes.TrimPrefix(part, []byte("---")))
		if len(trimmed) == 0 {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
