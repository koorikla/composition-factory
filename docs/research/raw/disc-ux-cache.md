# Provider discovery: UX, caching, and offline behaviour

Research brief for **compositionfactory** (`cf`). Area: discovery UX, caching, offline.
Date: 2026-08-28. All measurements taken on this machine unless marked DOCS.

---

## Decisions this enables

1. **Ship a static, periodically-rebuilt index — not live queries.** A complete `cf` catalogue of every Crossplane provider that exists today (153 provider roots on the Upbound Marketplace, 409 `provider-*` packages in `ghcr.io/crossplane-contrib`) is **~30 KB gzipped**. That is 1/170th of a single Helm `index.yaml` on this laptop (`bitnami-index.yaml` = 26 MB). There is no size argument for live search, and there *is* a hard argument against it: neither catalogue can be enumerated anonymously (below).

2. **The "empty tag list" negative result in the brief is wrong — retract it.** `GET https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list` returns **HTTP 200 and 446 tags** once you complete the bearer-token challenge against the realm the registry actually advertises (`/service/token`, not `/token`, with a *repository-scoped* `scope` parameter). Version listing on Upbound is a solved problem: 2 requests, ~600 ms, 6.8 KB gzipped. `cf provider versions` can be built today.

3. **Short names must resolve deterministically or fail loudly — never guess.** 15 of 137 distinct provider repository names (11%) are published by more than one account. `provider-kubernetes` and `provider-helm` are each published by *both* `crossplane-contrib` and `upbound`; `provider-minio` by three accounts. Adopt the `publisher/name` two-part form as the canonical short reference, treat a bare one-word name as a *convenience that errors on ambiguity*, and offer `cf provider pin` to record a project-level preference.

4. **Provenance is displayable and machine-verifiable for Upbound, and absent for crossplane-contrib — show that difference.** Upbound packages carry cosign keyless signatures, attestations and SPDX SBOMs (135 `.sig`, 135 `.att`, 74 `.sbom` sidecar tags on `provider-aws-sqs`). I decoded the signing certificate: identity `https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main`, issuer `https://token.actions.githubusercontent.com`. `ghcr.io/crossplane-contrib/provider-aws-s3` has **zero** signature tags. `cf` can and should render "signed by X / unsigned" honestly.

5. **Cache by digest, key by tag, and make staleness visible rather than fatal.** Copy the Terraform Registry's own cache policy, which I read off the wire: `cache-control: public, max-age=30, stale-while-revalidate=1800, stale-if-error=31536000`. `cf` should treat every cached artefact as a three-state value (`fresh` / `stale` / `absent`), serve stale forever when the network is gone, and annotate the UI — the offline path is the *default* path, not the error path.

---

## Retraction / correction of a premise in the task brief

> "NEGATIVE RESULT already established: `https://xpkg.upbound.io/v2/<repo>/tags/list` returns an EMPTY tag list"

**Not reproducible. The endpoint works.** Here is the full trace.

**Step 1 — unauthenticated request returns 401 with a challenge (VERIFIED):**

```
$ curl -s -D - -o /dev/null https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list
HTTP/2 401
docker-distribution-api-version: registry/2.0
www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io",scope="repository:upbound/provider-aws-sqs:pull"
server: istio-envoy

{"errors":[{"code":"UNAUTHORIZED","message":"authentication required",
 "detail":[{"Type":"repository","Class":"","Name":"upbound/provider-aws-sqs","Action":"pull"}]}]}
```

The body has **no `tags` key**. A client that ignores the status code and does `json.Unmarshal(body, &struct{Tags []string})` gets `Tags == nil` — *an empty tag list*. **That is almost certainly the origin of the reported negative result.**

**Step 2 — the token realm is `/service/token`, not `/token` (VERIFIED):**

```
$ curl -s -o /dev/null -w '%{http_code}\n' 'https://xpkg.upbound.io/token?scope=...&service=xpkg.upbound.io'
404
$ curl -s -o /dev/null -w '%{http_code}\n' 'https://xpkg.upbound.io/service/token?scope=repository%3Aupbound%2Fprovider-aws-sqs%3Apull&service=xpkg.upbound.io'
200
```

**Step 3 — a *scopeless* token is issued but is useless (VERIFIED):**

```
$ curl -s 'https://xpkg.upbound.io/service/token?service=xpkg.upbound.io' | jq -r .token | head -c 20
eyJhbGciOiJSUzI1Ni...            # HTTP 200 — a token IS returned
$ curl -H "Authorization: Bearer $SCOPELESS" .../tags/list
HTTP 401 {"errors":[{"code":"UNAUTHORIZED", ...}]}
```

A client that fetches a token once without a scope and reuses it across repos gets 401 everywhere — the second plausible origin of the negative result.

**Step 4 — with a correctly scoped token it works (VERIFIED):**

```
$ curl -H "Authorization: Bearer $TOK" https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list
HTTP 200, 28120 bytes (6767 with Accept-Encoding: gzip), 0.71 s
{"name":"upbound/provider-aws-sqs","tags":["sha256-02b68b...att","sha256-02b68b...sig", ... ]}
```

446 tags total: 135 `.sig`, 135 `.att`, 74 `.sbom`, and **101 real version tags** —
`v0.1.0-rc.0 … v1.23.2, v2.0.0 … v2.7.1`, plus the floating aliases `v1` and `v2`.

**Step 5 — every repo I probed behaves the same (VERIFIED). No empty list anywhere:**

| repo | HTTP | tags |
|---|---|---|
| `upbound/provider-aws-sqs` | 200 | 446 |
| `upbound/provider-aws-s3` | 200 | 28 KB response |
| `upbound/provider-family-aws` | 200 | 541 |
| `upbound/configuration-aws-network` | 200 | 214 |
| `upbound/provider-gcp-storage` | 200 | 81 real versions, incl. `v3.0.0` |
| `crossplane-contrib/provider-kubernetes` (mirrored on xpkg.upbound.io) | 200 | 96 (0 cosign sidecars) |
| `crossplane-contrib/provider-helm` (mirrored) | 200 | 52 |
| `crossplane-contrib/function-go-templating` (mirrored) | 200 | 485 |
| `crossplane/crossplane` | 200 | 3578 (99.5 KB) |

**No pagination is used.** `?n=100` and `?n=1000` both return the *same* 446 tags and **no `Link` header** — the Upbound registry ignores `n` and returns everything in one shot. (GHCR, by contrast, honours `n` and emits `link: </v2/…/tags/list?last=v0.51.0-50.g1b65e52d&n=10>; rel="next"`.)

**Why the confusion is worth designing around:** go-containerregistry performs the challenge automatically, which is why the package-fetch path already works. Any hand-rolled `net/http` call to `tags/list` will silently produce an empty list. `cf` should use `go-containerregistry`'s `remote.List` / `google.List` rather than raw HTTP, and should treat `len(tags)==0` as an *error condition to investigate*, never as "this provider has no versions".

---

## Endpoint ledger

Everything below marked VERIFIED was executed on 2026-08-27/28 from a residential IP, unauthenticated except where noted.

### Registries

| # | URL | Status | Auth | Shape | Rate limits observed | Verdict |
|---|---|---|---|---|---|---|
| R1 | `https://xpkg.upbound.io/v2/` | 401 | anon token via `/service/token` | OCI error JSON | none exposed | VERIFIED |
| R2 | `https://xpkg.upbound.io/service/token?scope=repository:<repo>:pull&service=xpkg.upbound.io` | 200 | none | `{"token":"eyJ…"}` (796 chars, RS256 JWT, 30 min exp) | 10 rapid requests → all 200 | VERIFIED |
| R3 | `https://xpkg.upbound.io/v2/<repo>/tags/list` | 200 | scoped bearer | `{"name":…,"tags":[…]}` | 12 rapid requests → all 200; **no `RateLimit-*` / `Retry-After` headers at all** | VERIFIED |
| R4 | `https://xpkg.upbound.io/v2/_catalog?n=50` | **401** | — | `{"errors":[{"code":"UNAUTHORIZED","detail":[{"Type":"registry","Name":"catalog","Action":"*"}]}]}` | — | VERIFIED — **catalogue enumeration is closed** |
| R5 | `https://xpkg.upbound.io/v2/<repo>/referrers/<digest>` | **404** (`404 page not found`, plain text — not even OCI JSON) | — | — | — | VERIFIED — OCI 1.1 referrers unsupported |
| R6 | `https://ghcr.io/token?scope=repository:<repo>:pull&service=ghcr.io` | 200 | none | `{"token":"…"}` (72 chars) | — | VERIFIED |
| R7 | `https://ghcr.io/v2/crossplane-contrib/<repo>/tags/list?n=…` | 200 | anon bearer | `{"name":…,"tags":[…]}` + `Link: rel="next"` | not hit | VERIFIED |
| R8 | `https://ghcr.io/v2/…/referrers/<digest>` | **404** `MANIFEST_UNKNOWN` | — | — | — | VERIFIED — no referrers |
| R9 | `https://xpkg.crossplane.io/v2/` | 401, `www-authenticate: Bearer realm="https://ghcr.io/token",service="ghcr.io"` | GHCR token | — | — | VERIFIED — **it is a CNAME/alias in front of GHCR**; you must get the token from `ghcr.io`, not from `xpkg.crossplane.io` |
| R10 | `https://xpkg.crossplane.io/v2/crossplane-contrib/provider-aws-s3/tags/list?n=20` (GHCR token) | 200 | GHCR anon token | 18 tags, `v1.20.1 … v2.7.0` | — | VERIFIED |

**Blob fetches redirect cross-host.** `GET /v2/<repo>/blobs/<digest>` returns a 3xx to a CDN; without `-L` you get an unparseable body. Any hand-rolled fetch must follow redirects **and drop the `Authorization` header on the cross-host hop** (go-containerregistry does this; a naive `http.Client` with a default `CheckRedirect` forwards the header, which some CDNs reject).

**Full package fetch, re-measured end to end (VERIFIED):** token → index manifest → amd64 child manifest → config blob → base layer = **5 requests, 28,193 bytes, 1.90 s** for `upbound/provider-aws-sqs:v2`. This confirms the ~20 KB / 5 request figure in the brief. The base layer is 20,071 bytes gzipped, 184,320 bytes uncompressed, a tar containing exactly one entry: `package.yaml`.

### Catalogue / index sources

| # | URL | Status | Auth | Shape | Rate limits | Verdict |
|---|---|---|---|---|---|---|
| C1 | `https://marketplace.upbound.io/providers?page=N` | 200 | none | Next.js SSR HTML, ~300 KB/page, with `<script id="__NEXT_DATA__">` containing a react-query `dehydratedState` → `packages[]` | 7 pages crawled back-to-back, no throttling | VERIFIED |
| C2 | `https://marketplace.upbound.io/api/*`, `https://api.upbound.io/v1/marketplace/*`, `/_next/data/…` | **404** (all 6 shapes I tried) | — | — | — | VERIFIED — **no public marketplace API exists** |
| C3 | `https://marketplace.upbound.io/sitemap.xml` | 200, 11,413 bytes | none | 103 `<loc>` entries: 95 providers, 6 functions, 1 configurations, 1 root — a *curated subset*, not the full 153 | — | VERIFIED |
| C4 | `https://marketplace.upbound.io/robots.txt` | 200 | — | `Disallow: /` for `GPTBot`, `ClaudeBot`, `Google-Extended`; `Allow: /` for `*`; sitemap declared | — | VERIFIED — see terms note below |
| C5 | `https://api.github.com/orgs/crossplane-contrib/packages?package_type=container` | **401 unauthenticated** / 200 with a token | **token required even for public packages** | `[{name,…}]` | 5000/hr authenticated core | VERIFIED |
| C6 | `https://api.github.com/orgs/crossplane-contrib/repos?per_page=100` | 200 | none | repo array | **60/hr unauthenticated**; `x-ratelimit-remaining: 40` after 20 calls | VERIFIED |
| C7 | `https://api.github.com/search/repositories?q=topic:crossplane-provider` | 200 | none | `total_count: 71` — mostly noise (`awslabs/crossplane-on-eks`, `crossplane/terrajet`) | **10/min unauth, 30/min authenticated** | VERIFIED — topic tagging is too sparse to be an index |
| C8 | `https://artifacthub.io/api/v1/packages/search?…` | 200 | none | `{"packages":[…]}` + `pagination-total-count` header | none exposed | VERIFIED |
| C9 | ArtifactHub kind enumeration `kind=0..30` | 200 | none | Helm 17,784 · Krew 410 · KCL 348 · Meshery 330 · Kyverno 564 · OLM 454 · … | — | VERIFIED — **there is no Crossplane kind. ArtifactHub does not index Crossplane packages.** |
| C10 | `https://hub.docker.com/v2/search/repositories/?query=crossplane` | 200 | none | `{"count":883,"next":…,"results":[{repo_name,star_count,pull_count,…}]}` | `x-ratelimit-limit: 180`, `x-ratelimit-remaining`, `x-ratelimit-ip` | VERIFIED — irrelevant to xpkgs but a good rate-limit-header model |

**Marketplace SSR blob, real sample (VERIFIED, trimmed):**

```json
{"account":"upbound","repository":"provider-azapi","repoKey":"upbound/provider-azapi",
 "packageType":"Provider","public":true,"tier":"official",
 "pkgDigest":"sha256:d1b44f29bc654496b688222c68e208f335049cf8b1fdd5231bfe3f890401b062",
 "iconURL":"https://assets.upbound.io/…/icons/icon.svg?Expires=1787866961&Signature=…",
 "annotations":{"hardening":["CVE Remediation","Backporting"],"host":["XP","UXP","Spaces"],
   "subscription":["Community","Standard","Enterprise","Business Critical"],
   "support":["Upbound"],"verification":["Official"]},
 "updatedAt":"2026-08-23T23:08:42Z","downloadCount":10837,
 "description":"Upbound's official Crossplane provider to manage Microsoft Azure API\nresources in Kubernetes.",
 "version":"v2.1.3"}
```

The wrapping query object is `["packageSearch",{"packageType":"Provider","size":24,"excludeFamily":true},false]`
with `{"count":348,"filteredCount":153,"page":0,"size":24,"facets":{…}}`.

Query parameters that **do** work on the SSR page (VERIFIED):
- `?query=sqs` → `count=6, filteredCount=5`; note it *implicitly flips* `excludeFamily` to `false`, so search sees family members.
- `?page=N` → 1-indexed in the URL, 0-indexed in the payload.
- `?size=100` → **ignored**, always 24. Full crawl = 7 requests, 2,059,596 bytes of HTML for 153 records.
- `?excludeFamily=false` → **ignored** on the browse view (stays `true`).

**Licence / terms of use.** I could not locate an Upbound terms-of-service document: `www.upbound.io/legal`, `upbound.io/terms-of-service`, `www.upbound.io/terms-and-conditions`, `marketplace.upbound.io/terms` **all 404** (VERIFIED). The only machine-readable signal is `robots.txt` (C4), which blocks named AI crawlers and allows everyone else. A `cf` binary is not `ClaudeBot`, but the intent is clear enough that I would **not** ship client-side SSR scraping in the distributed binary. Scraping in a CI job that produces a public index, with a descriptive `User-Agent` and a 7-request-per-day footprint, is defensible; per-user scraping at N× users is not. Package *content* is Apache-2.0 (`meta.crossplane.io/license: Apache-2.0` in the package meta, VERIFIED) — the licence question is about the *catalogue metadata*, not the packages. Recommend asking Upbound for a blessed JSON endpoint; the marketplace clearly has one internally (`packageSearch` with facets).

GitHub API terms permit programmatic access within rate limits; krew, brew and `gh` all depend on it. ArtifactHub is CNCF/Apache-2.0 and its API is public and documented.

---

## 1. How comparable tools solve "browse and install from a remote catalogue"

Measured on this machine where possible.

### `helm search hub` vs `helm search repo` — the cleanest split of the two models

Helm ships **both** models and names them differently, which is the single most useful UX precedent here.

| | `helm search hub` | `helm search repo` |
|---|---|---|
| Index lives | remote, ArtifactHub | local, `$HELM_REPOSITORY_CACHE/<repo>-index.yaml` |
| Refresh | every invocation, live HTTP | explicit `helm repo update` |
| Default endpoint | `https://hub.helm.sh` (Monocular-compatible shim in front of ArtifactHub) — VERIFIED from `helm search hub --help` | n/a |
| Offline | **hard failure**: `Error: unable to perform search against "https://127.0.0.1:1"` (VERIFIED) | **works**: returned `crossplane-stable/crossplane 2.4.0` with no network needed (VERIFIED) |
| Size | n/a | **76 MB total** on this laptop; largest single index `bitnami-index.yaml` = **26 MB**; `argo-index.yaml` = 1.6 MB; `authelia-index.yaml` = 375 KB (VERIFIED) |

Helm v4.2.4. Cache paths on macOS (VERIFIED): `HELM_REPOSITORY_CACHE="~/Library/Caches/helm/repository"`, `HELM_CONFIG_HOME="~/Library/Preferences/helm"`.

**Lesson for `cf`:** two verbs with two different network contracts confuses people (`hub` vs `repo` is not self-explanatory), and 26 MB per repo is a size failure caused by `index.yaml` carrying *every version of every chart with full metadata*. `cf` should have **one** verb and a **flat, versionless-by-default** index.

### `krew` — the model to copy

- **Index is a git repository.** Default: `github.com/kubernetes-sigs/krew-index`, one YAML manifest per plugin under `plugins/`.
- **Measured (VERIFIED):** 402 plugin manifests, **833,840 bytes total** (avg 2,074 B, max 4,669 B). `master.tar.gz` = **210,104 bytes**. Repo including full git history = 6.3 MB (`size: 6301` KB from the GitHub API).
- **Refresh:** `kubectl krew update` does a shallow `git fetch` + hard reset against each configured index; indexes are cloned into `$KREW_ROOT/index/<name>` (default `~/.krew`).
- **Offline:** `krew search` and `krew info` read the local clone and work with no network. Only `install`/`upgrade` need the network (they fetch the archive named by `uri` + verify `sha256`).
- **Multiple indexes:** `kubectl krew index add foo https://github.com/foo/custom-index.git`, then `kubectl krew install foo/bar`. Plugins are addressed **`INDEX/PLUGIN`**, and the default index is implicit when the prefix is omitted (DOCS, krew.sigs.k8s.io/docs/user-guide/custom-indexes/).

A real manifest (VERIFIED, 991 bytes) carries exactly what an install needs and nothing more:

```yaml
apiVersion: krew.googlecontainertools.github.com/v1alpha2
kind: Plugin
metadata: {name: ctx}
spec:
  homepage: https://github.com/ahmetb/kubectx
  shortDescription: Switch between contexts in your kubeconfig
  version: v0.11.0
  platforms:
  - selector: {matchExpressions: [{key: os, operator: In, values: [darwin, linux]}]}
    uri: https://github.com/ahmetb/kubectx/archive/v0.11.0.tar.gz
    sha256: 1c8eb6b30c0067f89e5b2f9480865b0e3229a221fadddb644ce192d663c63907
```

**This is the closest fit for `cf`**: comparable catalogue size (402 plugins vs ~350–900 packages), git-hosted, pinned-by-hash, offline-first, and the `INDEX/PLUGIN` addressing scheme is *exactly* the disambiguation primitive the Crossplane name collisions demand.

### `terraform init` + the Terraform Registry protocol

Covered in detail in §2. For this section the relevant facts:

- Discovery is **not** part of the protocol. `terraform init` never searches; it resolves an address the user already wrote in `required_providers`.
- Offline is a **first-class, separately specified protocol** (`provider_network_mirror`) plus a `filesystem_mirror` and the `terraform providers mirror` subcommand that materialises an air-gapped bundle (DOCS).
- Integrity is pinned in `.terraform.lock.hcl` with per-platform `h1:` hashes, so a re-init is offline-verifiable.
- The registry's own HTTP caching is instructive (VERIFIED off the wire):
  `cache-control: public, max-age=30, stale-while-revalidate=1800, stale-if-error=31536000` on `/v1/providers/hashicorp/random/versions`.
  Thirty seconds fresh, thirty minutes serve-stale-while-revalidating, **one year serve-stale-on-error**. Served from CloudFront (`x-cache: Hit from cloudfront`, `age: 15965`).

### `docker search`

Hits `https://hub.docker.com/v2/search/repositories/?query=…` (VERIFIED, HTTP 200). Purely live; there is no local index, and it fails offline. Notable only for exposing honest rate-limit headers: `x-ratelimit-limit: 180`, `x-ratelimit-remaining`, `x-ratelimit-reset`, `x-ratelimit-ip`. Response is a plain page-cursor list:

```json
{"count":883,"next":"https://hub.docker.com/v2/search/repositories/?page=2&page_size=2&query=crossplane",
 "results":[{"repo_name":"crossplane/crossplane","pull_count":50345176,"star_count":6,"is_official":false}]}
```

Note `is_official: false` for `crossplane/crossplane` — a reminder that registry-side "official" flags are not a trust signal you can borrow.

### `gh extension search`

Live GitHub search for `topic:gh-extension`. **975 repositories** carry the topic (VERIFIED via `gh api search/repositories?q=topic:gh-extension`). No local index; fails offline. Rate limit on the search resource is **30/min authenticated, 10/min unauthenticated** (VERIFIED from `x-ratelimit-limit`). The whole mechanism rests on maintainers voluntarily applying a topic — which is exactly why the equivalent for Crossplane (`topic:crossplane-provider`, 71 repos, VERIFIED) is unusable as an index: it under-counts by 5× against the 348 packages the marketplace knows about, and half the hits are not providers.

### `brew search`

Two-tier local cache, and the tiering is the interesting part (VERIFIED on this machine):

- `~/Library/Caches/Homebrew/api/formula_names.txt` — **76,259 bytes for 8,571 formulae** (~9 bytes per name). This is what name search hits.
- `~/Library/Caches/Homebrew/api/` total — **30 MB** (full `formula.json` / `cask.json` payloads for details).
- Remote source `https://formulae.brew.sh/api/formula.json` — **5,089,497 bytes gzipped**, `cache-control: max-age=600`, weak ETag (VERIFIED).
- Legacy tap clones still on disk: `/opt/homebrew/Library/Taps` = **62 MB**.
- Offline: `HOMEBREW_NO_AUTO_UPDATE=1 brew search --formula '/^crossplane/'` returned `crossplane` instantly with no network (VERIFIED).

**Lesson:** separating a tiny *name* index from a fat *detail* payload lets search be instant and offline while details stay lazy. `cf`'s numbers are so small that it can collapse both tiers — but the pattern is the fallback if the catalogue grows.

### Summary table

| Tool | Index location | Refresh | Size (measured) | Offline behaviour |
|---|---|---|---|---|
| `helm search hub` | remote ArtifactHub | every call | — | **fails hard** |
| `helm search repo` | `~/Library/Caches/helm/repository/*.yaml` | `helm repo update` | 76 MB here; 26 MB largest single index | full search works |
| `krew` | git clone at `~/.krew/index/<name>` | `krew update` (shallow fetch) | 402 manifests, 834 KB; 210 KB tarball | search/info work; install needs net |
| `terraform init` | none (no discovery); `.terraform.lock.hcl` pins | n/a | lock file only | `filesystem_mirror` / `network_mirror` / `terraform providers mirror` |
| `docker search` | none | every call | — | **fails hard** |
| `gh extension search` | none (GitHub search API) | every call | — | **fails hard** |
| `brew search` | `~/Library/Caches/Homebrew/api` | auto-update / `brew update` | 76 KB names + 30 MB details | full search works |

Three of six fail hard offline. All three that work offline keep a **local file** and refresh it on an **explicit or scheduled** command. That is the design.

---

## 2. The Terraform Registry protocol, and what Crossplane has instead

### The protocol (DOCS, endpoints VERIFIED live)

**(a) Service discovery.** The client fetches `https://<hostname>/.well-known/terraform.json` and reads the `providers.v1` key to learn the base path.

```
$ curl https://registry.terraform.io/.well-known/terraform.json      # HTTP 200
{"modules.v1":"/v1/modules/","providers.v1":"/v1/providers/"}
```

This one indirection is what makes the whole thing federatable: any host can serve providers by publishing that file. `source = "example.com/myorg/myprovider"` just works.

**(b) List available versions** — `GET <base>/:namespace/:type/versions`:

```
$ curl https://registry.terraform.io/v1/providers/hashicorp/random/versions   # HTTP 200
{"id":"hashicorp/random","versions":[
  {"version":"3.4.1","protocols":["5.0"],
   "platforms":[{"os":"darwin","arch":"arm64"},{"os":"linux","arch":"amd64"}, …]}, …]}
```

Note what is in there: not just the version string but the **protocol compatibility range** and the **platform matrix**. The client can reject an incompatible version before downloading anything.

**(c) Find a provider package** — `GET <base>/:namespace/:type/:version/download/:os/:arch`:

```
$ curl https://registry.terraform.io/v1/providers/hashicorp/random/3.6.0/download/darwin/arm64  # HTTP 200
{"protocols":["5.0"],"os":"darwin","arch":"arm64",
 "filename":"terraform-provider-random_3.6.0_darwin_arm64.zip",
 "download_url":"https://releases.hashicorp.com/terraform-provider-random/3.6.0/…zip",
 "shasums_url":"https://releases.hashicorp.com/…/…_SHA256SUMS",
 "shasums_signature_url":"https://releases.hashicorp.com/…/…_SHA256SUMS.72D7468F.sig",
 "shasum":"e747c0fd5d7684e5bfad8aa0ca441903f15ae7a98a737ff6aca24ba223207e2c",
 "signing_keys":{"gpg_public_keys":[{"key_id":"34365D9472D7468F","ascii_armor":"-----BEGIN PGP…"}]}}
```

The registry **hands the client the trust material inline**: the checksum, the detached signature URL, and the signing public key. The client verifies before extracting.

**(d) Not in the protocol: search.** There is no browse or search endpoint in the spec. HashiCorp runs a *separate, unversioned, non-protocol* API for the website — `GET /v1/providers?namespace=hashicorp&limit=2` (VERIFIED, HTTP 200) — which returns `{"meta":{"limit","current_offset","next_offset","next_url"},"providers":[{id,namespace,name,version,description,source,published_at,downloads,tier,logo_url}]}`. Note `tier: "official"` and `downloads: 721203965`. This is the same shape Upbound's `packageSearch` returns. **Neither vendor considers browse part of the protocol; both build it as a product surface on top.**

**(e) Authentication:** none specified for public registries; credentials in the CLI config are attached to metadata requests but *not* to archive downloads (DOCS).

**(f) The offline half — `provider_network_mirror` (DOCS).** A second, deliberately dumber protocol designed to be servable from static hosting:

- `GET <base>/:namespace/:type/index.json` → `{"versions":{"3.6.0":{}, "3.5.1":{}}}`
- `GET <base>/:namespace/:type/:version.json` → `{"archives":{"darwin_arm64":{"url":"…","hashes":["h1:…"]}}}`

Configured via a CLI-config block:

```hcl
provider_installation {
  network_mirror { url = "https://terraform.example.com/providers/" }
}
```

and `terraform providers mirror ./dir` materialises exactly that directory tree for air-gapped use. **The offline story is a specified artifact format, not a cache implementation detail.** That is the part `cf` should steal hardest.

### Does Crossplane have an equivalent?

**No. Nothing at any of the four layers.** (VERIFIED by probing, DOCS for the negative claims.)

| Terraform layer | Crossplane equivalent | Status |
|---|---|---|
| Service discovery (`/.well-known/terraform.json`) | none | **absent** — a package address is a raw OCI reference; there is no indirection, no way for a host to declare "I serve xpkgs" |
| List versions | OCI `tags/list` | **exists, but generic**: no protocol/platform compatibility data, and the semantics of a tag are unconstrained. `upbound/provider-aws-sqs` returns 446 tags of which 345 are cosign sidecars the client must filter by naming convention |
| Download + inline trust material | OCI manifest/blob | **exists, but trust material is out-of-band**: signatures live at a *conventional tag* (`sha256-<digest>.sig`) because the referrers API 404s on both registries (VERIFIED, R5/R8) |
| Search/browse | Upbound Marketplace UI | **exists as a product, not an API** — no public endpoint (VERIFIED, C2); ArtifactHub has no Crossplane kind (VERIFIED, C9) |
| Offline mirror format | none | **absent** — `crossplane xpkg extract`/the `~/.crossplane/cache` layout is an implementation detail, not a spec |

What Crossplane *does* have, which Terraform does not, is **in-band package metadata**. Every xpkg carries a `meta.pkg.crossplane.io/v1` document with maintainer, licence, source repo, family membership, dependencies and a Crossplane version constraint (see §5). That is genuinely better raw material than the registry protocol's `versions` response — it just has no discovery layer above it.

**Would `cf` be inventing a protocol?** Partly, and it should be honest about the scope. There are three distinct things and `cf` should only build the first two:

1. **A catalogue** (which providers exist, who publishes them, what they are called). Crossplane has none. `cf` must build one, and should build it as **data, not as a protocol** — a versioned JSON document at a stable URL, with the schema documented. This is a krew-index, not a registry protocol.
2. **A resolution rule** (short name → OCI ref). `cf`'s own concern, no standard needed.
3. **A version/download protocol.** Do **not** invent one. OCI `tags/list` + manifest + the `io.crossplane.xpkg:<digest>=base` label already work anonymously on all three registries (VERIFIED). Use go-containerregistry.

If the catalogue schema is published and the index is a plain git repo, the "protocol" is ~40 lines of documentation and anyone can host an alternate index — the krew property. That is the right amount of invention.

---

## 3. Recommended design for `cf`

### 3.1 Static index, rebuilt on a schedule

**Decision: a periodically-rebuilt static index, fetched over plain HTTPS from a git-hosted JSON file. Not live queries.**

Three reasons, all measured:

1. **Neither catalogue can be enumerated by the client.** `xpkg.upbound.io/v2/_catalog` → 401 (VERIFIED). `api.github.com/orgs/crossplane-contrib/packages` → 401 without a token (VERIFIED). There is *no* anonymous enumeration path. Live search would require either shipping a credential or scraping the marketplace from every user's machine — the latter at odds with C4's robots posture and fragile against a Next.js `buildId` bump.
2. **It is tiny.** See sizing below.
3. **Offline is the requirement, not a nice-to-have.** The brief says the tool must keep working offline against its cache. A live-query design makes discovery a network dependency by construction.

### 3.2 Sizing — measured, not estimated

Built from the real 153-provider marketplace crawl.

| Index tier | Contents | 153 providers (measured) | scaled to 350 pkgs | scaled to 900 pkgs |
|---|---|---|---|---|
| **A — search index** | id, registry, latest version, tier, downloads, 140-char description | **27,554 B raw / 5,241 B gzip** (180 B/pkg) | ~63 KB / ~12 KB gz | ~162 KB / ~31 KB gz |
| **B — A + full version list** (~100 versions each) | + `versions[]` | 166 KB raw / 7.2 KB gzip | ~380 KB / ~16 KB gz | ~978 KB / ~42 KB gz |

Version strings compress extraordinarily well (`v2.5.1`, `v2.5.2`, … is near-perfectly redundant), so **tier B costs almost nothing over tier A once gzipped**: 42 KB vs 31 KB at 900 packages.

**Recommendation: ship tier B.** One file, ~40 KB gzipped, containing every provider *and* every version. `cf provider versions` then works fully offline, which `helm search repo` and `krew search` both give you and which is the difference between "browse" and "browse and pick a version".

For scale context: krew's index is 210 KB (5× larger) for a comparable catalogue; `bitnami-index.yaml` on this laptop is 26 MB (620× larger).

Current real cardinality (VERIFIED): 153 provider roots and 348 total packages on the Upbound Marketplace; 409 `provider-*` packages in `ghcr.io/crossplane-contrib` (179 `aws-*`, 99 `azure-*`, 82 `gcp-*`, 20 `alibabacloud-*`). Deduplicated union is on the order of **500–900** entries. The 900-package column is the realistic upper bound.

### 3.3 Where the index lives and how it is built

**Publish** at two URLs backed by the same git repo:

```
https://raw.githubusercontent.com/<org>/cf-index/main/index-v1.json.gz     # ~40 KB
https://raw.githubusercontent.com/<org>/cf-index/main/index-v1.json.sha256
```

`raw.githubusercontent.com` serves `ETag` and `Cache-Control` and needs no auth; a GitHub Pages or R2 mirror can be added later without changing the client (the client should read a `mirrors[]` array out of the index itself, so the *next* fetch can rotate).

**Build** in a scheduled GitHub Action, daily:

| Source | Requests | Cost | Auth |
|---|---|---|---|
| Marketplace SSR crawl, 7 pages | 7 | 2.06 MB HTML (VERIFIED) | none |
| `orgs/crossplane-contrib/packages` (paginated) | ~5 | small | `GITHUB_TOKEN` (free in Actions) |
| `tags/list` per package (token + list) | ~2 × 900 | ~7 KB gz each ≈ 6 MB | anonymous |
| Package `meta` for new/changed digests only | 5 × Δ | 28 KB each | anonymous |

Serial that is ~15 minutes; at concurrency 8, ~2 minutes. Entirely within a free Actions runner. Critically, **the scraping happens once per day in one place**, not once per user per search.

**Index schema (proposed, v1):**

```json
{
  "schemaVersion": 1,
  "generatedAt": "2026-08-28T04:00:00Z",
  "mirrors": ["https://cf-index.example.dev/index-v1.json.gz"],
  "publishers": {
    "upbound":            {"registry": "xpkg.upbound.io",     "tier": "official",
                           "signing": {"identity": "https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main",
                                       "issuer": "https://token.actions.githubusercontent.com"}},
    "crossplane-contrib": {"registry": "xpkg.crossplane.io",  "tier": "community", "signing": null}
  },
  "providers": [
    {
      "publisher": "upbound",
      "name": "provider-aws-sqs",
      "short": "aws-sqs",
      "friendly": "Provider AWS (sqs)",
      "family": "provider-family-aws",
      "dependsOn": [{"provider": "xpkg.upbound.io/upbound/provider-family-aws", "version": "v2.4.0"}],
      "description": "Upbound's official Crossplane provider to manage AWS sqs services in Kubernetes.",
      "license": "Apache-2.0",
      "source": "github.com/crossplane-contrib/provider-upjet-aws",
      "maintainer": "Upbound <support@upbound.io>",
      "crossplaneVersion": ">=v1.12.1-0",
      "capabilities": ["SafeStart"],
      "downloads": 1961953,
      "updatedAt": "2026-08-23T23:08:42Z",
      "signed": true,
      "latest": "v2.7.1",
      "versions": ["v2.7.1", "v2.7.0", "…"],
      "digests": {"v2.7.1": "sha256:e3aaedccfcc3022bed7763fb3f5a48b4ce5ae915e6dc5b2032688cb06f8aaf11"}
    }
  ]
}
```

Every field above is populated from something I actually retrieved: `friendly`, `family`, `dependsOn`, `license`, `source`, `maintainer`, `crossplaneVersion`, `capabilities` come from the in-package `meta.pkg.crossplane.io/v1` document (§5); `downloads`, `tier`, `updatedAt` from the marketplace payload; `versions` and `digests` from `tags/list` + manifest.

**`digests` is the load-bearing field.** It lets `cf` resolve a tag to a digest offline and pin it, which closes the mutable-tag hole (§6).

**Multiple indexes, krew-style.** `cf index add <name> <url>` / `cf index list` / `cf index remove <name>`. An enterprise with a private registry publishes its own index JSON and its providers become addressable as `<index>/<publisher>/<name>`. This costs almost nothing to build and is the difference between a tool people can adopt internally and one they can't.

### 3.4 Cache layout, TTLs, and offline behaviour

Root: `$CF_CACHE`, defaulting to `$XDG_CACHE_HOME/cf` (`~/Library/Caches/cf` on macOS).

**Important: reuse `~/.crossplane/cache` for package content.** That directory already exists on this machine and is exactly the right shape (VERIFIED):

```
~/.crossplane/cache/xpkg.upbound.io/upbound/provider-aws-sqs@v2/package.yaml     (180 KB)
~/.crossplane/cache/xpkg.upbound.io/crossplane-contrib/provider-kubernetes@v1.0.0/package.yaml
~/.crossplane/cache/xpkg.crossplane.io/crossplane/crossplane@stable/package.yaml
                                                                    # 18 MB total, 11 packages
```

Sharing it means `cf` and `crossplane xpkg get-crds` warm each other's cache. **But note the flaw to avoid: the directory is keyed by `@<tag>`, and `provider-aws-sqs@v2` is a floating tag. That entry will be silently stale forever.** `cf` should write digest-keyed directories alongside and keep a separate tag→digest map with its own TTL.

| Layer | Path | Key | Soft TTL | Hard TTL | On expiry |
|---|---|---|---|---|---|
| **L0 index** | `index/<name>/index-v1.json` + `.etag` | index name | **24 h** | **never** | conditional GET with `If-None-Match`; on failure serve stale + warn once |
| **L1 tag→digest** | `resolve/<registry>/<repo>/<tag>.json` | registry+repo+tag | **1 h** (floating tags: `v2`, `latest`, `stable`) / **never** (exact semver: `v2.7.1`) | 30 d | re-resolve; on failure serve stale + warn |
| **L2 package content** | `pkg/<registry>/<repo>/<digest>/package.yaml` | **content digest** | **never** | **never** | immutable; garbage-collect by LRU above a size cap |
| **L3 derived schemas** | `schema/<digest>/<group>/<version>/<kind>.json` | content digest | never | never | derived from L2, rebuildable |
| **L4 signature verdicts** | `verify/<digest>.json` | content digest | 7 d | never | re-verify; stale verdict shown as "verified 12 days ago" |

The distinction on L1 is the important one and I have not seen another tool get it right: **an exact semver tag on a content-addressed registry is immutable in practice and should be cached forever; a floating tag is a pointer and must have a short TTL.** `cf` can detect the difference syntactically (`^v\d+$` and `^v\d+\.\d+$` and `latest`/`stable`/`main` are floating; a full `vX.Y.Z` is not).

**Offline behaviour — the three-state contract.** Every read returns `(value, freshness)` where freshness ∈ {`fresh`, `stale(age)`, `absent`}.

- `fresh` → normal output, no annotation.
- `stale` → **full normal output**, plus one line to stderr: `note: provider index is 9 days old (offline); run 'cf index update' when connected`. Exit 0. Never a prompt, never a blocking retry.
- `absent` → the only failure. Message must state precisely what is missing and what would fix it, and must distinguish "never fetched" from "fetch failed":
  `error: no provider index cached. 'cf provider search' needs an index; run 'cf index update' (needs network), or 'cf index add <name> <file://path>' for an air-gapped index.`

**Network timeouts must be short and the fallback silent.** Default 3 s connect / 8 s total for index refresh, then fall through to cache. A tool that hangs for 30 s on a captive-portal DNS is worse offline than one with no network code at all.

**Air-gap path.** Borrow `terraform providers mirror` wholesale:

```
cf index export ./cf-bundle          # writes index-v1.json + every referenced package layer
cf index add corp file://./cf-bundle # on the air-gapped side
```

Because the bundle is digest-addressed, it is verifiable on arrival with no network.

**`--offline` flag.** An explicit `--offline` (and `CF_OFFLINE=1`) that makes *any* network attempt an error rather than a fallback, for CI reproducibility. Terraform's `-plugin-dir` plays this role; make it a first-class flag rather than an emergent behaviour.

### 3.5 CLI surface

Design rules: one search verb (not helm's two); every resolution prints what it resolved to; ambiguity is an error with a copy-pasteable fix.

```
$ cf provider search sqs

  NAME                          PUBLISHER           LATEST    SIGNED  DOWNLOADS  DESCRIPTION
→ aws-sqs                       upbound  official   v2.7.1    yes       1.9M     Manage AWS sqs services
  aws-lambda                    upbound  official   v2.7.1    yes       655K     Manage AWS lambda services
  scaleway                      scaleway community  v0.6.0    no         50K     Scaleway resources (matches: sqs)
  aws                           crossplane-contrib  v0.59.0   no        3.0M     Community AWS provider (deprecated)

  index: 4 hours old · 153 providers · 'cf provider add aws-sqs' to use the first result
```

```
$ cf provider add aws-sqs

  resolved  aws-sqs → xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1
            digest  sha256:e3aaedccfcc3022bed7763fb3f5a48b4ce5ae915e6dc5b2032688cb06f8aaf11
            signed  cosign · github.com/upbound/upbound-official-build (verified)
            family  also fetching xpkg.upbound.io/upbound/provider-family-aws:v2.4.0  (required by dependsOn)

  fetched   provider-aws-sqs      184 KB   8 CRDs   (5 requests, 28 KB over the wire)
  fetched   provider-family-aws    12 KB   2 CRDs

  8 resource kinds now available. Try: cf compose sqs.aws.m.upbound.io/Queue
```

Note the family fetch is **automatic and announced** — the brief records that `crossplane xpkg get-crds` does not resolve `spec.dependsOn`, and the index carries the edge (`dependsOn: xpkg.upbound.io/upbound/provider-family-aws v2.4.0`, VERIFIED from the package meta), so `cf` can do what the upstream CLI won't.

Full surface:

| Command | Network | Offline behaviour |
|---|---|---|
| `cf provider search <term>` | none | reads L0; warns if stale |
| `cf provider search --all <term>` | none | includes family members (default hides them, like the marketplace's `excludeFamily`) |
| `cf provider list --available [--publisher X] [--tier official] [--signed]` | none | reads L0 |
| `cf provider versions <ref>` | none if index has it; else L1 | index carries all versions, so offline works |
| `cf provider info <ref>` | none for index fields; L2 for CRD counts | shows what it has, marks the rest `(not cached)` |
| `cf provider add <ref>` | yes, unless L2 hit | with `--offline`, errors unless the digest is cached |
| `cf provider pin <short> <publisher>` | none | writes `.cf/providers.yaml` |
| `cf index update [--all]` | yes | the only command that *must* have network |
| `cf index status` | none | per-index: name, url, age, provider count, last error |
| `cf index add/remove/list <name> <url>` | on add only | `file://` accepted |
| `cf index export <dir>` | yes | air-gap bundle |

`cf index status`, which is what a user hits when something feels wrong:

```
$ cf index status

  NAME       SOURCE                                              AGE       PROVIDERS  STATUS
  default    github.com/<org>/cf-index                           4h        153        ok
  corp       file:///opt/cf-bundle                                —         12        ok (local)

  last refresh attempt: 4 hours ago (success)
  cache: 18 MB in ~/Library/Caches/cf, 11 packages, 0 stale digests
```

**Auto-refresh policy:** refresh the index in the background at most once per 24 h, triggered by any `cf provider *` command, **never blocking the command**. Print the result of the *previous* refresh, not this one. Homebrew's blocking auto-update is the anti-pattern; `HOMEBREW_NO_AUTO_UPDATE=1` exists precisely because it was wrong. Provide `CF_NO_AUTO_UPDATE=1` from day one anyway.

### 3.6 GUI surface — the palette "add provider" flow

Given a fully local index, the palette can be **synchronous and instant**, which changes the interaction entirely: no spinner, no debounce-against-a-remote-API, no empty-state-while-loading.

```
⌘K → "add provider"

┌────────────────────────────────────────────────────────────────────┐
│  Add provider                                          index 4h old │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ sqs                                                       ⌫  │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                    │
│  ● aws-sqs                            upbound      OFFICIAL  ✓sig  │
│    Provider AWS (sqs) · Apache-2.0 · 1.9M pulls · updated 5d ago   │
│    v2.7.1  ▾                                                       │
│                                                                    │
│  ○ aws-lambda                         upbound      OFFICIAL  ✓sig  │
│  ○ scaleway                           scaleway     community       │
│                                                                    │
│  ─────────────────────────────────────────────────────────────     │
│  ⏎ add · ⌘⏎ add with version picker · ⌥ show family members         │
└────────────────────────────────────────────────────────────────────┘
```

Behaviours that follow from the caching design:

- **Filtering is local and per-keystroke.** 900 records × fuzzy match is microseconds. No debounce, no request cancellation, no loading state.
- **The version dropdown is populated from the index**, so it opens instantly and works offline. It defaults to the latest **exact** version (`v2.7.1`), not the floating alias (`v2`) — and if the user picks `v2` deliberately, the UI shows what it currently resolves to: `v2 → v2.7.1 (moves)`.
- **A single row per collision group.** When `provider-kubernetes` matches two publishers, render *one* row that expands, not two lookalike rows — otherwise the user picks whichever is first, which is exactly the failure mode to avoid:

  ```
  ▾ kubernetes                                         2 publishers
      ● crossplane-contrib   v1.3.0   6.9M pulls   community   no sig
      ○ upbound              v1.3.1   1.6M pulls   OFFICIAL    ✓sig
      these are different builds — pick one, or 'cf provider pin'
  ```

- **The staleness badge is always visible and always clickable** (`index 4h old` → refresh). Never a modal.
- **Adding shows the resolution and the family fetch** in the same detail the CLI does; the GUI's job is to make the two-package reality of upjet families legible, not to hide it.
- **Offline is a badge, not a blocker.** `index 9d old · offline` in amber; every provider in the index stays addable, and only the actual fetch fails — with the cached-digest set visibly marked as available.

---

## 4. The short-name problem

### The problem is real and measurable

Of 137 distinct provider repository names across 153 marketplace entries, **15 (11%) are published by more than one account** (VERIFIED, full list):

| repository | publishers (sorted by downloads) |
|---|---|
| `provider-kubernetes` | **crossplane-contrib** v1.3.0 6,947,609 community · **upbound** v1.3.1 1,590,224 official |
| `provider-helm` | **crossplane-contrib** v1.4.0 2,243,670 community · **upbound** v1.4.1 833,798 official |
| `provider-family-azure` | **upbound** v2.7.1 12,544,940 official · **crossplane-contrib** v2.7.0 6,187 community |
| `provider-minio` | vshn v0.4.5 128,243 · alekc v1.0.1 239 · markopolo123 v0.2.1 124 |
| `provider-opensearch` | tages v1.2.0 247,839 · dkb-bank v0.3.0 21,618 |
| `provider-github` | coopnorge v0.13.0 152,633 · xunholy v0.1.8 4,601 |
| `provider-tencentcloud` | crossplane-contrib v0.8.6 136,479 · arindraaribudi v1.82.9 29 |
| `provider-databricks` | **upbound** v0.1.11 3,692 official · lalanne v2.4.2 2,435 community |
| `provider-rabbitmq` | evaneos v1.0.1 1,681 · pnowy v2.0.1 861 |
| `provider-sonarqube` | globallogicuki v0.0.5 1,284 · crossplane-contrib v0.13.0 302 |
| `provider-vcd` | ankasoftco v0.1.0 553 · arkilasystems v0.3.0 235 |
| `provider-exoscale` | vshn v1.0.3 255 · exoscale v0.1.0 80 |
| `provider-vultr` | crossplane-contrib v0.2.0 137 · **upbound** v1.0.0 6 |
| `provider-cloudscale` | onzack-ag v0.1.5 16 · vshn v0.5.11 10 |
| `provider-ksyun` | notone v0.1.7 13 · kingsoftcloud v0.1.0 6 |

Several of these are genuinely hostile to heuristics:

- **`provider-databricks`**: the "official" one (Upbound) is `v0.1.11` with 3,692 downloads; the community one is `v2.4.2` with 2,435. Tier says one thing, version maturity says another, downloads are a coin-flip.
- **`provider-vultr`**: `crossplane-contrib` has 137 downloads, `upbound` has **6**. Neither is meaningfully "the" one.
- **`provider-kubernetes` / `provider-helm`**: `crossplane-contrib` has 3–4× the downloads, but `upbound`'s is tier "official" and one patch newer. A downloads heuristic and a tier heuristic give **opposite** answers.
- **`provider-aws-s3`**: published by *both* `upbound` (on `xpkg.upbound.io`) and `crossplane-contrib` (on `xpkg.crossplane.io`), and I confirmed both are **built from the same source repository** — `meta.crossplane.io/source: github.com/crossplane-contrib/provider-upjet-aws` in both packages (VERIFIED). Same code, two builds, different trust posture: Upbound's is signed with SBOM layers (`schema.json`, `schema.kcl`, `schema.python`, `schema.go`) and pins `dependsOn … version: v2.4.0`; contrib's is unsigned and declares `dependsOn … version: '>= 0.0.0'`.

**Any silent tiebreak will be wrong for some user, and they will not notice.** That is the design constraint.

### Proposed scheme

**Canonical reference grammar** (parsed left to right, first match wins):

| # | Form | Example | Rule |
|---|---|---|---|
| 1 | **Full OCI ref** — contains `:` or a first segment with a `.` | `xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1` | passed through untouched; no index needed; works with zero index |
| 2 | **Index-qualified** — 3 segments | `corp/upbound/aws-sqs` | `<index>/<publisher>/<name>`, krew's scheme |
| 3 | **Publisher-qualified** — 2 segments | `upbound/aws-sqs` | **the canonical short form.** Publisher names are unique per index; unambiguous by construction |
| 4 | **Bare short name** — 1 segment | `aws-sqs` | convenience; **resolves only if exactly one match**, else hard error |

**Name normalisation**, applied to both the index's `short` field and to user input, in order:

1. lowercase; `_` → `-`
2. strip leading `provider-`, `crossplane-provider-`, `provider-upjet-`
3. strip trailing `-provider`
4. collapse repeated `-`

So `provider-aws-sqs` → `aws-sqs`; `crossplane-provider-castai` → `castai`; `crossplane-provider-yc` → `yc`. The index publishes the normalised `short` alongside the raw `name` so the client never has to re-derive it (and so a future normalisation change is an index rebuild, not a client upgrade).

**Version suffix** is orthogonal and uses `@`: `upbound/aws-sqs@v2.7.1`, `aws-sqs@v2`. Using `@` rather than `:` keeps the short form visually distinct from a full OCI ref, so `cf` can tell at a glance which grammar it is parsing.

**Resolution algorithm for form 4 (bare name):**

```
matches := index.byShortName[normalise(input)]
switch len(matches) {
case 1:  resolve; print "resolved  aws-sqs → xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1"
case 0:  suggest by edit distance + substring; exit 1
default:
    if pin, ok := projectPins[normalise(input)]; ok { resolve to pin; print "resolved via .cf/providers.yaml pin" }
    else { AMBIGUOUS ERROR }
}
```

**The ambiguity error — the whole point of the scheme:**

```
$ cf provider add kubernetes

  error: 'kubernetes' is ambiguous — 2 publishers ship a provider with this name.

    crossplane-contrib/kubernetes   v1.3.0   6.9M pulls   community    unsigned
    upbound/kubernetes              v1.3.1   1.6M pulls   official     signed

  These are different builds. Pick one:

    cf provider add crossplane-contrib/kubernetes
    cf provider add upbound/kubernetes

  Or record a preference for this project:

    cf provider pin kubernetes crossplane-contrib
```

Non-negotiables in that output: **no default is applied**, the two candidates are shown with the *disagreeing* signals side by side (downloads favour contrib, tier and signing favour upbound), and both fixes are copy-pasteable.

**Ranking vs selection.** The index carries a `rank` used **only for display order** in search results and *never* for silent selection: (1) publisher is `upbound` or `crossplane-contrib`, (2) tier `official` > `partner` > `community`, (3) signed > unsigned, (4) downloads. Documenting this as display-only, in the help text, is what keeps it from quietly becoming a resolution rule.

**Pins.** `cf provider pin <short> <publisher>` writes a checked-in project file:

```yaml
# .cf/providers.yaml
apiVersion: cf/v1
pins:
  kubernetes: crossplane-contrib
  helm: crossplane-contrib
indexes:
  default: github.com/<org>/cf-index@2026-08-28   # optional: pin the index revision too
```

A pin makes bare names deterministic *within a project*, which is the only scope where a default is legitimate — the team decided, and the decision is in version control next to the compositions it affects. A global `~/.config/cf/pins.yaml` should exist too but rank *below* the project file.

**Two hard rules:**

1. **`cf` always prints the full ref it resolved to**, in every mode, including the GUI. Short names are an input convenience, never an output. This is `helm`'s biggest UX miss (`helm install foo bar/baz` never tells you which chart version came from where until you ask).
2. **Whatever `cf` writes to disk — the generated Composition, the lock file — carries the full ref plus the digest, never the short name.** The short name is resolved at input time and discarded. That way a `.cf` directory checked into git reproduces byte-identically on a machine with a different index, a different pin file, or no index at all.

---

## 5. Trust and provenance in the UI

`cf` executes nothing from these packages — it downloads a `package.yaml` and reads CRDs. But it *renders third-party names, descriptions, links and icons*, and it tells the user "this is the official AWS provider", which is a trust claim. Three things follow.

### What Crossplane actually gives you (VERIFIED)

**Every xpkg carries a signed-in-place metadata document.** Extracted live from `xpkg.upbound.io/upbound/provider-aws-sqs:v2` (5 requests, 28 KB):

```yaml
apiVersion: meta.pkg.crossplane.io/v1
kind: Provider
metadata:
  annotations:
    friendly-name.meta.crossplane.io: Provider AWS (sqs)
    meta.crossplane.io/description: |
      Upbound's official Crossplane provider to manage Amazon Web Services (AWS)
      sqs services in Kubernetes.
    meta.crossplane.io/license: Apache-2.0
    meta.crossplane.io/maintainer: Upbound <support@upbound.io>
    meta.crossplane.io/source: github.com/crossplane-contrib/provider-upjet-aws
    meta.upbound.io/hardening: |
      - CVE Remediation
      - Backporting
      - FIPS Compatibility
    meta.upbound.io/support: Upbound
    meta.upbound.io/verification: Official
  labels:
    pkg.crossplane.io/provider-family: provider-family-aws
  name: provider-aws-sqs
spec:
  capabilities: [SafeStart]
  crossplane: {version: '>=v1.12.1-0'}
  dependsOn:
  - {provider: xpkg.upbound.io/upbound/provider-family-aws, version: v2.4.0}
```

The same field for `ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0` (VERIFIED) is *identical in structure* but **lacks every `meta.upbound.io/*` annotation** and declares `dependsOn … version: '>= 0.0.0'`.

**This matters more than the marketplace metadata**, because it is in-band: it travels with the digest and is covered by the signature. The marketplace's `tier: "official"` is a claim by a website; `meta.upbound.io/verification: Official` inside a cosign-signed layer is a claim you can verify. `cf` should display the **in-band** values and treat the index's copies as a search aid only.

### Signing: yes, and it is verifiable

**Crossplane supports signature verification natively** since v1.18 via the `ImageConfig` resource — `spec.verification.cosign.authorities`, with multiple authorities OR'd together (DOCS: docs.crossplane.io/latest/packages/image-configs/).

**Upbound signs, keyless, via Sigstore.** I pulled `sha256-e3aaedcc….sig` and decoded the embedded certificate (VERIFIED):

```
Issuer:      O=sigstore.dev, CN=sigstore-intermediate
Validity:    Feb  9 11:43:15 2026 → Feb  9 11:53:15 2026        (10 minutes — ephemeral)
SAN (critical): URI:https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main
1.3.6.1.4.1.57264.1.1:  https://token.actions.githubusercontent.com
```

Layer annotations present: `dev.cosignproject.cosign/signature`, `dev.sigstore.cosign/bundle` (with a Rekor `SignedEntryTimestamp`), `dev.sigstore.cosign/certificate`, `dev.sigstore.cosign/chain`. The `bundle` means **verification does not require a Rekor round-trip** — the inclusion proof travels with the signature, so `cf` can verify offline from cached bytes.

This exactly matches Upbound's published instructions (DOCS, docs.upbound.io/providers/signature-verification), so my decode independently confirms the documented identity:

```
cosign verify xpkg.upbound.io/upbound/<provider>@sha256:<digest> \
  --certificate-identity https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Sidecar counts on `upbound/provider-aws-sqs` (VERIFIED): **135 `.sig`, 135 `.att`, 74 `.sbom`** out of 446 tags. Attestations are SPDX JSON (`cosign verify-attestation --type spdxjson`).

**crossplane-contrib does not sign.** `ghcr.io/crossplane-contrib/provider-aws-s3` — **0 `.sig`, 0 `.att`** across all 18 tags (VERIFIED). Same for the mirrored contrib repos on `xpkg.upbound.io` (`provider-kubernetes`: 96 tags, 0 sidecars). Report this as a fact, not as a warning — a huge share of legitimate community usage is unsigned, and crying wolf trains people to ignore the badge.

**Can `cf` verify without shelling out to cosign?** Yes. `github.com/sigstore/sigstore-go` verifies a bundle in-process against the embedded certificate and the trusted-root, with no network if the bundle is present. That is ~1 dependency and no subprocess. Do that.

**Discovery caveat:** the OCI 1.1 **referrers API 404s on both registries** (VERIFIED, R5/R8). Signature discovery *must* use the cosign tag convention: compute the manifest digest, transform `sha256:abcd…` → `sha256-abcd….sig`, fetch that tag. Do not build on referrers.

### What the UI should show

Per provider, in this order of prominence:

| Field | Source | Verifiable? |
|---|---|---|
| **Publisher + registry host** — `upbound` on `xpkg.upbound.io` | the OCI ref itself | yes, by construction |
| **Signature verdict** — `✓ signed · github.com/upbound/upbound-official-build` / `unsigned` | cosign sidecar tag + `sigstore-go` | **yes** |
| **Resolved digest**, always shown next to the tag | manifest | yes |
| **Licence** — `Apache-2.0` | `meta.crossplane.io/license` (in-band) | yes (covered by sig) |
| **Maintainer** — `Upbound <support@upbound.io>` | `meta.crossplane.io/maintainer` (in-band) | yes |
| **Source repo** — `github.com/crossplane-contrib/provider-upjet-aws` | `meta.crossplane.io/source` (in-band) | yes |
| **Crossplane compatibility** — `>=v1.12.1-0` | `spec.crossplane.version` (in-band) | yes |
| **Family / dependencies** | `spec.dependsOn`, `pkg.crossplane.io/provider-family` | yes |
| **Vendor verification claim** — `Official`, `Upbound support`, hardening list | `meta.upbound.io/*` (in-band) | yes, *as a claim by the publisher* |
| Downloads, tier, updatedAt | marketplace index | **no — label as unverified** |
| Icon | `assets.upbound.io` signed URL, expires in 5 min | no — see below |

**Three rules for the rendering:**

1. **Separate verified facts from claims, visually.** A `✓` next to "signed by github.com/upbound/upbound-official-build" is a cryptographic statement. `OFFICIAL` is a vendor's self-description. They must not share a badge style. My proposed split: signature verdict gets a lock glyph and a colour; tier gets a plain uppercase text label with no colour.

2. **Never render remote content directly, and never hotlink.** The marketplace `iconURL` is a signed URL with `Expires=1787866961` (a ~5-minute TTL) — hotlinking it means a broken image and a per-render request to Upbound revealing which providers each user browses. Fetch icons once at **index build time**, re-host them in the index repo, and cap them. Descriptions and `friendly-name` are third-party strings: HTML-escape unconditionally, strip control characters, truncate, and do not follow or auto-link URLs found inside them.

3. **Downloads are the most persuasive and least trustworthy number on the card.** They are unauditable, unnormalised (CI pulls dominate), and only exist for Upbound-hosted packages — there is no equivalent from GHCR, so a contrib-only provider would show blank and read as "unpopular". Either render them for both sources or render them for neither. My recommendation: show them, from the index, with the source labelled (`1.9M pulls (Upbound Marketplace)`), and never let them feed resolution.

**A concrete honesty case to design for:** `upbound/provider-aws-s3` is signed, has schema layers, and pins its family dependency; `crossplane-contrib/provider-aws-s3` is unsigned with an unpinned `>= 0.0.0` family dependency — and **both are built from the same source repo**. A UI that showed only "official vs community" would imply a code difference that does not exist; a UI that showed `source:` and the signature verdict tells the truth: same code, different supply chain.

---

## 6. Failure modes to design against

### 6.1 Index staleness

*Symptom:* the index lists `v2.7.1`; `v2.8.0` shipped this morning. Or a provider was renamed/withdrawn.

- **Always show the age.** In the CLI footer of every search, in the GUI header, in `cf index status`. Never make the user guess.
- **Escalate by age, never block:** < 24 h silent; 1–7 d a one-line note; > 7 d a prominent note naming `cf index update`; > 30 d the note names the possibility that the index URL has moved and points at `cf index status` for the last error.
- **The index is never authoritative about versions at add time.** `cf provider add aws-sqs` with no explicit version does a live `tags/list` (2 requests, ~600 ms, 6.8 KB gz — VERIFIED) and takes the newest, falling back to the index's `latest` if offline. Cheap enough to always do; the index exists for *browsing*, not for pinning.
- **Serve stale on error forever.** Terraform's registry uses `stale-if-error=31536000` (VERIFIED off the wire); adopt the same posture in the client. A failed refresh must never invalidate what is already cached.
- **Let users pin the index revision.** `indexes: {default: github.com/<org>/cf-index@2026-08-28}` in `.cf/providers.yaml` for reproducible CI.

### 6.2 Index says it exists, the OCI ref 404s

*Causes:* provider deleted or made private; the index recorded a bad publisher/repo; a family member that never existed for that service; a floating tag repointed.

**The hard part: Upbound cannot distinguish "gone" from "private".** For a nonexistent repository, the *token endpoint itself* 404s and the registry then returns **401, not 404** (VERIFIED):

```
$ curl -s -o /dev/null -w '%{http_code}\n' \
    'https://xpkg.upbound.io/service/token?scope=repository%3Aupbound%2Fprovider-does-not-exist%3Apull&service=xpkg.upbound.io'
404
$ curl -H 'Authorization: Bearer ' https://xpkg.upbound.io/v2/upbound/provider-does-not-exist/tags/list
HTTP 401  {"errors":[{"code":"UNAUTHORIZED","message":"authentication required", …}]}
```

GHCR behaves correctly by comparison: nonexistent repo → **404** (VERIFIED). A *valid* repo with a bad tag → **404 `MANIFEST_UNKNOWN`, `"detail":"unknown tag=v99.0.0"`** on Upbound (VERIFIED).

So the client must classify by **which step failed**, not by the final status code:

| Signal | Meaning | Message |
|---|---|---|
| token endpoint 404 | repo does not exist *or* is private | `xpkg.upbound.io/upbound/provider-foo not found. It may have been removed, renamed, or made private. (Upbound's registry cannot distinguish these.) Try: cf provider search foo` |
| token 200, `tags/list` 401 | scope bug in the client — **our fault** | internal error; log the scope string |
| `tags/list` 200, `manifests/<tag>` 404 `MANIFEST_UNKNOWN` | version gone | `v2.8.0 is not published for …/provider-aws-sqs. Available: v2.7.1, v2.7.0, … (cf provider versions)` |
| manifest 200, blob 404 | registry inconsistency / partial GC | retry once, then report a registry-side error, not a user error |

- **Never let one dead entry break browsing.** A 404 on add is a per-provider failure; search and list must still work.
- **Self-healing:** on a confirmed not-found, mark the index entry `stale-404` locally with a timestamp, grey it in the GUI, and trigger a background index refresh. If the refresh still lists it, surface a "report this" affordance — that is an index bug and the index repo is the place to file it.
- **Index CI must verify.** The daily build resolves every entry's `latest` to a digest; anything that fails becomes `"status":"unresolvable"` in the published index rather than a silent landmine. This is the single highest-value thing the build job does.

### 6.3 Rate limiting

Measured limits:

| Endpoint | Limit | Headers |
|---|---|---|
| `xpkg.upbound.io` registry | **none observed** — 12 rapid `tags/list` and 10 rapid token requests all 200 | **no `RateLimit-*` or `Retry-After` headers at all** |
| GitHub REST, unauthenticated | **60/hr** | `x-ratelimit-limit/remaining/used/reset` |
| GitHub REST, authenticated | **5000/hr** | same |
| GitHub search, unauth / auth | **10/min / 30/min** | `x-ratelimit-resource: search` |
| Docker Hub search | **180/window** | `x-ratelimit-limit/remaining/reset/ip` |
| ArtifactHub | none observed | `pagination-total-count` only |

**The absence of headers on `xpkg.upbound.io` is the design problem.** There is nothing to read, so `cf` cannot be a good citizen reactively — it must be one by construction:

- **Client-side concurrency cap of 4** against any single registry host, and a token bucket of ~10 req/s. This costs nothing at `cf`'s natural request volume (a `provider add` is 5 requests) and guarantees a single user can never look like an attack.
- **Reuse the bearer token across requests to the same repo.** The JWT is valid ~30 minutes; re-fetching it per request doubles the request count for zero benefit. Cache in-memory keyed by `(registry, repo, action)`.
- **Send `Accept-Encoding: gzip`.** Verified 4.5× reduction on `tags/list` (30,558 → 6,767 bytes). Go's default transport does this only if you don't set the header manually; be sure not to break it.
- **Set a descriptive `User-Agent`** — `cf/<version> (+https://github.com/<org>/compositionfactory)` — so an operator who does need to throttle can talk to us instead of blanket-blocking.
- **Honour `Retry-After` and `RateLimit-*` if they ever appear**, and treat 429 and 503 as retryable with jittered exponential backoff (base 1 s, cap 30 s, max 3 attempts). Treat 401/403/404 as **not** retryable — retrying a 401 is what turns a client bug into an abuse pattern.
- **Never retry inside a search.** Search is index-local; if a live call is somehow needed and it 429s, degrade to index data and annotate.
- **Move the aggregate load to the build job.** This is the strongest mitigation and it falls out of the static-index decision: the ~1,800 registry requests needed to enumerate every version of every package happen **once per day from one IP**, not once per user per browse. A live-query design would multiply that by the user count.

### 6.4 Others worth designing against

- **Mutable-tag TOCTOU.** A known Crossplane advisory (GHSA-wfqx-gjrf-g28r) covers exactly this: verify a signature by tag, then install by tag, and a malicious registry can serve different bytes for the second request. `cf` must **resolve tag → digest once, then use the digest for every subsequent request**, including the signature lookup (`sha256-<digest>.sig` is digest-derived anyway) — and write the digest to the lock file.
- **The pre-existing floating-tag cache poison.** `~/.crossplane/cache/xpkg.upbound.io/upbound/provider-aws-sqs@v2/` is on this machine right now (VERIFIED) and is keyed by a tag that moves. If `cf` shares that directory it inherits a permanently-stale entry. Read from it opportunistically, but **write digest-keyed**, and never trust a `@<floating-tag>` directory without re-resolving.
- **Very large tag lists.** `crossplane/crossplane` returns 3,578 tags / 99.5 KB in 2.87 s (VERIFIED). Parse streaming, cap the version list shown to the newest N with a "show all", and never render 3,578 rows into a palette.
- **Cosign sidecars polluting version lists.** 345 of 446 tags on `provider-aws-sqs` are `.sig`/`.att`/`.sbom` (VERIFIED). Filter `^sha256-[0-9a-f]{64}\.(sig|att|sbom)$` before anything else, or the version picker is 77% noise.
- **Family members hidden by default.** The marketplace defaults to `excludeFamily: true` (153 shown of 348). `cf provider search aws` should show `aws-s3`, `aws-sqs` etc. because that is what the user means — but `cf provider list --available` should default to roots, with `--all`. Follow the marketplace's own implicit rule: **a search query includes family members, a browse does not** (VERIFIED — the marketplace flips `excludeFamily` to `false` as soon as `?query=` is set).
- **Registry aliasing confusion.** `xpkg.crossplane.io` authenticates against `ghcr.io` (VERIFIED, R9). A client that derives the token realm from the requested hostname will fail. Always read the realm from the `WWW-Authenticate` header — which, again, is what go-containerregistry does and hand-rolled code does not.

---

## What I did not verify

- The exact refresh mechanics of `krew update` (shallow fetch vs full clone) — DOCS + prior knowledge; `krew` is not installed here.
- Whether Upbound's `packageSearch` backend is reachable directly (I found only SSR; the six API shapes I guessed all 404).
- Upbound's terms of service for programmatic access — **the document is not at any of four obvious URLs, all 404** (VERIFIED). Worth an email before shipping an index built from marketplace data.
- Whether `sigstore-go` verifies these specific bundles end-to-end in Go — I decoded the certificate and confirmed the bundle is present and self-contained, but did not run a verification.
- Long-run rate limits on `xpkg.upbound.io`. I tested a 12-request burst; a 2,000-request index build is a different question and should be run once, observed, and throttled accordingly.
- GHCR anonymous rate limits (I made only a handful of requests).

## Scratch artefacts

- `disc/all_providers.json` — the full 153-provider marketplace crawl, the basis for every sizing and collision number above.
- `.../disc/contrib_pkgs.txt` — 439 `ghcr.io/crossplane-contrib` package names (409 `provider-*`).
- `.../disc/cosign.pem` — the decoded Upbound signing certificate.
- `.../disc/scrape.py`, `.../disc/mkq.py` — the marketplace crawlers.

## Sources

- [Terraform provider registry protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol)
- [Terraform provider network mirror protocol](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol)
- [Krew custom indexes](https://krew.sigs.k8s.io/docs/user-guide/custom-indexes/)
- [Upbound signature verification](https://docs.upbound.io/providers/signature-verification)
- [Crossplane Image Configs (signature verification)](https://docs.crossplane.io/latest/packages/image-configs/)
- [Crossplane signature-verification TOCTOU advisory GHSA-wfqx-gjrf-g28r](https://advisories.gitlab.com/golang/github.com/crossplane/crossplane/GHSA-wfqx-gjrf-g28r/)
- [Feature: Enable package signature validation with cosign (crossplane#3048)](https://github.com/crossplane/crossplane/issues/3048)
- [New Default Crossplane Registry in Crossplane 1.15](https://blog.crossplane.io/new-default-crossplane-registry-in-crossplane-1-15/)
- [Upbound Marketplace providers](https://marketplace.upbound.io/providers)
- [Crossplane packages: providers](https://docs.crossplane.io/latest/packages/providers/)
