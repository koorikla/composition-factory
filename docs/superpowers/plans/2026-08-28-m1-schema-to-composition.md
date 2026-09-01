# compositionfactory M1 — Schema to Composition — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pull a Crossplane provider package over OCI, index its managed-resource schemas, and generate a working XRD + Composition + functions.yaml from a blueprint file — verified by rendering the result with `crossplane composition render`.

**Architecture:** A single Go binary. `internal/xpkg` fetches only the package layer of an OCI image. `internal/schema` parses those CRDs into a path-addressed tree. `internal/cache` persists them with digest pinning. `internal/blueprint` is the on-disk IR. `internal/emit` turns a blueprint into YAML. `cmd/cf` is a thin kong CLI over those packages — no logic of its own, because the HTTP and MCP front doors in later milestones must call the identical functions.

**Tech Stack:** Go 1.25+ · `github.com/alecthomas/kong` (the Crossplane CLI's own parser) · `github.com/google/go-containerregistry` v0.22.0 · `sigs.k8s.io/yaml` · `github.com/google/go-cmp` · stdlib `testing`

**Spec:** [`docs/superpowers/specs/2026-08-27-compositionfactory-design.md`](../specs/2026-08-27-compositionfactory-design.md)

## Global Constraints

- Go module path is `github.com/koorikla/compositionfactory`; the binary is `cf`.
- Go 1.25 minimum (`go 1.25` in go.mod). The dev machine has 1.27.0.
- No cluster and no Docker may be required by `cf provider add` or `cf gen`. Only the acceptance test in Task 11 may use Docker.
- Emitted YAML must be byte-deterministic: sorted map keys, stable field order, LF endings, a single trailing newline, no timestamps, no version stamps, and no trailing whitespace on any line. This is a correctness requirement — a churning file on a `prune: true` ArgoCD repo is a live-cluster incident.
- Provenance goes in YAML **comments**, never annotations. An annotation creates a perpetual ArgoCD sync loop.
- Never emit a `kustomization.yaml`. It flips ArgoCD from Directory to Kustomize mode, after which any file absent from `resources:` is deleted under `prune: true`.
- The generated Composition must always carry `options: ["missingkey=error"]` at the **top level of the function input**, not nested under `inline`. The nested form is a fatal error.
- XRDs are `apiextensions.crossplane.io/v2` and must always emit `scope:` explicitly. `LegacyCluster` is not a valid v2 scope and must never be emitted.
- For a `Namespaced` XRD, emit the `.m.` namespaced managed-resource variant.
- Never hard-code the managed-resource spec envelope. Compute `envelope = spec.properties − {forProvider, initProvider}` and render what remains from its own schema.

---

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `Makefile`, `.github/workflows/ci.yml` | Module, build/test entry points, two CI lanes (no-Docker and Docker) |
| `cmd/cf/main.go` | kong CLI wiring only — parses flags, calls `internal/*` |
| `cmd/cf/provider.go` | `cf provider add` |
| `cmd/cf/gen.go` | `cf gen`, including `--check` exit codes |
| `internal/xpkg/fetch.go` | OCI pull of the single package layer; returns raw YAML documents |
| `internal/schema/crd.go` | CRD YAML → `CRD` struct; storage-version selection |
| `internal/schema/tree.go` | OpenAPI properties → `Node` tree; `Leaves` path addressing |
| `internal/cache/store.go` | On-disk provider cache + `.cf.lock` digest pinning |
| `internal/blueprint/types.go` | Blueprint IR structs |
| `internal/blueprint/load.go` | Parse + validate a blueprint file |
| `internal/emit/yaml.go` | Deterministic YAML writer shared by all emitters |
| `internal/emit/xrd.go` | Blueprint → CompositeResourceDefinition |
| `internal/emit/composition.go` | Blueprint → Composition (go-template body) |
| `internal/emit/functions.go` | Blueprint → functions.yaml |
| `internal/emit/emit.go` | `Generate()` — the one entry point HTTP/MCP will also call |
| `testdata/` | Golden files and the XQueue acceptance fixtures |

---

## Task 1: Repo scaffold and `cf version`

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore`, `cmd/cf/main.go`, `.github/workflows/ci.yml`
- Test: `cmd/cf/main_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `main.CLI` struct (kong root), and a buildable `cf` binary. Later tasks add subcommand structs as fields on `CLI`.

- [ ] **Step 1: Write the failing test**

`cmd/cf/main_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestVersionCommand(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("cf"), kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"version"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out bytes.Buffer
	if err := ctx.Run(&out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "cf ") {
		t.Errorf("version output = %q, want it to contain %q", out.String(), "cf ")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cf/ -run TestVersionCommand -v`
Expected: FAIL — the package does not compile, `undefined: CLI`.

- [ ] **Step 3: Write minimal implementation**

`go.mod`:
```
module github.com/koorikla/compositionfactory

go 1.25

require (
	github.com/alecthomas/kong v1.16.1
	github.com/google/go-cmp v0.7.0
	github.com/google/go-containerregistry v0.22.0
	sigs.k8s.io/yaml v1.6.0
)
```

`cmd/cf/main.go`:
```go
// Command cf generates Crossplane Compositions and XRDs from provider schemas.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// CLI is the kong root. Subcommands are added as fields in later tasks.
type CLI struct {
	Version VersionCmd `cmd:"" help:"Print the cf version."`
}

type VersionCmd struct{}

func (v *VersionCmd) Run(out io.Writer) error {
	_, err := fmt.Fprintf(out, "cf %s\n", version)
	return err
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("cf"),
		kong.Description("Generate Crossplane Compositions and XRDs from provider schemas."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run(io.Writer(os.Stdout)))
}
```

`Makefile`:
```make
.PHONY: test test-docker lint build

# Lane A: no Docker, no cluster. Must pass anywhere.
test:
	go test ./... -short

# Lane B: needs a Docker daemon.
test-docker:
	go test ./... -run Acceptance -v

build:
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/cf ./cmd/cf

lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | test -z "$$(cat)"
```

`.gitignore`:
```
/bin/
/out/
```

`.github/workflows/ci.yml`:
```yaml
name: ci
on: [push, pull_request]
jobs:
  # Lane A runs everywhere and catches malformed output fast.
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with: { go-version: '1.25' }
      - run: make lint
      - run: make test
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go mod tidy && go test ./cmd/cf/ -run TestVersionCommand -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum Makefile .gitignore cmd/cf/main.go cmd/cf/main_test.go .github/workflows/ci.yml
git commit -m "feat: scaffold cf binary with kong and CI lane A"
```

---

## Task 2: Fetch the xpkg package layer over OCI

**Files:**
- Create: `internal/xpkg/fetch.go`
- Test: `internal/xpkg/fetch_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Package struct { Ref string; Digest string; Docs [][]byte }`
  - `func Fetch(ctx context.Context, ref string) (*Package, error)`

**Background the implementer needs:** a Crossplane package image tags its schema layer in the **image config labels**, as `io.crossplane.xpkg:<layer-digest>=base`. Fetching only that layer costs ~20 KB against a ~271 MB image. The layer is a tarball containing a single file (conventionally `package.yaml`) holding a multi-document YAML stream: the provider meta object followed by every CRD.

- [ ] **Step 1: Write the failing test**

`internal/xpkg/fetch_test.go`. This uses go-containerregistry's in-process registry, so the test needs no network:
```go
package xpkg

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/registry"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/xpkg/ -v`
Expected: FAIL — `undefined: Fetch`.

- [ ] **Step 3: Write minimal implementation**

`internal/xpkg/fetch.go`:
```go
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
```

Add `"io"` to the test file's imports if `go vet` flags it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/xpkg/ -v`
Expected: PASS, both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/xpkg/
git commit -m "feat(xpkg): fetch only the package layer of an OCI xpkg image"
```

---

## Task 3: Parse CRDs and pick the right version

**Files:**
- Create: `internal/schema/crd.go`
- Test: `internal/schema/crd_test.go`

**Interfaces:**
- Consumes: `xpkg.Package.Docs` (`[][]byte`).
- Produces:
  - `type CRD struct { Group, Kind, Plural, Scope string; Categories []string; Versions []Version }`
  - `type Version struct { Name string; Served, Storage, Deprecated bool; Properties map[string]any }`
  - `func ParseCRDs(docs [][]byte) ([]CRD, error)`
  - `func (c CRD) Preferred() (Version, error)`
  - `func (c CRD) IsManaged() bool`
  - `func (c CRD) Namespaced() bool`
  - `func (c CRD) APIVersion() string` — e.g. `sqs.aws.m.upbound.io/v1beta1`

**Background:** never take `versions[0]`. 14 of 102 legacy EC2 CRDs serve two versions with inconsistent storage flags. Take the `storage: true` version and skip `deprecated: true`. A managed resource is identified by the `managed` category.

- [ ] **Step 1: Write the failing test**

`internal/schema/crd_test.go`:
```go
package schema

import "testing"

var twoVersionCRD = []byte(`
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
    categories: [crossplane, managed]
  versions:
  - name: v1beta1
    served: true
    storage: false
    deprecated: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region: {type: string}
  - name: v1beta2
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region: {type: string}
`)

var notManaged = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: providerconfigs.test.example.org
spec:
  group: test.example.org
  scope: Cluster
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [crossplane, providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)

func TestParseCRDsPicksStorageVersion(t *testing.T) {
	crds, err := ParseCRDs([][]byte{twoVersionCRD})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if len(crds) != 1 {
		t.Fatalf("got %d CRDs, want 1", len(crds))
	}
	c := crds[0]
	v, err := c.Preferred()
	if err != nil {
		t.Fatalf("Preferred: %v", err)
	}
	if v.Name != "v1beta2" {
		t.Errorf("Preferred = %q, want v1beta2 (the storage version, not versions[0])", v.Name)
	}
	if got, want := c.APIVersion(), "test.m.example.org/v1beta2"; got != want {
		t.Errorf("APIVersion = %q, want %q", got, want)
	}
	if !c.IsManaged() {
		t.Error("IsManaged = false, want true")
	}
	if !c.Namespaced() {
		t.Error("Namespaced = false, want true")
	}
}

func TestParseCRDsSkipsNonManaged(t *testing.T) {
	crds, err := ParseCRDs([][]byte{notManaged})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if crds[0].IsManaged() {
		t.Error("ProviderConfig reported as managed; it carries the providerconfig category")
	}
}

func TestParseCRDsIgnoresNonCRDDocuments(t *testing.T) {
	meta := []byte("apiVersion: meta.pkg.crossplane.io/v1\nkind: Provider\nmetadata: {name: p}\n")
	crds, err := ParseCRDs([][]byte{meta, twoVersionCRD})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if len(crds) != 1 {
		t.Fatalf("got %d CRDs, want 1 (the meta document must be skipped)", len(crds))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/schema/ -v`
Expected: FAIL — `undefined: ParseCRDs`.

- [ ] **Step 3: Write minimal implementation**

`internal/schema/crd.go`:
```go
// Package schema turns CustomResourceDefinition documents into a form the
// generator can walk: a preferred version per kind and a path-addressed tree.
package schema

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// CRD is the subset of a CustomResourceDefinition the generator needs.
type CRD struct {
	Group      string
	Kind       string
	Plural     string
	Scope      string
	Categories []string
	Versions   []Version
}

// Version is one served version of a CRD.
type Version struct {
	Name       string
	Served     bool
	Storage    bool
	Deprecated bool
	// Properties is spec.versions[].schema.openAPIV3Schema.properties, left as
	// decoded JSON so the tree builder can walk it without a typed OpenAPI model.
	Properties map[string]any
}

// crdDoc mirrors only the fields we read.
type crdDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Group string `json:"group"`
		Scope string `json:"scope"`
		Names struct {
			Kind       string   `json:"kind"`
			Plural     string   `json:"plural"`
			Categories []string `json:"categories"`
		} `json:"names"`
		Versions []struct {
			Name       string `json:"name"`
			Served     bool   `json:"served"`
			Storage    bool   `json:"storage"`
			Deprecated bool   `json:"deprecated"`
			Schema     struct {
				OpenAPIV3Schema struct {
					Properties map[string]any `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// ParseCRDs decodes every CustomResourceDefinition in docs, skipping any
// document that is not one (such as the package meta object).
func ParseCRDs(docs [][]byte) ([]CRD, error) {
	var out []CRD
	for i, d := range docs {
		var doc crdDoc
		if err := yaml.Unmarshal(d, &doc); err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		if doc.Kind != "CustomResourceDefinition" {
			continue
		}
		c := CRD{
			Group:      doc.Spec.Group,
			Kind:       doc.Spec.Names.Kind,
			Plural:     doc.Spec.Names.Plural,
			Scope:      doc.Spec.Scope,
			Categories: doc.Spec.Names.Categories,
		}
		for _, v := range doc.Spec.Versions {
			c.Versions = append(c.Versions, Version{
				Name:       v.Name,
				Served:     v.Served,
				Storage:    v.Storage,
				Deprecated: v.Deprecated,
				Properties: v.Schema.OpenAPIV3Schema.Properties,
			})
		}
		out = append(out, c)
	}
	return out, nil
}

// Preferred returns the storage version, falling back to the first served
// non-deprecated version. It never blindly returns Versions[0].
func (c CRD) Preferred() (Version, error) {
	for _, v := range c.Versions {
		if v.Storage {
			return v, nil
		}
	}
	for _, v := range c.Versions {
		if v.Served && !v.Deprecated {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("%s.%s: no storage or served non-deprecated version", c.Plural, c.Group)
}

// IsManaged reports whether this CRD is a Crossplane managed resource.
func (c CRD) IsManaged() bool {
	for _, cat := range c.Categories {
		if cat == "managed" {
			return true
		}
	}
	return false
}

// Namespaced reports whether the CRD is namespace-scoped. In Crossplane v2 the
// namespaced managed-resource variants live in ".m." groups.
func (c CRD) Namespaced() bool { return c.Scope == "Namespaced" }

// APIVersion returns group/version for the preferred version, or group/ if none.
func (c CRD) APIVersion() string {
	v, err := c.Preferred()
	if err != nil {
		return c.Group + "/"
	}
	return c.Group + "/" + v.Name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/schema/ -v`
Expected: PASS, all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/schema/crd.go internal/schema/crd_test.go
git commit -m "feat(schema): parse CRDs and select the storage version"
```

---

## Task 4: Build the path-addressed schema tree

**Files:**
- Create: `internal/schema/tree.go`
- Test: `internal/schema/tree_test.go`

**Interfaces:**
- Consumes: `Version.Properties` from Task 3.
- Produces:
  - `type Node struct { Name, Type, Description string; Required bool; Children []*Node }`
  - `type Leaf struct { Path string; Node *Node }`
  - `func BuildTree(props map[string]any, required []string) []*Node`
  - `func (c CRD) ForProvider() ([]*Node, error)`
  - `func (c CRD) Envelope() ([]*Node, error)` — `spec.properties − {forProvider, initProvider}`
  - `func Leaves(nodes []*Node, prefix string) []Leaf`

**Background:** paths address array elements as `containers[0].image`. `Envelope` must be computed, never hard-coded: `provider-kubernetes`'s `ObservedObjectCollection` has no `forProvider` at all, and only `spec.providerConfigRef` survived every CRD inspected.

- [ ] **Step 1: Write the failing test**

`internal/schema/tree_test.go`:
```go
package schema

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

var nestedCRD = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: things.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Thing, plural: things, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string, description: Region to use.}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  containers:
                    type: array
                    items:
                      properties:
                        image: {type: string}
                        ports:
                          type: array
                          items:
                            properties:
                              containerPort: {type: integer}
              managementPolicies:
                type: array
                items: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties:
                  kind: {type: string}
                  name: {type: string}
`)

func parseOne(t *testing.T, doc []byte) CRD {
	t.Helper()
	crds, err := ParseCRDs([][]byte{doc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	return crds[0]
}

func TestLeavesUsesArrayIndexedPaths(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, err := c.ForProvider()
	if err != nil {
		t.Fatalf("ForProvider: %v", err)
	}
	var got []string
	for _, l := range Leaves(fp, "") {
		got = append(got, l.Path)
	}
	want := []string{
		"containers[0].image",
		"containers[0].ports[0].containerPort",
		"region",
		"tags",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Leaves paths (-want +got):\n%s", diff)
	}
}

func TestMapIsALeafNotABranch(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, _ := c.ForProvider()
	for _, l := range Leaves(fp, "") {
		if l.Path == "tags" && l.Node.Type != "map" {
			t.Errorf("tags type = %q, want map (additionalProperties collapses to a leaf)", l.Node.Type)
		}
	}
}

func TestRequiredIsCarried(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, _ := c.ForProvider()
	for _, l := range Leaves(fp, "") {
		if l.Path == "region" && !l.Node.Required {
			t.Error("region.Required = false, want true")
		}
		if l.Path == "tags" && l.Node.Required {
			t.Error("tags.Required = true, want false")
		}
	}
}

func TestEnvelopeExcludesForProviderAndInitProvider(t *testing.T) {
	c := parseOne(t, nestedCRD)
	env, err := c.Envelope()
	if err != nil {
		t.Fatalf("Envelope: %v", err)
	}
	var got []string
	for _, n := range env {
		got = append(got, n.Name)
	}
	want := []string{"managementPolicies", "providerConfigRef"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Envelope (-want +got):\n%s", diff)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/schema/ -run 'Leaves|Envelope|Map|Required' -v`
Expected: FAIL — `undefined: Leaves`.

- [ ] **Step 3: Write minimal implementation**

`internal/schema/tree.go`:
```go
package schema

import (
	"fmt"
	"sort"
)

// Node is one property in a schema tree. A Node with Children is a branch; one
// without is a leaf that can carry a value.
type Node struct {
	Name        string
	Type        string // string, number, integer, boolean, object, array, map
	Description string
	Required    bool
	Children    []*Node
}

// Leaf is a settable field and the dotted path that addresses it. Array
// elements are indexed, e.g. containers[0].image.
type Leaf struct {
	Path string
	Node *Node
}

// BuildTree converts an OpenAPI properties map into sorted Nodes. Sorting keeps
// emitted output deterministic.
func BuildTree(props map[string]any, required []string) []*Node {
	req := make(map[string]bool, len(required))
	for _, r := range required {
		req[r] = true
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]*Node, 0, len(names))
	for _, name := range names {
		raw, _ := props[name].(map[string]any)
		out = append(out, buildNode(name, raw, req[name]))
	}
	return out
}

func buildNode(name string, raw map[string]any, required bool) *Node {
	n := &Node{Name: name, Required: required}
	n.Type, _ = raw["type"].(string)
	n.Description, _ = raw["description"].(string)

	switch n.Type {
	case "object":
		// additionalProperties means a map of scalars: a leaf, not a branch.
		if _, isMap := raw["additionalProperties"]; isMap {
			n.Type = "map"
			return n
		}
		if props, ok := raw["properties"].(map[string]any); ok {
			n.Children = BuildTree(props, stringSlice(raw["required"]))
		}
	case "array":
		if items, ok := raw["items"].(map[string]any); ok {
			if props, ok := items["properties"].(map[string]any); ok {
				n.Children = BuildTree(props, stringSlice(items["required"]))
			}
		}
	default:
		// An untyped node with properties is still an object in practice.
		if props, ok := raw["properties"].(map[string]any); ok && n.Type == "" {
			n.Type = "object"
			n.Children = BuildTree(props, stringSlice(raw["required"]))
		}
	}
	return n
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Leaves flattens nodes to settable fields with their paths.
func Leaves(nodes []*Node, prefix string) []Leaf {
	var out []Leaf
	for _, n := range nodes {
		path := n.Name
		if prefix != "" {
			path = prefix + "." + n.Name
		}
		switch {
		case len(n.Children) == 0:
			out = append(out, Leaf{Path: path, Node: n})
		case n.Type == "array":
			out = append(out, Leaves(n.Children, path+"[0]")...)
		default:
			out = append(out, Leaves(n.Children, path)...)
		}
	}
	return out
}

// specProperties returns spec.properties and spec.required for the preferred version.
func (c CRD) specProperties() (map[string]any, []string, error) {
	v, err := c.Preferred()
	if err != nil {
		return nil, nil, err
	}
	spec, ok := v.Properties["spec"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s: no spec in openAPIV3Schema", c.Kind)
	}
	props, ok := spec["properties"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s: no spec.properties", c.Kind)
	}
	return props, stringSlice(spec["required"]), nil
}

// ForProvider returns the spec.forProvider subtree.
func (c CRD) ForProvider() ([]*Node, error) {
	props, _, err := c.specProperties()
	if err != nil {
		return nil, err
	}
	fp, ok := props["forProvider"].(map[string]any)
	if !ok {
		// Legitimate: provider-kubernetes ObservedObjectCollection has none.
		return nil, nil
	}
	inner, _ := fp["properties"].(map[string]any)
	return BuildTree(inner, stringSlice(fp["required"])), nil
}

// Envelope returns spec.properties minus forProvider and initProvider. It is
// computed rather than hard-coded: the envelope is not universal across providers.
func (c CRD) Envelope() ([]*Node, error) {
	props, required, err := c.specProperties()
	if err != nil {
		return nil, err
	}
	rest := make(map[string]any, len(props))
	for k, v := range props {
		if k == "forProvider" || k == "initProvider" {
			continue
		}
		rest[k] = v
	}
	return BuildTree(rest, required), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/schema/ -v`
Expected: PASS, all seven tests.

- [ ] **Step 5: Commit**

```bash
git add internal/schema/tree.go internal/schema/tree_test.go
git commit -m "feat(schema): path-addressed tree with computed spec envelope"
```

---

## Task 5: Cache providers on disk and pin them in a lockfile

**Files:**
- Create: `internal/cache/store.go`
- Test: `internal/cache/store_test.go`

**Interfaces:**
- Consumes: `xpkg.Package`, `schema.CRD`.
- Produces:
  - `type Store struct { Root string }`
  - `func New(root string) *Store`
  - `func (s *Store) Save(pkg *xpkg.Package, crds []schema.CRD) error`
  - `func (s *Store) Load(ref string) ([]schema.CRD, error)`
  - `type Lock struct { Providers []LockEntry }`, `type LockEntry struct { Ref, Digest string }`
  - `func ReadLock(path string) (*Lock, error)`, `func (l *Lock) Write(path string) error`, `func (l *Lock) Set(ref, digest string)`

**Background:** `:v2` is a moving tag. Without digest pinning the same blueprint emits a different Composition next month, which breaks reproducible generation.

- [ ] **Step 1: Write the failing test**

`internal/cache/store_test.go`:
```go
package cache

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	pkg := &xpkg.Package{Ref: "example.org/provider-test:v2", Digest: "sha256:abc123"}
	crds := []schema.CRD{{
		Group: "test.m.example.org", Kind: "Widget", Plural: "widgets",
		Scope: "Namespaced", Categories: []string{"managed"},
		Versions: []schema.Version{{Name: "v1beta1", Served: true, Storage: true}},
	}}
	if err := s.Save(pkg, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load(pkg.Ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff(crds, got); diff != "" {
		t.Errorf("round trip (-want +got):\n%s", diff)
	}
}

func TestLoadUnknownProviderErrors(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Load("example.org/never-added:v1"); err == nil {
		t.Fatal("want an error loading a provider that was never added, got nil")
	}
}

func TestLockSetIsIdempotentAndSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cf.lock")
	l := &Lock{}
	l.Set("example.org/b:v1", "sha256:bbb")
	l.Set("example.org/a:v1", "sha256:aaa")
	l.Set("example.org/b:v1", "sha256:bbb2") // same ref, new digest -> replace
	if err := l.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := ReadLock(path)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	want := []LockEntry{
		{Ref: "example.org/a:v1", Digest: "sha256:aaa"},
		{Ref: "example.org/b:v1", Digest: "sha256:bbb2"},
	}
	if diff := cmp.Diff(want, got.Providers); diff != "" {
		t.Errorf("lock entries (-want +got):\n%s", diff)
	}
}

func TestReadLockMissingFileIsEmptyNotAnError(t *testing.T) {
	l, err := ReadLock(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("ReadLock on a missing file should succeed: %v", err)
	}
	if len(l.Providers) != 0 {
		t.Errorf("got %d providers, want 0", len(l.Providers))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

`internal/cache/store.go`:
```go
// Package cache persists extracted provider schemas on disk and pins the
// resolved image digests so generation is reproducible.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// Store is a directory of cached provider schemas.
type Store struct{ Root string }

// New returns a Store rooted at root. Use DefaultRoot for the usual location.
func New(root string) *Store { return &Store{Root: root} }

// DefaultRoot is ~/.cache/compositionfactory, or ./.cf-cache if HOME is unset.
func DefaultRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ".cf-cache"
	}
	return filepath.Join(dir, "compositionfactory")
}

// slug turns an image reference into a filesystem-safe directory name.
func slug(ref string) string {
	r := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return r.Replace(ref)
}

// Save writes the parsed CRDs for pkg into the cache.
func (s *Store) Save(pkg *xpkg.Package, crds []schema.CRD) error {
	dir := filepath.Join(s.Root, slug(pkg.Ref))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	body, err := json.MarshalIndent(crds, "", " ")
	if err != nil {
		return fmt.Errorf("encode CRDs: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "crds.json"), body, 0o644)
}

// Load returns the cached CRDs for ref.
func (s *Store) Load(ref string) ([]schema.CRD, error) {
	path := filepath.Join(s.Root, slug(ref), "crds.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider %q is not in the cache; run: cf provider add %s", ref, ref)
	}
	var crds []schema.CRD
	if err := json.Unmarshal(body, &crds); err != nil {
		return nil, fmt.Errorf("decode cached CRDs for %q: %w", ref, err)
	}
	return crds, nil
}

// LockEntry pins one provider reference to a resolved digest.
type LockEntry struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

// Lock is the contents of .cf.lock.
type Lock struct {
	Providers []LockEntry `json:"providers"`
}

// Set adds or replaces the entry for ref and keeps the list sorted.
func (l *Lock) Set(ref, digest string) {
	for i := range l.Providers {
		if l.Providers[i].Ref == ref {
			l.Providers[i].Digest = digest
			return
		}
	}
	l.Providers = append(l.Providers, LockEntry{Ref: ref, Digest: digest})
	sort.Slice(l.Providers, func(i, j int) bool { return l.Providers[i].Ref < l.Providers[j].Ref })
}

// ReadLock reads path. A missing file is an empty lock, not an error.
func ReadLock(path string) (*Lock, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var l Lock
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &l, nil
}

// Write saves the lock with a trailing newline, sorted and stable.
func (l *Lock) Write(path string) error {
	sort.Slice(l.Providers, func(i, j int) bool { return l.Providers[i].Ref < l.Providers[j].Ref })
	body, err := json.MarshalIndent(l, "", " ")
	if err != nil {
		return fmt.Errorf("encode lock: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/ -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat(cache): on-disk provider schema cache with digest lockfile"
```

---

## Task 6: `cf provider add`

**Files:**
- Create: `cmd/cf/provider.go`
- Modify: `cmd/cf/main.go` — add the `Provider` field to `CLI`
- Test: `cmd/cf/provider_test.go`

**Interfaces:**
- Consumes: `xpkg.Fetch`, `schema.ParseCRDs`, `cache.Store`, `cache.Lock`.
- Produces: `type ProviderCmd struct { Add ProviderAddCmd }` on the kong root.

**Background:** `provider-aws-sqs` ships **zero** ProviderConfig CRDs — they live in `provider-family-aws`, and `crossplane xpkg get-crds` does not follow `spec.dependsOn`. M1 does not auto-follow dependencies; it prints a hint so the user adds the family explicitly. Auto-following lands in M2 with the palette.

- [ ] **Step 1: Write the failing test**

`cmd/cf/provider_test.go`:
```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// fakeFetch stands in for the network.
func fakeFetch(ref string) (*xpkg.Package, error) {
	return &xpkg.Package{
		Ref:    ref,
		Digest: "sha256:deadbeef",
		Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Widget, plural: widgets, categories: [managed]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)},
	}, nil
}

func TestProviderAddCachesAndLocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	cmd := &ProviderAddCmd{
		Ref:      "example.org/provider-test:v2",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     lockPath,
		fetch:    fakeFetch,
	}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "1 managed resource") {
		t.Errorf("output = %q, want a managed-resource count", out.String())
	}

	got, err := cache.New(filepath.Join(dir, "cache")).Load(cmd.Ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "Widget" {
		t.Fatalf("cached CRDs = %+v, want one Widget", got)
	}

	l, err := cache.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if len(l.Providers) != 1 || l.Providers[0].Digest != "sha256:deadbeef" {
		t.Errorf("lock = %+v, want the resolved digest pinned", l.Providers)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lockfile not written: %v", err)
	}
}

func TestProviderAddReportsZeroManagedResources(t *testing.T) {
	dir := t.TempDir()
	cmd := &ProviderAddCmd{
		Ref:      "example.org/family:v2",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     filepath.Join(dir, ".cf.lock"),
		fetch: func(ref string) (*xpkg.Package, error) {
			return &xpkg.Package{Ref: ref, Digest: "sha256:f00", Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: providerconfigs.test.example.org}
spec:
  group: test.example.org
  scope: Cluster
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)}}, nil
		},
	}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// A family package legitimately has none; say so rather than looking broken.
	if !strings.Contains(out.String(), "0 managed resources") {
		t.Errorf("output = %q, want it to report zero managed resources", out.String())
	}
	var _ schema.CRD
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cf/ -run TestProviderAdd -v`
Expected: FAIL — `undefined: ProviderAddCmd`.

- [ ] **Step 3: Write minimal implementation**

`cmd/cf/provider.go`:
```go
package main

import (
	"context"
	"fmt"
	"io"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// ProviderCmd groups provider subcommands.
type ProviderCmd struct {
	Add ProviderAddCmd `cmd:"" help:"Fetch a provider package and cache its schemas."`
}

// ProviderAddCmd pulls an xpkg image, extracts its CRDs, caches them and pins
// the digest. It needs no cluster and no Docker.
type ProviderAddCmd struct {
	Ref      string `arg:"" help:"xpkg reference, e.g. xpkg.upbound.io/upbound/provider-aws-sqs:v2"`
	CacheDir string `help:"Schema cache directory." default:"${cachedir}"`
	Lock     string `help:"Lockfile path." default:".cf.lock"`

	// fetch is swapped in tests.
	fetch func(ref string) (*xpkg.Package, error)
}

func (c *ProviderAddCmd) Run(out io.Writer) error {
	fetch := c.fetch
	if fetch == nil {
		fetch = func(ref string) (*xpkg.Package, error) {
			return xpkg.Fetch(context.Background(), ref)
		}
	}
	pkg, err := fetch(c.Ref)
	if err != nil {
		return err
	}
	crds, err := schema.ParseCRDs(pkg.Docs)
	if err != nil {
		return fmt.Errorf("%s: %w", c.Ref, err)
	}
	if err := cache.New(c.CacheDir).Save(pkg, crds); err != nil {
		return err
	}
	l, err := cache.ReadLock(c.Lock)
	if err != nil {
		return err
	}
	l.Set(c.Ref, pkg.Digest)
	if err := l.Write(c.Lock); err != nil {
		return err
	}

	managed := 0
	for _, c := range crds {
		if c.IsManaged() {
			managed++
		}
	}
	noun := "managed resources"
	if managed == 1 {
		noun = "managed resource"
	}
	fmt.Fprintf(out, "added %s\n  digest %s\n  %d %s of %d CRDs\n",
		c.Ref, pkg.Digest, managed, noun, len(crds))
	if managed == 0 {
		fmt.Fprintf(out, "  note: this package defines no managed resources. Family packages\n"+
			"  carry only ProviderConfig types; add the service package too.\n")
	}
	return nil
}
```

In `cmd/cf/main.go`, add the field and the kong variable:
```go
type CLI struct {
	Version  VersionCmd  `cmd:"" help:"Print the cf version."`
	Provider ProviderCmd `cmd:"" help:"Manage provider schema sources."`
}
```
and in `main()` add `kong.Vars{"cachedir": cache.DefaultRoot()}` to the `kong.Parse` options, importing `"github.com/koorikla/compositionfactory/internal/cache"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cf/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/cf/provider.go cmd/cf/provider_test.go cmd/cf/main.go
git commit -m "feat(cli): cf provider add fetches, caches and pins a provider"
```

---

## Task 7: Blueprint types, loading and validation

**Files:**
- Create: `internal/blueprint/types.go`, `internal/blueprint/load.go`
- Test: `internal/blueprint/load_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Blueprint struct { APIVersion, Kind string; Metadata Metadata; Spec Spec }`
  - `type Spec struct { Sources []Source; XRD XRD; Resources []Resource }`
  - `type XRD struct { Group, Kind, Plural, Version, Scope string; Parameters map[string]Parameter }`
  - `type Parameter struct { Type string; Required bool; Enum []string; Default string; Description string }`
  - `type Resource struct { Name, Kind, Provider string; Fields map[string]Field }`
  - `type Field struct { From, Value, Raw string }`
  - `func Load(path string) (*Blueprint, error)`
  - `func (b *Blueprint) Validate() error`

**Background:** the `parameters:` block is single-source — one declaration emits both the XRD schema and the template's `| default`. Writing defaults twice is a documented pain in real repos.

- [ ] **Step 1: Write the failing test**

`internal/blueprint/load_test.go`:
```go
package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bp.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidBlueprint(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Spec.XRD.Kind != "XQueue" {
		t.Errorf("Kind = %q, want XQueue", b.Spec.XRD.Kind)
	}
	if got := b.Spec.XRD.Parameters["location"]; !got.Required || len(got.Enum) != 2 {
		t.Errorf("location = %+v, want required with 2 enum values", got)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resources = %+v, want one named main-queue", b.Spec.Resources)
	}
}

func TestValidateRejectsMissingScope(t *testing.T) {
	body := strings.Replace(valid, "    scope: Namespaced\n", "", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err = %v, want a complaint about scope", err)
	}
}

func TestValidateRejectsLegacyClusterScope(t *testing.T) {
	body := strings.Replace(valid, "scope: Namespaced", "scope: LegacyCluster", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "LegacyCluster") {
		t.Fatalf("err = %v, want LegacyCluster to be refused", err)
	}
}

func TestValidateRejectsUnknownParameterType(t *testing.T) {
	body := strings.Replace(valid, "maxMessageSize: {type: integer}", "maxMessageSize: {type: int}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Fatalf("err = %v, want an unknown-type error naming int", err)
	}
}

func TestValidateRejectsFieldWithTwoSources(t *testing.T) {
	body := strings.Replace(valid,
		"maxMessageSize: {from: params.maxMessageSize}",
		"maxMessageSize: {from: params.maxMessageSize, value: \"1024\"}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want a complaint that a field has more than one source", err)
	}
}

func TestValidateRejectsUnknownParameterReference(t *testing.T) {
	body := strings.Replace(valid, "params.maxMessageSize}", "params.nope}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/blueprint/ -v`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

`internal/blueprint/types.go`:
```go
// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

// Blueprint is the root document.
type Blueprint struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name string `json:"name"`
}

type Spec struct {
	Sources   []Source   `json:"sources"`
	XRD       XRD        `json:"xrd"`
	Resources []Resource `json:"resources"`
}

// Source is one schema source. M1 supports provider packages only.
type Source struct {
	Provider string `json:"provider"`
}

// XRD describes the composite API to generate.
type XRD struct {
	Group      string               `json:"group"`
	Kind       string               `json:"kind"`
	Plural     string               `json:"plural"`
	Version    string               `json:"version"`
	Scope      string               `json:"scope"`
	Parameters map[string]Parameter `json:"parameters"`
}

// Parameter is one spec field of the composite API. It is single-source: this
// declaration produces both the XRD schema and the template default.
type Parameter struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
}

// Resource is one composed resource.
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	Fields   map[string]Field `json:"fields"`
}

// Field sets one path on a composed resource. Exactly one of From, Value or Raw
// must be set.
type Field struct {
	From  string `json:"from"`
	Value string `json:"value"`
	Raw   string `json:"raw"`
}
```

`internal/blueprint/load.go`:
```go
package blueprint

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// validTypes are the parameter types M1 accepts.
var validTypes = map[string]bool{
	"string": true, "integer": true, "number": true,
	"boolean": true, "object": true, "array": true,
}

// Load reads and validates a blueprint file.
func Load(path string) (*Blueprint, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read blueprint: %w", err)
	}
	var b Blueprint
	if err := yaml.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &b, nil
}

// Validate reports the first structural problem, naming the offending field.
func (b *Blueprint) Validate() error {
	x := b.Spec.XRD
	if x.Group == "" || x.Kind == "" || x.Plural == "" || x.Version == "" {
		return fmt.Errorf("spec.xrd needs group, kind, plural and version")
	}
	switch x.Scope {
	case "Namespaced", "Cluster":
	case "LegacyCluster":
		return fmt.Errorf("spec.xrd.scope: LegacyCluster is not valid in apiextensions.crossplane.io/v2")
	case "":
		return fmt.Errorf("spec.xrd.scope must be set explicitly to Namespaced or Cluster; " +
			"the server and the crossplane CLI default it differently")
	default:
		return fmt.Errorf("spec.xrd.scope: unknown scope %q", x.Scope)
	}

	names := make([]string, 0, len(x.Parameters))
	for n := range x.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if t := x.Parameters[n].Type; !validTypes[t] {
			return fmt.Errorf("spec.xrd.parameters.%s: unknown type %q", n, t)
		}
	}

	for _, r := range b.Spec.Resources {
		if r.Name == "" || r.Kind == "" {
			return fmt.Errorf("every resource needs a name and a kind")
		}
		paths := make([]string, 0, len(r.Fields))
		for p := range r.Fields {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			f := r.Fields[p]
			set := 0
			for _, v := range []string{f.From, f.Value, f.Raw} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return fmt.Errorf("resource %q field %q: set exactly one of from, value or raw (got %d)",
					r.Name, p, set)
			}
			if f.From != "" {
				param, ok := strings.CutPrefix(f.From, "params.")
				if !ok {
					return fmt.Errorf("resource %q field %q: from must start with params. (got %q)",
						r.Name, p, f.From)
				}
				if _, exists := x.Parameters[param]; !exists {
					return fmt.Errorf("resource %q field %q: references unknown parameter %q",
						r.Name, p, param)
				}
			}
		}
	}
	return nil
}

// DereferencedParams returns the parameter names any template actually reads,
// sorted. Every one must be required in the emitted XRD so a missing value
// fails at the XR gate rather than rendering the literal string "<no value>".
func (b *Blueprint) DereferencedParams() []string {
	seen := map[string]bool{}
	for _, r := range b.Spec.Resources {
		for _, f := range r.Fields {
			if param, ok := strings.CutPrefix(f.From, "params."); ok && f.From != "" {
				seen[param] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/blueprint/ -v`
Expected: PASS, all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/blueprint/
git commit -m "feat(blueprint): typed IR with validation and parameter reference checks"
```

---

## Task 8: Deterministic YAML writer and the XRD emitter

**Files:**
- Create: `internal/emit/yaml.go`, `internal/emit/xrd.go`
- Test: `internal/emit/xrd_test.go`

**Interfaces:**
- Consumes: `blueprint.Blueprint`.
- Produces:
  - `type Doc struct { buf bytes.Buffer }` with `func NewDoc() *Doc`, `(*Doc).Line(indent int, format string, args ...any)`, `(*Doc).Comment(format string, args ...any)`, `(*Doc).Bytes() []byte`
  - `func XRD(b *blueprint.Blueprint) ([]byte, error)`

**Background:** determinism is a correctness requirement (§8 of the spec). `Doc` guarantees LF endings, no trailing whitespace and exactly one trailing newline. Provenance is a comment, never an annotation.

- [ ] **Step 1: Write the failing test**

`internal/emit/xrd_test.go`:
```go
package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func testBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"location":       {Type: "string", Required: true, Enum: []string{"EU", "US"}},
					"providerName":   {Type: "string", Required: true},
					"maxMessageSize": {Type: "integer"},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"maxMessageSize": {From: "params.maxMessageSize"},
				},
			}},
		},
	}
}

func TestXRDShape(t *testing.T) {
	got, err := XRD(testBlueprint())
	if err != nil {
		t.Fatalf("XRD: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"apiVersion: apiextensions.crossplane.io/v2",
		"kind: CompositeResourceDefinition",
		"name: xqueues.platform.sparky.ee",
		"scope: Namespaced",
		"referenceable: true",
		"enum:",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q\n---\n%s", want, s)
		}
	}
	if strings.Contains(s, "LegacyCluster") {
		t.Error("output contains LegacyCluster, which is not valid in v2")
	}
}

// A parameter the template dereferences must be required even if the blueprint
// did not mark it so — otherwise a missing value renders "<no value>".
func TestXRDMarksDereferencedParamsRequired(t *testing.T) {
	got, _ := XRD(testBlueprint())
	s := string(got)
	if !strings.Contains(s, "required: [location, maxMessageSize, providerName]") {
		t.Errorf("required list wrong; maxMessageSize is dereferenced so it must be required\n---\n%s", s)
	}
}

func TestOutputIsDeterministicAndClean(t *testing.T) {
	a, _ := XRD(testBlueprint())
	b, _ := XRD(testBlueprint())
	if !bytes.Equal(a, b) {
		t.Error("two runs produced different bytes; output must be deterministic")
	}
	if bytes.Contains(a, []byte("\r")) {
		t.Error("output contains CR; must be LF only")
	}
	if !bytes.HasSuffix(a, []byte("\n")) || bytes.HasSuffix(a, []byte("\n\n")) {
		t.Error("output must end with exactly one newline")
	}
	for i, line := range bytes.Split(a, []byte("\n")) {
		if len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

func TestProvenanceIsACommentNotAnAnnotation(t *testing.T) {
	got, _ := XRD(testBlueprint())
	s := string(got)
	if !strings.Contains(s, "# Generated by compositionfactory") {
		t.Error("missing the do-not-edit provenance comment")
	}
	if strings.Contains(s, "annotations:") {
		t.Error("provenance must not be an annotation: it causes a perpetual ArgoCD sync loop")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emit/ -v`
Expected: FAIL — `undefined: XRD`.

- [ ] **Step 3: Write minimal implementation**

`internal/emit/yaml.go`:
```go
// Package emit turns a blueprint into the YAML artifacts Crossplane consumes.
// Output is byte-deterministic: a churning file on a prune:true GitOps repo is
// a live-cluster incident, so determinism is a correctness requirement.
package emit

import (
	"bytes"
	"fmt"
	"strings"
)

// Doc accumulates YAML lines with guaranteed LF endings, no trailing
// whitespace, and exactly one trailing newline.
type Doc struct{ buf bytes.Buffer }

func NewDoc() *Doc { return &Doc{} }

// Line writes one line at the given indent level (two spaces per level).
func (d *Doc) Line(indent int, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if text == "" {
		d.buf.WriteByte('\n')
		return
	}
	d.buf.WriteString(strings.Repeat("  ", indent))
	d.buf.WriteString(strings.TrimRight(text, " \t"))
	d.buf.WriteByte('\n')
}

// Comment writes a top-level comment line.
func (d *Doc) Comment(format string, args ...any) {
	d.Line(0, "# "+fmt.Sprintf(format, args...))
}

// Bytes returns the document, normalised to exactly one trailing newline.
func (d *Doc) Bytes() []byte {
	out := bytes.ReplaceAll(d.buf.Bytes(), []byte("\r\n"), []byte("\n"))
	return append(bytes.TrimRight(out, "\n"), '\n')
}

// header is the provenance every generated file carries, as comments.
func header(d *Doc, source string) {
	d.Comment("Generated by compositionfactory. Do not edit.")
	d.Comment("Source: %s", source)
	d.Comment("Regenerate with: cf gen")
}
```

`internal/emit/xrd.go`:
```go
package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// XRD renders the CompositeResourceDefinition for b.
func XRD(b *blueprint.Blueprint) ([]byte, error) {
	x := b.Spec.XRD
	if x.Scope == "LegacyCluster" {
		return nil, fmt.Errorf("scope LegacyCluster is not valid in apiextensions.crossplane.io/v2")
	}
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Line(0, "apiVersion: apiextensions.crossplane.io/v2")
	d.Line(0, "kind: CompositeResourceDefinition")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s.%s", x.Plural, x.Group)
	d.Line(0, "spec:")
	d.Line(1, "group: %s", x.Group)
	d.Line(1, "names:")
	d.Line(2, "kind: %s", x.Kind)
	d.Line(2, "plural: %s", x.Plural)
	// Always explicit: the API server defaults an omitted scope to Namespaced
	// while `crossplane xrd convert` defaults it to LegacyCluster.
	d.Line(1, "scope: %s", x.Scope)
	d.Line(1, "versions:")
	d.Line(1, "- name: %s", x.Version)
	d.Line(2, "served: true")
	d.Line(2, "referenceable: true")
	d.Line(2, "schema:")
	d.Line(3, "openAPIV3Schema:")
	d.Line(4, "type: object")
	d.Line(4, "properties:")
	d.Line(5, "spec:")
	d.Line(6, "type: object")
	d.Line(6, "properties:")

	names := make([]string, 0, len(x.Parameters))
	for n := range x.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := x.Parameters[n]
		d.Line(7, "%s:", n)
		d.Line(8, "type: %s", p.Type)
		if p.Description != "" {
			d.Line(8, "description: %s", p.Description)
		}
		if len(p.Enum) > 0 {
			d.Line(8, "enum:")
			for _, e := range p.Enum {
				d.Line(8, "- %s", e)
			}
		}
		if p.Type == "object" {
			d.Line(8, "additionalProperties:")
			d.Line(9, "type: string")
		}
	}
	if req := requiredParams(b); len(req) > 0 {
		d.Line(6, "required: [%s]", strings.Join(req, ", "))
	}
	d.Comment("Every parameter the template dereferences is required above, so a")
	d.Comment("missing value fails at the XR gate instead of rendering \"<no value>\".")
	return d.Bytes(), nil
}

// requiredParams unions the explicitly-required parameters with every parameter
// the templates actually read.
func requiredParams(b *blueprint.Blueprint) []string {
	seen := map[string]bool{}
	for n, p := range b.Spec.XRD.Parameters {
		if p.Required {
			seen[n] = true
		}
	}
	for _, n := range b.DereferencedParams() {
		if _, ok := b.Spec.XRD.Parameters[n]; ok {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emit/ -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/emit/yaml.go internal/emit/xrd.go internal/emit/xrd_test.go
git commit -m "feat(emit): deterministic YAML writer and XRD emitter"
```

---

## Task 9: The Composition emitter

**Files:**
- Create: `internal/emit/composition.go`
- Test: `internal/emit/composition_test.go`

**Interfaces:**
- Consumes: `blueprint.Blueprint`, `[]schema.CRD` (to resolve a resource's `Kind` to an apiVersion and to check the envelope).
- Produces: `func Composition(b *blueprint.Blueprint, crds []schema.CRD) ([]byte, error)`

**Background the implementer must not get wrong:**
- `options: ["missingkey=error"]` is a **top-level** field of the function input, a sibling of `inline:`. Nesting it under `inline:` is a fatal error. Without it, a missing field renders the literal string `<no value>` into a live resource and every validation gate still exits 0.
- For a `Namespaced` XRD, resolve the `.m.` variant of the kind.
- `providerConfigRef` in the v2 namespaced envelope requires **both** `kind` and `name`.
- Never emit `deletionPolicy` for a namespaced MR — it is not in that envelope and gets pruned.
- Optional fields are wrapped in `{{- with }}` so an unset value omits the key rather than writing an empty one.

- [ ] **Step 1: Write the failing test**

`internal/emit/composition_test.go`:
```go
package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func testCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  maxMessageSize: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties: {region: {type: string}}
              deletionPolicy: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

func TestCompositionSelectsNamespacedVariant(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "apiVersion: sqs.aws.m.upbound.io/v1beta1") {
		t.Errorf("did not select the .m. namespaced variant\n---\n%s", s)
	}
	if strings.Contains(s, "apiVersion: sqs.aws.upbound.io/v1beta1") {
		t.Error("emitted the legacy cluster-scoped variant for a Namespaced XRD")
	}
}

// The single most important assertion in this package.
func TestOptionsIsTopLevelNotNestedUnderInline(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	lines := strings.Split(string(got), "\n")
	var optIndent, inlineIndent int = -1, -1
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		indent := len(l) - len(trimmed)
		if strings.HasPrefix(trimmed, "options:") && optIndent == -1 {
			optIndent = indent
		}
		if strings.HasPrefix(trimmed, "inline:") && inlineIndent == -1 {
			inlineIndent = indent
		}
	}
	if optIndent == -1 {
		t.Fatal("no options: key; missingkey=error must always be emitted")
	}
	if inlineIndent == -1 {
		t.Fatal("no inline: key")
	}
	if optIndent != inlineIndent {
		t.Errorf("options: is indented %d and inline: is %d — options must be a SIBLING of inline, "+
			"not nested inside it (nesting is a fatal error at runtime)", optIndent, inlineIndent)
	}
	if !strings.Contains(string(got), "missingkey=error") {
		t.Error("missingkey=error missing; without it a missing field renders the string <no value>")
	}
}

func TestOptionalFieldIsGuarded(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	s := string(got)
	if !strings.Contains(s, "{{- with $spec.maxMessageSize }}") {
		t.Errorf("optional field not wrapped in a with-guard\n---\n%s", s)
	}
}

func TestProviderConfigRefCarriesKindAndName(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	s := string(got)
	if !strings.Contains(s, "kind: ClusterProviderConfig") || !strings.Contains(s, "name: {{ $spec.providerName }}") {
		t.Errorf("providerConfigRef must carry both kind and name in the v2 namespaced envelope\n---\n%s", s)
	}
}

func TestNoDeletionPolicyForNamespacedMR(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	if strings.Contains(string(got), "deletionPolicy") {
		t.Error("deletionPolicy is absent from the v2 namespaced envelope and would be pruned")
	}
}

func TestResourceNameAnnotationPresent(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	if !strings.Contains(string(got), `setResourceNameAnnotation "main-queue"`) {
		t.Error("every composed resource needs a stable composition-resource-name annotation")
	}
}

func TestUnknownKindIsAClearError(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Kind = "Nonexistent"
	_, err := Composition(b, testCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "Nonexistent") {
		t.Fatalf("err = %v, want an error naming the unknown kind", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emit/ -run Composition -v`
Expected: FAIL — `undefined: Composition`.

- [ ] **Step 3: Write minimal implementation**

`internal/emit/composition.go`:
```go
package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Composition renders the Composition for b, resolving each resource's kind
// against crds.
func Composition(b *blueprint.Blueprint, crds []schema.CRD) ([]byte, error) {
	x := b.Spec.XRD
	wantNamespaced := x.Scope == "Namespaced"

	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Line(0, "apiVersion: apiextensions.crossplane.io/v1")
	d.Line(0, "kind: Composition")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s.%s", x.Plural, x.Group)
	d.Line(0, "spec:")
	d.Line(1, "compositeTypeRef:")
	d.Line(2, "apiVersion: %s/%s", x.Group, x.Version)
	d.Line(2, "kind: %s", x.Kind)
	d.Line(1, "mode: Pipeline")
	d.Line(1, "pipeline:")
	d.Line(1, "- step: render-resources")
	d.Line(2, "functionRef:")
	d.Line(3, "name: function-go-templating")
	d.Line(2, "input:")
	d.Line(3, "apiVersion: gotemplating.fn.crossplane.io/v1beta1")
	d.Line(3, "kind: GoTemplate")
	d.Line(3, "source: Inline")
	// options is a SIBLING of inline. Nesting it under inline is a fatal error,
	// and without it a missing field renders the literal string "<no value>".
	d.Line(3, `options: ["missingkey=error"]`)
	d.Line(3, "inline:")
	d.Line(4, "template: |")

	const ti = 5 // template body indent level
	d.Line(ti, "{{- $spec := .observed.composite.resource.spec -}}")
	d.Line(ti, "{{- $xr := .observed.composite.resource.metadata.name -}}")

	for _, r := range b.Spec.Resources {
		crd, err := resolveKind(crds, r.Kind, wantNamespaced)
		if err != nil {
			return nil, err
		}
		d.Line(ti, "---")
		d.Line(ti, "apiVersion: %s", crd.APIVersion())
		d.Line(ti, "kind: %s", crd.Kind)
		d.Line(ti, "metadata:")
		d.Line(ti, "  annotations:")
		d.Line(ti, "    {{ setResourceNameAnnotation %q }}", r.Name)
		d.Line(ti, "spec:")
		d.Line(ti, "  forProvider:")
		if err := writeFields(d, ti+2, r, b); err != nil {
			return nil, err
		}
		// The v2 namespaced envelope requires both kind and name here.
		if wantNamespaced {
			d.Line(ti, "  providerConfigRef:")
			d.Line(ti, "    kind: ClusterProviderConfig")
			d.Line(ti, "    name: {{ $spec.providerName }}")
		}
	}

	d.Line(1, "- step: auto-ready")
	d.Line(2, "functionRef:")
	d.Line(3, "name: function-auto-ready")
	return d.Bytes(), nil
}

// writeFields emits the forProvider body for one resource, sorted for determinism.
func writeFields(d *Doc, indent int, r blueprint.Resource, b *blueprint.Blueprint) error {
	paths := make([]string, 0, len(r.Fields))
	for p := range r.Fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		f := r.Fields[p]
		switch {
		case f.Value != "":
			d.Line(indent, "%s: %s", p, f.Value)
		case f.Raw != "":
			d.Line(indent, "%s: %s", p, f.Raw)
		case f.From != "":
			param := strings.TrimPrefix(f.From, "params.")
			decl, ok := b.Spec.XRD.Parameters[param]
			if !ok {
				return fmt.Errorf("resource %q field %q: unknown parameter %q", r.Name, p, param)
			}
			expr := "$spec." + param
			if decl.Required {
				d.Line(indent, "%s: {{ %s }}", p, expr)
				continue
			}
			// Optional: omit the key entirely when unset rather than writing empty.
			d.Line(indent, "{{- with %s }}", expr)
			d.Line(indent, "%s: {{ . }}", p)
			d.Line(indent, "{{- end }}")
		}
	}
	return nil
}

// resolveKind finds the CRD for kind, preferring the scope the XRD needs. For a
// Namespaced XRD that is the ".m." group variant; the legacy cluster-scoped one
// has a different spec envelope and its fields get pruned.
func resolveKind(crds []schema.CRD, kind string, wantNamespaced bool) (schema.CRD, error) {
	var fallback *schema.CRD
	for i := range crds {
		c := crds[i]
		if c.Kind != kind || !c.IsManaged() {
			continue
		}
		if c.Namespaced() == wantNamespaced {
			return c, nil
		}
		fallback = &crds[i]
	}
	scope := "cluster-scoped"
	if wantNamespaced {
		scope = "namespaced"
	}
	if fallback != nil {
		return schema.CRD{}, fmt.Errorf("kind %q: no %s variant found (only %s in %s); "+
			"a %s XRD needs the matching variant", kind, scope, fallback.Scope, fallback.Group, scope)
	}
	return schema.CRD{}, fmt.Errorf("kind %q not found in any cached provider; run cf provider add", kind)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emit/ -v`
Expected: PASS, all tests including the earlier XRD ones.

- [ ] **Step 5: Commit**

```bash
git add internal/emit/composition.go internal/emit/composition_test.go
git commit -m "feat(emit): Composition emitter with .m. selection and missingkey=error"
```

---

## Task 10: functions.yaml, `Generate`, and `cf gen --check`

**Files:**
- Create: `internal/emit/functions.go`, `internal/emit/emit.go`, `cmd/cf/gen.go`
- Modify: `cmd/cf/main.go` — add the `Gen` field to `CLI`
- Test: `internal/emit/emit_test.go`, `cmd/cf/gen_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `func Functions(b *blueprint.Blueprint) ([]byte, error)`
  - `type Output struct { Path string; Body []byte }`
  - `func Generate(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error)` — **the single entry point the HTTP and MCP front doors will also call**
  - `type GenCmd struct{ ... }` with `--check`

**Background:** `functions.yaml` is a **required** third argument to `crossplane composition render` (`error: functions argument is required when not in a project`), so it is a generated artifact, not an afterthought. `render.crossplane.io/*` annotations are **not** required — verified — but we emit `runtime-docker-name` because renders otherwise leak containers. `--check` uses distinct exit codes so CI can tell a crash from drift.

- [ ] **Step 1: Write the failing test**

`internal/emit/emit_test.go`:
```go
package emit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionsYAMLListsBothFunctions(t *testing.T) {
	got, err := Functions(testBlueprint())
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"kind: Function",
		"name: function-go-templating",
		"name: function-auto-ready",
		"render.crossplane.io/runtime-docker-name",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("functions.yaml missing %q\n---\n%s", want, s)
		}
	}
	if strings.Count(s, "kind: Function") != 2 {
		t.Errorf("want exactly 2 Function documents, got %d", strings.Count(s, "kind: Function"))
	}
}

func TestGenerateProducesThreeFilesAtStablePaths(t *testing.T) {
	outs, err := Generate(testBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := map[string]bool{}
	for _, o := range outs {
		got[filepath.ToSlash(o.Path)] = true
	}
	for _, want := range []string{
		"out/xrds/xqueues.platform.sparky.ee.yaml",
		"out/compositions/xqueues.platform.sparky.ee.yaml",
		"out/functions.yaml",
	} {
		if !got[want] {
			t.Errorf("missing output %q; got %v", want, got)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, _ := Generate(testBlueprint(), testCRDs(t), "out")
	b, _ := Generate(testBlueprint(), testCRDs(t), "out")
	if len(a) != len(b) {
		t.Fatalf("different file counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || string(a[i].Body) != string(b[i].Body) {
			t.Fatalf("output %q differs between runs", a[i].Path)
		}
	}
}
```

`cmd/cf/gen_test.go`:
```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const genBlueprint = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xqueue}
spec:
  sources:
    - provider: example.org/provider-test:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

const genCRDs = `[{"Group":"sqs.aws.m.upbound.io","Kind":"Queue","Plural":"queues","Scope":"Namespaced","Categories":["managed"],"Versions":[{"Name":"v1beta1","Served":true,"Storage":true,"Properties":null}]}]`

// seed writes a blueprint and a pre-populated schema cache.
func seed(t *testing.T) (dir, bpPath, cacheDir string) {
	t.Helper()
	dir = t.TempDir()
	bpPath = filepath.Join(dir, "xqueue.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(genBlueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir = filepath.Join(dir, "cache")
	pdir := filepath.Join(cacheDir, "example.org_provider-test_v2")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "crds.json"), []byte(genCRDs), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, bpPath, cacheDir
}

func TestGenWritesFiles(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	cmd := &GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := cmd.Run(&buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range []string{
		"xrds/xqueues.platform.sparky.ee.yaml",
		"compositions/xqueues.platform.sparky.ee.yaml",
		"functions.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestGenCheckExitCodes(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer

	// Generate once so the tree is in sync.
	if err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatal(err)
	}
	// In sync -> code 0.
	code, err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if err != nil || code != 0 {
		t.Fatalf("in-sync check: code=%d err=%v, want 0/nil", code, err)
	}
	// Hand-edit a generated file -> drift, code 2.
	target := filepath.Join(out, "functions.yaml")
	if err := os.WriteFile(target, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ = (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if code != 2 {
		t.Errorf("drift check: code=%d, want 2 (distinct from 1, which means the tool failed)", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emit/ ./cmd/cf/ -v`
Expected: FAIL — `undefined: Functions`, `undefined: GenCmd`.

- [ ] **Step 3: Write minimal implementation**

`internal/emit/functions.go`:
```go
package emit

import "github.com/koorikla/compositionfactory/internal/blueprint"

// fn is one pipeline function and the package that provides it.
type fn struct{ name, pkg string }

// M1 pins the versions verified against Crossplane v2.4.0.
var defaultFunctions = []fn{
	{"function-go-templating", "xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0"},
	{"function-auto-ready", "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"},
}

// Functions renders functions.yaml, the required third argument to
// `crossplane composition render`.
func Functions(b *blueprint.Blueprint) ([]byte, error) {
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Comment("Required by: crossplane composition render <xr> <composition> functions.yaml")
	d.Comment("No render.crossplane.io/runtime annotation is needed to render; the")
	d.Comment("docker-name annotation below only makes renders reuse one container")
	d.Comment("per function instead of leaking a new one on every run.")
	for i, f := range defaultFunctions {
		if i > 0 {
			d.Line(0, "---")
		}
		d.Line(0, "apiVersion: pkg.crossplane.io/v1")
		d.Line(0, "kind: Function")
		d.Line(0, "metadata:")
		d.Line(1, "name: %s", f.name)
		d.Line(1, "annotations:")
		d.Line(2, "render.crossplane.io/runtime-docker-name: cf-%s", f.name)
		d.Line(0, "spec:")
		d.Line(1, "package: %s", f.pkg)
	}
	return d.Bytes(), nil
}
```

`internal/emit/emit.go`:
```go
package emit

import (
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Output is one generated file.
type Output struct {
	Path string
	Body []byte
}

// Generate renders every artifact for b into outDir. This is the ONLY entry
// point: the CLI, the HTTP server and the MCP server all call it, so a
// UI-authored artifact is always reproducible from the CLI.
func Generate(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error) {
	name := b.Spec.XRD.Plural + "." + b.Spec.XRD.Group + ".yaml"

	xrd, err := XRD(b)
	if err != nil {
		return nil, err
	}
	comp, err := Composition(b, crds)
	if err != nil {
		return nil, err
	}
	fns, err := Functions(b)
	if err != nil {
		return nil, err
	}
	// Sorted by path so callers can diff two runs positionally.
	return []Output{
		{Path: filepath.Join(outDir, "compositions", name), Body: comp},
		{Path: filepath.Join(outDir, "functions.yaml"), Body: fns},
		{Path: filepath.Join(outDir, "xrds", name), Body: xrd},
	}, nil
}
```

`cmd/cf/gen.go`:
```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// GenCmd renders a blueprint to YAML on disk.
type GenCmd struct {
	Blueprint string `arg:"" help:"Path to the blueprint file."`
	Out       string `short:"o" help:"Output directory." default:"."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
	Check     bool   `help:"Do not write. Exit 0 if in sync, 2 if the tree has drifted."`
}

func (c *GenCmd) Run(out io.Writer) error {
	code, err := c.run(out)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// run returns the intended exit code so tests can assert it without exiting.
// 0 = in sync or written, 1 = tool error (returned as err), 2 = drift.
func (c *GenCmd) run(out io.Writer) (int, error) {
	b, err := blueprint.Load(c.Blueprint)
	if err != nil {
		return 1, err
	}
	store := cache.New(c.CacheDir)
	var crds []schema.CRD
	for _, s := range b.Spec.Sources {
		got, err := store.Load(s.Provider)
		if err != nil {
			return 1, err
		}
		crds = append(crds, got...)
	}
	outputs, err := emit.Generate(b, crds, c.Out)
	if err != nil {
		return 1, err
	}

	if c.Check {
		drift := false
		for _, o := range outputs {
			existing, err := os.ReadFile(o.Path)
			if err != nil || !bytes.Equal(existing, o.Body) {
				fmt.Fprintf(out, "drift: %s\n", o.Path)
				drift = true
			}
		}
		if drift {
			fmt.Fprintln(out, "generated output is stale; run: cf gen")
			return 2, nil
		}
		fmt.Fprintln(out, "in sync")
		return 0, nil
	}

	for _, o := range outputs {
		if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(o.Path, o.Body, 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(out, "wrote %s\n", o.Path)
	}
	return 0, nil
}
```

In `cmd/cf/main.go` add to `CLI`:
```go
	Gen GenCmd `cmd:"" help:"Generate XRD, Composition and functions.yaml from a blueprint."`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -short -v`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/emit/functions.go internal/emit/emit.go internal/emit/emit_test.go cmd/cf/gen.go cmd/cf/gen_test.go cmd/cf/main.go
git commit -m "feat(cli): cf gen with functions.yaml and --check drift exit codes"
```

---

## Task 11: The XQueue acceptance test

**Files:**
- Create: `testdata/xqueue.cf.yaml`, `testdata/xr.yaml`, `acceptance_test.go`
- Test: `acceptance_test.go`

**Interfaces:**
- Consumes: the whole binary.
- Produces: nothing — this is the M1 exit gate.

**Background:** `crossplane composition render` is byte-for-byte deterministic (verified: three runs to an identical SHA-256), so a golden comparison is safe. It needs Docker, so this test is skipped under `-short` and gated on a Docker probe. This is Lane B.

- [ ] **Step 1: Write the failing test**

`testdata/xqueue.cf.yaml`:
```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {value: "eu-north-1"}
        maxMessageSize: {from: params.maxMessageSize}
```

`testdata/xr.yaml`:
```yaml
apiVersion: platform.sparky.ee/v1alpha1
kind: XQueue
metadata:
  name: demo
  namespace: default
spec:
  location: EU
  providerName: localstack
  maxMessageSize: 2048
```

`acceptance_test.go` (at the repo root):
```go
package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireTool skips the test when a binary or daemon is unavailable, so Lane A
// stays green on runners without Docker.
func requireTool(t *testing.T, name string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
	if len(args) > 0 {
		if err := exec.Command(name, args...).Run(); err != nil {
			t.Skipf("%s %v failed: %v", name, args, err)
		}
	}
}

func TestAcceptanceXQueueRenders(t *testing.T) {
	if testing.Short() {
		t.Skip("acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()
	bin := filepath.Join(dir, "cf")
	if out, err := exec.Command("go", "build", "-o", bin, "./cmd/cf").CombinedOutput(); err != nil {
		t.Fatalf("build cf: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(dir, "cache")
	lock := filepath.Join(dir, ".cf.lock")
	ref := "xpkg.upbound.io/upbound/provider-aws-sqs:v2"

	// Step 1: fetch the provider. No cluster, no Docker.
	add := exec.Command(bin, "provider", "add", ref, "--cache-dir", cacheDir, "--lock", lock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("cf provider add: %v\n%s", err, out)
	}

	// Step 2: generate.
	outDir := filepath.Join(dir, "out")
	gen := exec.Command(bin, "gen", "testdata/xqueue.cf.yaml", "-o", outDir, "--cache-dir", cacheDir)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cf gen: %v\n%s", err, out)
	}

	// Step 3: --check must report in sync immediately after generating.
	chk := exec.Command(bin, "gen", "testdata/xqueue.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	// Step 4: render what we generated.
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	render := exec.Command("crossplane", "composition", "render",
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	rendered, err := render.CombinedOutput()
	if err != nil {
		t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
	}
	got := string(rendered)

	for _, want := range []string{
		"apiVersion: sqs.aws.m.upbound.io/v1beta1",
		"kind: Queue",
		"maxMessageSize: 2048",
		"kind: ClusterProviderConfig",
		"name: localstack",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, got)
		}
	}

	// The defect class that passes every other gate: a legal string that is wrong.
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(got, bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}

	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile("testdata/xqueue.render.golden.yaml", rendered, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run Acceptance -v`
Expected: FAIL — the blueprint or the generated output does not yet render. (If Docker is absent it SKIPS; that is the intended Lane A behaviour, not a pass.)

- [ ] **Step 3: Make it pass**

No new production code should be needed. If it fails, the failure is a real defect in Tasks 8–10 — fix it there rather than adapting the test. Two failures are expected and instructive:
- `region` must be present, since it is the only OpenAPI-required field on the AWS SQS Queue. The blueprint sets it literally; if you removed it, render fails.
- If `options` was nested under `inline`, render fails outright. That is the constraint the emitter test already guards.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-docker`
Expected: PASS. First run pulls function images and may take minutes; later runs are seconds.

- [ ] **Step 5: Commit**

```bash
git add testdata/ acceptance_test.go
git commit -m "test: end-to-end acceptance — provider add, gen, render"
```

- [ ] **Step 6: Add CI Lane B**

Append to `.github/workflows/ci.yml`:
```yaml
  # Lane B needs a Docker daemon; crossplane render runs the engine in containers.
  acceptance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with: { go-version: '1.25' }
      - name: Install crossplane CLI
        run: |
          curl -sL https://raw.githubusercontent.com/crossplane/crossplane/main/install.sh | sh
          sudo mv crossplane /usr/local/bin/
      - run: make test-docker
```

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Lane B acceptance job"
```

---

## M1 Exit Criteria

- [ ] `make test` passes with no Docker and no cluster.
- [ ] `make test-docker` renders the XQueue and finds no `<no value>`.
- [ ] `cf gen --check` exits 0 immediately after `cf gen`, and 2 after a hand-edit.
- [ ] Every generated file opens with a `# Generated by compositionfactory` comment and carries no annotations.
- [ ] `.cf.lock` pins the provider digest.

## Not in M1 (later plans)

XRD field editor and the schema API (M2) · canvas, wires, reference inference (M3) · `when`, `forEach`, `dependsOn`, user-defined templates, multi-step pipelines (M4) · MCP server, `adopt`, K8s RBAC emission (M5) · cloud IAM (M6).
