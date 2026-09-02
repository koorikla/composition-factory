package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// PackageCmd builds a Crossplane Configuration package (.xpkg) from a
// blueprint: the emitted XRD and Composition plus a synthesized
// crossplane.yaml meta document that pins every provider and function the
// blueprint depends on, with the blueprint source embedded for recovery.
type PackageCmd struct {
	Blueprint string `arg:"" help:"Path to the blueprint file."`
	Out       string `short:"o" help:"Output path. Defaults to <blueprint name>.xpkg (or .package.yaml with --yaml)."`
	YAML      bool   `help:"Write the package.yaml document stream instead of an .xpkg image. Importable back via the GUI or POST /api/blueprint/import."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
}

func (c *PackageCmd) Run(out io.Writer) error {
	source, err := os.ReadFile(c.Blueprint)
	if err != nil {
		return err
	}
	b, err := blueprint.Parse(source)
	if err != nil {
		return err
	}
	if b.TemplateSource() == blueprint.TemplateSourceFileSystem {
		return fmt.Errorf("cannot package a blueprint with spec.emit.templateSource: FileSystem " +
			"(a Configuration package ships XRDs and Compositions only; switch to templateSource: Inline or use cf gen)")
	}
	store := cache.New(c.CacheDir)
	crds, err := cache.LoadSources(store, b, filepath.Dir(c.Blueprint))
	if err != nil {
		return err
	}
	native, err := k8s.Kinds()
	if err != nil {
		return err
	}
	crds = append(crds, native...)

	// Generate in memory; only the XRD and Composition belong in the package
	// stream (functions.yaml and providerconfigs are render/bootstrap
	// helpers the xpkg spec keeps out of Configuration packages).
	outputs, err := emit.Generate(b, crds, "")
	if err != nil {
		return err
	}
	var docs [][]byte
	for _, prefix := range []string{"xrds/", "compositions/"} {
		for _, o := range outputs {
			if strings.HasPrefix(filepath.ToSlash(o.Path), prefix) {
				docs = append(docs, o.Body)
			}
		}
	}
	meta, err := emit.ConfigurationMeta(b, source)
	if err != nil {
		return err
	}
	if c.YAML {
		path := c.Out
		if path == "" {
			path = b.Metadata.Name + ".package.yaml"
		}
		if err := os.WriteFile(path, xpkg.Stream(meta, docs), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s\n", path)
		return nil
	}

	img, err := xpkg.Build(meta, docs)
	if err != nil {
		return err
	}

	path := c.Out
	if path == "" {
		path = b.Metadata.Name + ".xpkg"
	}
	if err := xpkg.WriteTarball(img, path, b.Metadata.Name); err != nil {
		return err
	}
	digest, err := img.Digest()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %s (%s)\n", path, digest)
	return nil
}

// PushCmd pushes a built .xpkg to an OCI registry, using the same keychain
// auth path `cf provider add` already relies on. `crossplane xpkg push`
// works on the same file — this is the convenience spelling.
type PushCmd struct {
	Ref     string `arg:"" help:"Registry reference, e.g. ghcr.io/acme/xqueue:v0.1.0."`
	Package string `arg:"" help:"Path to the .xpkg written by cf package."`
}

func (c *PushCmd) Run(out io.Writer) error {
	img, err := tarball.ImageFromPath(c.Package, nil)
	if err != nil {
		return fmt.Errorf("read package %q: %w", c.Package, err)
	}
	ref, err := name.ParseReference(c.Ref)
	if err != nil {
		return fmt.Errorf("parse reference %q: %w", c.Ref, err)
	}
	if err := remote.Write(ref, img, remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return fmt.Errorf("push %q: %w", c.Ref, err)
	}
	digest, err := img.Digest()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "pushed %s (%s)\n", c.Ref, digest)
	return nil
}
