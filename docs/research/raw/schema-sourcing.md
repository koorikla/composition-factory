Cleanup done. I have everything — writing the brief.

```
Scratchpad artifacts (all absolute):
  /private/tmp/.../scratchpad/xpkgget/main.go      working Go extractor
  /private/tmp/.../scratchpad/fakereg/main.go      logging OCI registry (proof harness)
  /private/tmp/.../scratchpad/ext/*.yaml           extracted package streams
```

# Obtaining MR CRD schemas from ANY Crossplane provider, without a cluster

## Decisions this enables

1. **Pull one layer, not one image.** Every xpkg marks its package layer in the image-config labels; fetching it costs **5 HTTP requests / ~28 KB** for provider-aws-sqs versus **271,482,755 bytes** for the full image (~9,600x). VERIFIED with a request-logging registry. Never `docker pull`.
2. **go-containerregistry v0.22.0 is sufficient and idiomatic** — single static Go binary, no docker, no crane/oras/skopeo (oras and skopeo are *not* installed here; the tool must not depend on them). `authn.DefaultKeychain` + `remote.Get` + `LayerByDigest` is the whole recipe. Working code below, output byte-identical (same SHA-256) to `crossplane xpkg extract`.
3. **Anonymous pull works** on xpkg.upbound.io, xpkg.crossplane.io and ghcr.io. All return 401 on `/v2/` then grant a token to an anonymous request — that is normal bearer flow, not a credential requirement. VERIFIED: 796-char JWT issued with zero credentials.
4. **The family package must be pulled separately.** `provider-aws-sqs` contains zero ProviderConfig CRDs; `provider-family-aws` contains exactly 5. Nothing follows the dependency for you — `crossplane xpkg get-crds` does **not** resolve `dependsOn` (VERIFIED). The tool must read `spec.dependsOn` from the meta object and pull the family itself.
5. **Schemas cannot be shipped wholesale to a browser.** Full AWS family = **2,065 CRDs / 101,909,861 bytes** raw YAML; the largest *single* MR is **1,692,444 bytes** (firehose DeliveryStream). Keep schemas server-side, ship a small index + per-kind lazy fetch (a big CRD gzips to ~50-122 KB, which is a fine per-kind payload).

---

## 1. crossplane CLI v2.5.0 — actual subcommand tree

`crossplane --help` top level (VERIFIED, verbatim command list):

```
cluster        [BETA] Inspect a Crossplane cluster.
composition    Work with Crossplane Compositions.
config         View and update the crossplane CLI configuration file.
dependency     [BETA] Manage dependencies of control plane Projects.
function       [BETA] Work with functions in control plane Projects.
project        [BETA] Work with control plane Projects.
resource       [BETA] Work with Crossplane resources.
version        Print the client and server version information for the current context.
xpkg           Work with Crossplane packages.
xrd            [BETA] Work with Crossplane Composite Resource Definitions (XRDs).
completions    Get shell (bash/zsh/fish) completions.
```

There is **no `beta` subcommand** in v2.5.0 (it was flattened; `crossplane beta ...` errors with `unexpected argument beta`). VERIFIED.

`crossplane xpkg --help` (VERIFIED, verbatim):

```
xpkg batch       Batch build and push a family of provider packages.
xpkg build       Build a new package.
xpkg get-crds    Download CRDs from package dependencies.
xpkg init        Initialize a new package from a template.
xpkg install     Install a package in a control plane.
xpkg push        Push a package to a registry.
xpkg update      Update a package in a control plane.
xpkg extract     Extract package contents into a Crossplane cache compatible format. Fetches from a remote registry by default.
```

**`crossplane xpkg extract` EXISTS.** VERIFIED, verbatim:

```
Usage: crossplane xpkg extract [<package>] [flags]
Arguments:
  [<package>]    Name of the package to extract. Must be a valid and fully qualified OCI image tag or a path if using --from-xpkg.
Flags:
      --from-daemon        Fetch the image from the Docker daemon.
      --from-xpkg          Extract a local xpkg file. If package isn't specified, implies the only one in the current directory.
  -o, --output="out.gz"    Package output file. Extension must be .gz.
```

**`crossplane xpkg get-crds` is the "dump CRDs to a directory" command** you asked about. VERIFIED, verbatim flags:

```
Usage: crossplane xpkg get-crds <extensions> [flags]
Arguments:
  <extensions>    Extension sources as a comma-separated list of files, directories, or '-' for standard input.
Flags:
      --cache-dir="~/.crossplane/cache"  Absolute path to the cache directory holding downloaded schemas.
      --clean-cache                      Clean the cache directory before downloading package schemas.
      --crossplane-image=STRING          Specify the Crossplane image for fetching the built-in schemas.
      --flat                             Write files to a flat directory instead of organizing by group and version.
      --json-schema                      Write JSON Schema files instead of CRDs. Useful for YAML language server integration.
      --no-cache                         Disable caching entirely.
  -o, --output-dir="."                   Directory that receives the CRD or JSON Schema files.
      --update-cache                     Update cached schemas by downloading the latest version that satisfies a constraint.
```

`xpkg init` / `build` / `push` / `install` / `update` / `batch` all exist. There is **no `xpkg login`/`logout`** — auth is taken from the local docker config (`xpkg push --help`: "uses registry credentials from the local docker configuration"). VERIFIED.

`crossplane xrd` has `xrd convert` (XRD→CRD) and `xrd generate` (XR/SimpleSchema→XRD) — relevant to your XRD-generation goal but out of scope here.

**get-crds behaviour (VERIFIED).** Fed a `pkg.crossplane.io/v1 Provider` referencing `provider-aws-sqs:v2`, it wrote 8 files in 0.29 s:

```
crds/sqs.aws.m.upbound.io/v1beta1/{queue,queuepolicy,queueredrivepolicy,queueredriveallowpolicy}.yaml
crds/sqs.aws.upbound.io/v1beta1/{queue,queuepolicy,queueredrivepolicy,queueredriveallowpolicy}.yaml
```

Layout is `<group>/<version>/<kind-lowercased>.{yaml|json}`. `--json-schema` emits JSON Schema with `"$id": "sqs.aws.m.upbound.io/v1beta1/queuepolicy.json"`. **It did not pull `provider-family-aws`** — no family entry appeared in `~/.crossplane/cache` afterwards. So despite the name "from package dependencies", `dependsOn` is not traversed.

**Verdict for your tool:** don't shell out to the CLI. `extract` needs a `.gz` file round-trip, `get-crds` misses dependencies, and both require the binary to be installed. But `~/.crossplane/cache`'s layout is worth copying (§7).

---

## 2. Pulling and extracting without a cluster

### (a) `crossplane xpkg extract` — WORKS

```
$ time crossplane xpkg extract xpkg.upbound.io/upbound/provider-aws-sqs:v2 -o sqs.gz
1.817 total     # exit 0, 18,635 bytes
```

Anonymous, no cluster, no docker. Output is **gzip of the raw `package.yaml`** — it strips the tar wrapper. `gunzip -c sqs.gz` → 182,766 bytes of multi-document YAML. VERIFIED.

### (b) docker pull + save + untar — WORKS, but absurdly expensive

```
$ time docker pull --platform linux/amd64 xpkg.upbound.io/upbound/provider-aws-sqs:v2   → 14.8 s
$ docker images ...provider-aws-sqs:v2                                                  → 2.52 GB on disk
$ docker save ... -o img.tar                                                            → 501,960,704 bytes
$ tar -xf img.tar   # OCI layout: blobs/ index.json manifest.json oci-layout
# locate the payload:
FOUND in blobs/sha256/04115f40bbaf...  (20,071 bytes)  → package.yaml = 182,766 bytes
```

VERIFIED. Same digest, same payload as (a). **271 MB downloaded and 2.52 GB stored to obtain 182 KB.** Rejected.

### (c) crane / oras / skopeo

VERIFIED on this machine: `crane` = `/opt/homebrew/bin/crane` (present), **`oras` not found**, **`skopeo` not found**. I used `crane` only as an independent cross-check. **The tool must not depend on any of them** — none are guaranteed present, and (d) makes them unnecessary.

### (d) Pure Go with go-containerregistry — THE ANSWER

Module versions (VERIFIED): `github.com/google/go-containerregistry v0.22.0`, built with `go1.27.0 darwin/arm64`, static binary 11,019,714 bytes.

**A note on how this was verified.** Outbound TCP from freshly-compiled binaries is blocked in this sandbox (a trivial `http.Get` to the same host that `curl` reaches fine times out; localhost is unaffected). So rather than weaken that, I mirrored the **real registry bytes** with crane — the index, the amd64 child manifest, the config blob and the base layer — into a local OCI-distribution server that logs every request, and **deliberately did not mirror the 270 MB runtime layer**, so any attempt to touch it would 404 loudly. That is a stronger proof of the "base layer only" claim than hitting the real registry would have been. Result:

```
ref            = localhost:5555/upbound/provider-aws-sqs:v2
manifest type  = application/vnd.oci.image.index.v1+json
image digest   = sha256:1aff5a5aa39ec5c103782c098fe28a2774793e68c1419bc450a26c0a361e35f7
layers total   = 18
base layer     = sha256:04115f40bbaf016f4e530ef00fc2b7d2171061d71a1d4f243b1970985c44cc98
                 (config label io.crossplane.xpkg:<digest>=base)
base layer B   = 20071 compressed
stream bytes   = 182766 uncompressed
image bytes    = 271482755 (all layers, NOT downloaded)

REGISTRY LOG — every request the extractor made:
  GET /v2/
  GET /v2/upbound/provider-aws-sqs/manifests/v2
  GET /v2/upbound/provider-aws-sqs/manifests/sha256:1aff5a5a...
  GET /v2/upbound/provider-aws-sqs/blobs/sha256:fa49d78b...   → 4,144 bytes (config)
  GET /v2/upbound/provider-aws-sqs/blobs/sha256:04115f40...   → 20,071 bytes (base layer)

sha256 of extracted stream == sha256 of `crossplane xpkg extract` output:
  0226e8e0a91265ae4066f858810b64a79e2b6595473c62af9a427cd603bb2baa  (both)
```

**5 requests, ~28 KB total.** Zero missing-blob warnings. Generality VERIFIED against three more packages, all also 5 requests:

| package | registry | layers | base layer | stream | full image |
|---|---|---|---|---|---|
| `function-go-templating:v0.12.0` | xpkg.upbound.io | 14 | 1,587 B | 4,272 B | 25,279,983 B |
| `provider-helm:v0.21.0` | xpkg.crossplane.io | 4 | 10,194 B | 77,418 B | 23,079,516 B |
| `provider-family-aws:v2.4.0` | xpkg.upbound.io | 18 | 10,511 B | 81,209 B | 271,132,644 B |

#### The working code, verbatim

`/private/tmp/.../scratchpad/xpkgget/main.go`:

```go
// xpkgget extracts the Crossplane package stream (package.yaml) from any xpkg
// OCI image, without Docker and without a cluster. It downloads ONLY the
// package ("base") layer -- never the provider runtime layer.
package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Per the Crossplane xpkg specification the package layer is identified either
// by an OCI layer annotation io.crossplane.xpkg=base, or (as Upbound and the
// crossplane CLI actually emit) by an image-config label whose KEY is
// "io.crossplane.xpkg:<compressed layer digest>" and whose VALUE is "base".
const (
	xpkgAnnotation = "io.crossplane.xpkg"
	baseRole       = "base"
	streamFile     = "package.yaml"
)

// Meta reports what the extractor observed about the image.
type Meta struct {
	Ref            string
	ManifestType   string
	ImageDigest    string
	LayerCount     int
	BaseDigest     string
	FoundVia       string
	BaseCompressed int64
	AllLayersBytes int64
}

// FetchPackageStream returns the raw multi-document YAML package stream.
func FetchPackageStream(ctx context.Context, ref string) ([]byte, Meta, error) {
	var m Meta
	m.Ref = ref

	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, m, fmt.Errorf("parse ref: %w", err)
	}

	// authn.DefaultKeychain reads ~/.docker/config.json (and $DOCKER_CONFIG),
	// including credential helpers such as docker-credential-osxkeychain.
	// Anonymous pulls simply fall through when no entry matches the registry.
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithUserAgent("xpkgget/0.1"),
	}

	desc, err := remote.Get(r, opts...)
	if err != nil {
		return nil, m, fmt.Errorf("get manifest: %w", err)
	}
	m.ManifestType = string(desc.MediaType)

	img, err := resolveImage(desc, opts)
	if err != nil {
		return nil, m, err
	}
	if d, err := img.Digest(); err == nil {
		m.ImageDigest = d.String()
	}

	mf, err := img.Manifest()
	if err != nil {
		return nil, m, fmt.Errorf("manifest: %w", err)
	}
	m.LayerCount = len(mf.Layers)
	for _, l := range mf.Layers {
		m.AllLayersBytes += l.Size
	}

	h, via, err := findBaseLayer(img, mf)
	if err != nil {
		return nil, m, err
	}
	m.BaseDigest, m.FoundVia = h.String(), via

	// LayerByDigest is lazy: only this one blob is fetched over the network.
	layer, err := img.LayerByDigest(h)
	if err != nil {
		return nil, m, fmt.Errorf("layer %s: %w", h, err)
	}
	if sz, err := layer.Size(); err == nil {
		m.BaseCompressed = sz
	}

	rc, err := layer.Uncompressed() // transparently gunzips
	if err != nil {
		return nil, m, fmt.Errorf("open layer: %w", err)
	}
	defer rc.Close()

	data, err := readStreamFromTar(rc)
	if err != nil {
		return nil, m, err
	}
	return data, m, nil
}

// resolveImage turns a descriptor into an image, handling multi-arch indexes.
// The xpkg base layer is byte-identical across architectures, so any child
// manifest yields the same package stream; we prefer linux/amd64 and otherwise
// take the first child.
func resolveImage(desc *remote.Descriptor, opts []remote.Option) (v1.Image, error) {
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("image index: %w", err)
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("index manifest: %w", err)
		}
		var pick *v1.Descriptor
		for i := range im.Manifests {
			d := im.Manifests[i]
			if d.MediaType.IsIndex() || d.MediaType.IsImage() {
				if d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == "amd64" {
					pick = &d
					break
				}
				if pick == nil {
					pick = &d
				}
			}
		}
		if pick == nil {
			return nil, errors.New("index has no image manifests")
		}
		return idx.Image(pick.Digest)
	default:
		return desc.Image()
	}
}

// findBaseLayer locates the package layer by config label first (what Upbound
// and the crossplane CLI emit), then by OCI layer annotation (the spec form).
func findBaseLayer(img v1.Image, mf *v1.Manifest) (v1.Hash, string, error) {
	cf, err := img.ConfigFile() // fetches only the small config blob
	if err == nil && cf != nil {
		for k, v := range cf.Config.Labels {
			if v != baseRole || !strings.HasPrefix(k, xpkgAnnotation+":") {
				continue
			}
			h, err := v1.NewHash(strings.TrimPrefix(k, xpkgAnnotation+":"))
			if err != nil {
				continue
			}
			return h, "config label io.crossplane.xpkg:<digest>=base", nil
		}
	}
	for _, l := range mf.Layers {
		if l.Annotations[xpkgAnnotation] == baseRole {
			return l.Digest, "OCI layer annotation io.crossplane.xpkg=base", nil
		}
	}
	return v1.Hash{}, "", errors.New("no layer marked io.crossplane.xpkg=base")
}

// readStreamFromTar pulls package.yaml out of the layer tarball.
func readStreamFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in base layer", streamFile)
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.TrimPrefix(hdr.Name, "./") != streamFile {
			continue
		}
		return io.ReadAll(tr)
	}
}
```

**Three details that matter and are easy to get wrong:**

- `img.ConfigFile()` fetches only the config blob (4,144 B here), not layers. `img.LayerByDigest(h)` returns a *lazy* handle; the blob is fetched on `Uncompressed()`. Calling `img.Layers()` and ranging is also lazy, but any code that calls `.Digest()`/`.DiffID()` on every layer will fetch every layer. Match by manifest digest instead.
- The digest in the label key is the **compressed** layer digest (matches `manifest.layers[].digest`), *not* the diffID. VERIFIED: `shasum -a 256` of the fetched blob = `04115f40…` (the label key); its diffID is `331989ba…` = `rootfs.diff_ids[11]`. Using diffIDs here silently fails to match.
- `layer.Uncompressed()` handles gzip for you; do not add your own `gzip.NewReader`.

---

## 3. The xpkg format, precisely

For `xpkg.upbound.io/upbound/provider-aws-sqs:v2` (VERIFIED throughout):

- **Top-level manifest** is `application/vnd.oci.image.index.v1+json` with **2** children: linux/amd64 (`sha256:1aff5a5a…`) and linux/arm64 (`sha256:88a10940…`), each `application/vnd.docker.distribution.manifest.v2+json`, 3,465 bytes.
- **18 layers** per child, all `application/vnd.docker.image.rootfs.diff.tar.gzip`. Total 271,482,755 bytes (amd64); the single runtime layer is 270,565,987 of that (arm64's is 230,330,596).
- **The base layer digest is byte-identical across both architectures** (`04115f40…`, 20,071 B) — so architecture selection is irrelevant for schema extraction, and the base digest is a stable cross-arch cache key.
- **Roles live in the image config `Labels`**, keyed by compressed layer digest:

```json
"io.crossplane.xpkg:sha256:04115f40…": "base",
"io.crossplane.xpkg:sha256:78b7f60c…": "schema.go",
"io.crossplane.xpkg:sha256:848 60816…": "schema.python",
"io.crossplane.xpkg:sha256:ac405a32…": "schema.kcl",
"io.crossplane.xpkg:sha256:b50bf21b…": "schema.json",
"io.crossplane.xpkg:sha256:ed182b7a…": "upbound"
```

- The **base layer is a POSIX tar** (184,320 B uncompressed) containing **exactly one file**: `package.yaml`, **182,766 bytes**.
- `package.yaml` holds **9 YAML documents**: **1** `meta.pkg.crossplane.io/v1 Provider` followed by **8** CustomResourceDefinitions. 3,764 lines. Compression ratio ~9.8:1.
- The 8 CRDs are exactly the doubling you already established:

```
queuepolicies.sqs.aws.m.upbound.io              Namespaced  QueuePolicy              v1beta1
queueredriveallowpolicies.sqs.aws.m.upbound.io  Namespaced  QueueRedriveAllowPolicy  v1beta1
queueredrivepolicies.sqs.aws.m.upbound.io       Namespaced  QueueRedrivePolicy       v1beta1
queues.sqs.aws.m.upbound.io                     Namespaced  Queue                    v1beta1
queuepolicies.sqs.aws.upbound.io                Cluster     QueuePolicy              v1beta1
queueredriveallowpolicies.sqs.aws.upbound.io    Cluster     QueueRedriveAllowPolicy  v1beta1
queueredrivepolicies.sqs.aws.upbound.io         Cluster     QueueRedrivePolicy       v1beta1
queues.sqs.aws.upbound.io                       Cluster     Queue                    v1beta1
```

**Other layers, worth knowing but not depending on.** `schema.json` (22,467 B) contains pre-generated JSON Schemas — `models/io-upbound-aws-sqs-v1beta1-Queue.schema.json` (19,212 B), `models/io-upbound-m-aws-sqs-v1beta1-Queue.schema.json` (17,534 B), plus k8s meta models. There are parallel `schema.go` / `schema.kcl` / `schema.python` layers, and an `upbound` layer holding `.up/examples.yaml` (9,944 B — ready-made example MRs). These are **Upbound/CLI conventions, not the spec**: `function-go-templating` and `function-auto-ready` ship only `base`. Treat `schema.json` and `upbound` as optional accelerators; derive everything you need from `base`.

**Spec vs. practice (DOCS — crossplane/contributing/specifications/xpkg.md).** The spec says one layer descriptor *MAY* carry annotation `io.crossplane.xpkg: base`, `package.yaml` *MUST* exist in the base layer root "**if distinguished, or in the root of the image filesystem after all layer changesets are applied**", and exactly one `Provider.meta.pkg.crossplane.io` *MUST* be in the stream — **with no ordering requirement**. Two consequences: parse the stream by `kind`, don't assume the meta object is document 0 (it is in practice, in all 12 packages I extracted); and keep a last-resort path that flattens layers when neither label nor annotation is present. I did not encounter such a package — all 4 registries/publishers tested use the config-label form.

---

## 4. Auth

**Anonymous pull works everywhere tested.** VERIFIED. My `~/.docker/config.json` has entries only for `ghcr.io` (empty) and Docker Hub, with `credsStore: desktop` — **no xpkg.upbound.io credentials at all** — and both `crossplane xpkg extract` and `crane` succeeded.

The 401 on `/v2/` is the normal bearer handshake, not a wall:

```
xpkg.upbound.io      /v2/ → 401   www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io"
registry.upbound.io  /v2/ → 401   (same realm — shares xpkg.upbound.io's token service)
ghcr.io              /v2/ → 401   www-authenticate: Bearer realm="https://ghcr.io/token",service="ghcr.io"
```

Anonymous token grant, VERIFIED end to end:

```
GET https://xpkg.upbound.io/service/token?service=xpkg.upbound.io&scope=repository:upbound/provider-aws-sqs:pull
  → 796-char JWT, no credentials sent
GET /v2/upbound/provider-aws-sqs/manifests/v2  with that token  → 200
GET /v2/upbound/provider-aws-sqs/manifests/v2  with no header    → 401
```

Note the realm path is `/service/token`, not `/v2/token` (that 404s). go-containerregistry follows `www-authenticate` automatically — you never hand-roll this.

`/v2/_catalog` is **UNAUTHORIZED even with an anonymous token** — you cannot enumerate repositories. But `crane ls` (tag listing) *is* anonymous: `provider-aws-sqs` has **446 tags**. So version discovery works; package *discovery* needs an out-of-band source (marketplace API or a curated list).

**Recommendation:** `remote.WithAuthFromKeychain(authn.DefaultKeychain)`, exactly as in the code above. It reads `$DOCKER_CONFIG` or `~/.docker/config.json`, honours `credsStore`/`credHelpers` (this machine has `docker-credential-desktop`, `-osxkeychain`, `-ecr-login` on PATH), and falls through to anonymous when nothing matches. Do **not** use `authn.NewMultiKeychain` with cloud keychains unless you want ECR/GCR — it adds latency and failure modes. Offer `--username/--password-stdin` overrides mapping to `authn.FromConfig` for CI, and never log the token.

---

## 5. Provider family packages

**Yes — the tool must pull the family package too.** VERIFIED.

`provider-family-aws:v2.4.0`: base layer 10,511 B, stream **81,209 bytes**, **6 documents** = 1 meta `Provider` + **5 CRDs**, and *nothing else*:

```
clusterproviderconfigs.aws.m.upbound.io   Cluster     ClusterProviderConfig   aws.m.upbound.io
providerconfigs.aws.m.upbound.io          Namespaced  ProviderConfig          aws.m.upbound.io
providerconfigusages.aws.m.upbound.io     Namespaced  ProviderConfigUsage     aws.m.upbound.io
providerconfigs.aws.upbound.io            Cluster     ProviderConfig          aws.upbound.io
providerconfigusages.aws.upbound.io       Cluster     ProviderConfigUsage     aws.upbound.io
```

This matches your verified context exactly, and confirms the family package contains **ProviderConfig CRDs only — zero managed resources**. Conversely `provider-aws-sqs` contains **zero** ProviderConfig CRDs. Neither package alone lets you generate a valid Composition: you need the MR schemas from the service package and the ProviderConfig kinds/scoping from the family package.

**How to discover the family (the link is in the data).** The service package's meta object carries both a label and an explicit dependency — VERIFIED:

```yaml
metadata:
  labels:
    pkg.crossplane.io/provider-family: provider-family-aws
  name: provider-aws-sqs
spec:
  capabilities: [SafeStart]
  crossplane: {version: '>=v1.12.1-0'}
  dependsOn:
    - provider: xpkg.upbound.io/upbound/provider-family-aws
      version: v2.4.0
```

`provider-family-aws`'s own `dependsOn` is `null`, so the graph is one level deep for providers — resolve `spec.dependsOn[].provider` + `.version`, pull each, recurse with a visited-set (Configuration packages *can* nest deeper). Since `crossplane xpkg get-crds` doesn't do this, your tool owns it. The `pkg.crossplane.io/provider-family` label is the cheap grouping key for the UI.

---

## 6. Scale — real numbers

**Per-package, measured** (`raw` = uncompressed `package.yaml`; `base` = actual bytes downloaded):

| package | base layer | gz stream | raw stream | CRDs |
|---|---|---|---|---|
| provider-aws-ec2 | 974,924 | 877,554 | 8,560,015 | 204 |
| provider-aws-sagemaker | — | 406,051 | 3,650,635 | 46 |
| provider-aws-rds | 346,673 | 322,934 | 2,488,153 | 44 |
| provider-aws-s3 | 292,149 | 260,572 | 2,393,954 | 48 |
| provider-aws-medialive | — | 247,958 | 2,239,592 | 8 |
| provider-aws-glue | — | 184,621 | 1,686,022 | 30 |
| provider-aws-connect | — | 127,796 | 1,444,816 | 30 |
| provider-aws-iam | 88,822 | 73,051 | 911,858 | 46 |
| provider-aws-wafv2 | — | 47,446 | 399,888 | 12 |
| provider-aws-sqs | 20,071 | 18,635 | 182,766 | 8 |
| provider-aws-quicksight | — | 7,517 | 65,244 | 4 |
| provider-family-aws | 10,511 | — | 81,209 | 5 |

Extraction is ~1.5-1.8 s per package via the CLI regardless of size (dominated by round-trips, not bytes).

**Whole AWS family — authoritative, from the `provider-upjet-aws` git tree at `main` (VERIFIED, `truncated: false`, 22,725 entries):**

- **178 service groups** (`apis/cluster/` and `apis/namespaced/` each contain 178 dirs) → ~178 `provider-aws-*` packages + 1 family package.
- **2,065 CRD YAML files** in `package/crds/`, totalling **101,909,861 bytes** (97.2 MiB).
- Split: **1,033 namespaced** (`*.m.upbound.io`) vs **1,032 cluster** — a near-exact 1:1 doubling, as expected.
- Estimated download to index the entire family: ~**11-12 MB** of base layers (raw:base ratio ≈ 8.8:1 measured on ec2), across ~179 pulls.

**Largest single MR OpenAPI schemas** (whole-CRD document bytes, from the repo tree):

```
1,692,444   firehose.aws.upbound.io_deliverystreams.yaml     ← LARGEST
1,404,846   medialive.aws.upbound.io_channels.yaml
  828,057   firehose.aws.m.upbound.io_deliverystreams.yaml
  706,998   budgets.aws.upbound.io_budgets.yaml
  694,444   autoscaling.aws.upbound.io_autoscalinggroups.yaml
  631,665   medialive.aws.m.upbound.io_channels.yaml
  592,413   sagemaker.aws.upbound.io_domains.yaml
  576,281   securityhub.aws.upbound.io_insights.yaml
```

Distribution across the 485 CRDs I extracted: **min 3,661 / p50 28,928 / p90 90,229 / max 1,404,318 / mean 49,657** bytes. The tail is brutal — p50 is 29 KB but the max is 48x the p90.

**Other families, same method (VERIFIED):** Azure = **1,539 CRDs / 79,530,770 B**; GCP = **817 CRDs / 45,156,481 B**. Three clouds ≈ **4,421 CRDs / ~227 MB** raw.

**Browser-UI verdict.** Shipping all schemas client-side is out: 102 MB for AWS alone, ~227 MB for three clouds, and a single kind can be 1.7 MB. But **per-kind lazy loading is entirely viable** — gzip is very effective on these (VERIFIED): `launchtemplates.ec2` 434,529 → **49,540** gzipped (8.8:1); `channels.medialive` 1,404,318 → **121,923** (11.5:1). So: keep the corpus server-side, ship a compact index (§7) of a few hundred KB, and fetch one gzipped CRD (typically 3-15 KB, worst case ~122 KB) when the user selects a kind. Also strip `description` fields for the picker payload — they dominate these schemas — and serve full descriptions only in the field-detail view.

---

## 7. Recommended cache layout and index format

**Mirror the crossplane CLI's proven layout** (VERIFIED as `~/.crossplane/cache/<registry>/<org>/<repo>@<tag>/package.yaml`, currently 18 MB across 12 packages here), but add content-addressing and an index:

```
$XDG_CACHE_HOME/compositionfactory/            (default ~/.cache/compositionfactory)
├── index.json                                  # global, small, memory-mapped on start
├── blobs/sha256/<base-layer-digest>            # CAS: raw package.yaml, gzipped on disk
└── refs/<registry>/<org>/<repo>/
    ├── v2.json                                 # {"resolved":"sha256:e3aa…","base":"sha256:0411…","fetchedAt":…,"ttl":3600}
    └── v2.4.0.json                             # immutable tags: no TTL
```

Why this shape:

- **Key blobs by base-layer digest, not by tag.** VERIFIED: `:v2` and `:v2.4.0` resolve to the same image digest `sha256:e3aaedcc…` today, while `:v2.7.1` is `sha256:dcce6930…`; base layers differ per version (`04115f40…` vs `a4ef233c…`). Content-addressing dedupes floating/pinned aliases for free and makes cache entries immutable. It also dedupes across architectures, since the base layer is arch-identical.
- **Separate the mutable ref→digest mapping from the immutable blob.** Floating tags (`v2`, `latest`) get a short TTL and a cheap `HEAD manifest` revalidation (~2 requests, no blob transfer); pinned semver tags and `@sha256:` refs never expire.
- Store `package.yaml` gzipped: ~9.5:1, so the whole AWS family costs ~11 MB on disk instead of ~102 MB.

**Index format.** One JSON (or SQLite, once you pass a few thousand kinds) row per *kind*, deliberately excluding schemas:

```json
{
  "schemaVersion": 1,
  "packages": [
    {"ref":"xpkg.upbound.io/upbound/provider-aws-sqs","tag":"v2.4.0",
     "imageDigest":"sha256:e3aaedcc…","baseDigest":"sha256:04115f40…",
     "family":"provider-family-aws",
     "dependsOn":[{"provider":"xpkg.upbound.io/upbound/provider-family-aws","version":"v2.4.0"}]}
  ],
  "kinds": [
    {"kind":"Queue","group":"sqs.aws.upbound.io","version":"v1beta1","scope":"Cluster",
     "plural":"queues","categories":["crossplane","managed","aws"],
     "crdName":"queues.sqs.aws.upbound.io","baseDigest":"sha256:04115f40…",
     "docIndex":8,"docBytes":28949,
     "requiredSpec":["forProvider"],
     "role":"managed"}
  ]
}
```

- `baseDigest` + `docIndex` is the pointer into the CAS — **resolve a kind to its schema without re-parsing the package**: seek to document N of the cached stream. Keep `docBytes` so the UI can warn before loading a 1.4 MB monster.
- Index the **pair** `(kind, group)` and expose `scope` prominently, because the v2 doubling means `Queue` is ambiguous — `sqs.aws.upbound.io/Queue` (Cluster) and `sqs.aws.m.upbound.io/Queue` (Namespaced) coexist with identical `names.kind` and identical `categories`. The `.m.` infix in the group is the discriminator; `spec.scope` is the ground truth. Let users filter by scope, and default to Namespaced for Crossplane v2 Compositions.
- `role` distinguishes `managed` (has `spec.forProvider`) from `providerconfig` (from the family package) from `other`. VERIFIED MR shape: `spec.properties` = `{deletionPolicy, forProvider, initProvider, managementPolicies, providerConfigRef, writeConnectionSecretToRef}` with `spec.required == ["forProvider"]` — that is a reliable MR detector, and `forProvider` is the only subtree your Composition generator needs to walk.
- For search, build a separate inverted index over `kind` + `group` + field paths only (not descriptions). At 4,421 kinds across three clouds a plain in-memory map is fine; move to SQLite FTS5 only if you index field paths, which multiplies entries by ~100x.

**Concurrency/politeness:** ~179 pulls to index AWS. Cap at 8-16 concurrent, reuse one `http.Transport` (go-containerregistry shares it via `remote.WithTransport`), and cache the bearer token per registry+scope — otherwise you re-auth 179 times.

---

## Caveats

- The Go extractor was exercised against a local registry replaying **real, unmodified** registry bytes rather than the public endpoint, because outbound TCP from newly-built binaries is blocked in this sandbox (`crossplane`, `crane`, `curl` are unaffected — I used them for all live-network claims). Every byte it parsed came from xpkg.upbound.io / xpkg.crossplane.io via crane, and its output is SHA-256-identical to `crossplane xpkg extract`. Worth one live run in your own shell before you build on it.
- `docker pull` left a **2.52 GB** `provider-aws-sqs:v2` image in your local docker store, and `~/.crossplane/cache` grew by a few MB from the `get-crds` test. I didn't delete either — remove with `docker rmi xpkg.upbound.io/upbound/provider-aws-sqs:v2` if you want the space back.
- Family-wide totals (2,065 CRDs / 101,909,861 B) come from the `provider-upjet-aws` git tree at `main`, which may drift slightly from any one released package set; the per-package numbers are from actual pulls of `:v2`.

Sources: [xpkg specification](https://github.com/crossplane/crossplane/blob/main/contributing/specifications/xpkg.md), [provider-family-aws on Upbound Marketplace](https://marketplace.upbound.io/providers/upbound/provider-family-aws), [provider-upjet-aws](https://github.com/crossplane-contrib/provider-upjet-aws)