package xpkg

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Build assembles a Crossplane Configuration package image: one layer whose
// only file is package.yaml — the Configuration meta document followed by
// the XRD and Composition documents — labelled the way every xpkg is
// (io.crossplane.xpkg:<layer-digest>=base, the exact contract baseLayer
// reads). Everything is timestamp-free so the same inputs always produce
// the same digest.
func Build(meta []byte, docs [][]byte) (v1.Image, error) {
	stream := bytes.TrimRight(meta, "\n")
	for _, d := range docs {
		stream = append(stream, []byte("\n---\n")...)
		stream = append(stream, bytes.TrimRight(d, "\n")...)
	}
	stream = append(stream, '\n')

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:    "package.yaml",
		Mode:    0o644,
		Size:    int64(len(stream)),
		ModTime: time.Unix(0, 0), // fixed epoch: reproducible layer bytes
	}); err != nil {
		return nil, fmt.Errorf("write package.yaml header: %w", err)
	}
	if _, err := tw.Write(stream); err != nil {
		return nil, fmt.Errorf("write package.yaml: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close layer tar: %w", err)
	}

	// a real (gzip-compressed) tarball layer: its digest is the compressed
	// digest the base label must carry, and Uncompressed() hands readers the
	// tar back — both bit-reproducible because the tar bytes are
	layerBytes := buf.Bytes()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerBytes)), nil
	})
	if err != nil {
		return nil, fmt.Errorf("build package layer: %w", err)
	}
	digest, err := layer.Digest()
	if err != nil {
		return nil, fmt.Errorf("layer digest: %w", err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, fmt.Errorf("append package layer: %w", err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg = cfg.DeepCopy()
	cfg.Created = v1.Time{Time: time.Unix(0, 0)}
	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	cfg.Config.Labels[labelPrefix+digest.String()] = "base"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		return nil, fmt.Errorf("label config: %w", err)
	}
	return img, nil
}

// PackageStream returns the package.yaml document stream from any xpkg
// image — the same base-layer contract Fetch reads over the network.
func PackageStream(img v1.Image) ([]byte, error) {
	layer, err := baseLayer(img)
	if err != nil {
		return nil, err
	}
	return readSingleFileLayer(layer)
}

// WriteTarball saves img as a docker-loadable .xpkg tarball at path, tagged
// with tag (a bare name is fine; it only names the image inside the file —
// `crossplane xpkg push` and cf push re-tag on the way out).
func WriteTarball(img v1.Image, path, tag string) error {
	ref, err := name.NewTag(tag, name.WithDefaultRegistry(""), name.WithDefaultTag("latest"))
	if err != nil {
		return fmt.Errorf("bad package tag %q: %w", tag, err)
	}
	return tarball.WriteToFile(path, ref, img)
}

// WriteTarballTo is WriteTarball into any writer, for callers streaming the
// package instead of landing it on disk.
func WriteTarballTo(w io.Writer, img v1.Image, tag string) error {
	ref, err := name.NewTag(tag, name.WithDefaultRegistry(""), name.WithDefaultTag("latest"))
	if err != nil {
		return fmt.Errorf("bad package tag %q: %w", tag, err)
	}
	return tarball.Write(ref, img, w)
}
