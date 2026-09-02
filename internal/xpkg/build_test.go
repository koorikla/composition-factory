package xpkg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

var (
	metaDoc = []byte("apiVersion: meta.pkg.crossplane.io/v1\nkind: Configuration\nmetadata:\n  name: xqueue\n")
	xrdDoc  = []byte("apiVersion: apiextensions.crossplane.io/v2\nkind: CompositeResourceDefinition\n")
	compDoc = []byte("apiVersion: apiextensions.crossplane.io/v1\nkind: Composition\n")
)

// The built image must be readable by our own fetch path: exactly one layer
// labelled io.crossplane.xpkg:<digest>=base holding a package.yaml stream of
// meta + XRDs + Compositions.
func TestBuildRoundTripsThroughOwnReader(t *testing.T) {
	img, err := Build(metaDoc, [][]byte{xrdDoc, compDoc})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := baseLayer(img)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := readSingleFileLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	docs := splitYAML(stream)
	if len(docs) != 3 {
		t.Fatalf("package.yaml stream has %d docs, want 3:\n%s", len(docs), stream)
	}
	if !bytes.Contains(docs[0], []byte("kind: Configuration")) {
		t.Errorf("first doc is not the Configuration meta:\n%s", docs[0])
	}
}

func TestBuildReproducible(t *testing.T) {
	a, err := Build(metaDoc, [][]byte{xrdDoc, compDoc})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(metaDoc, [][]byte{xrdDoc, compDoc})
	if err != nil {
		t.Fatal(err)
	}
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da != db {
		t.Fatalf("two builds differ: %s vs %s", da, db)
	}
}

func TestWriteTarballReadsBack(t *testing.T) {
	img, err := Build(metaDoc, [][]byte{xrdDoc})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "xqueue.xpkg")
	if err := WriteTarball(img, path, "xqueue"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		t.Fatalf("tarball not written: %v", err)
	}
	back, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	// the docker tarball format re-serializes the manifest, so image digests
	// don't survive the trip — package content and the base-layer label must
	wantLayer, err := baseLayer(img)
	if err != nil {
		t.Fatal(err)
	}
	want, err := readSingleFileLayer(wantLayer)
	if err != nil {
		t.Fatal(err)
	}
	gotLayer, err := baseLayer(back)
	if err != nil {
		t.Fatalf("read-back image lost the xpkg base label: %v", err)
	}
	got, err := readSingleFileLayer(gotLayer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatal("package.yaml changed across the tarball round-trip")
	}
}
