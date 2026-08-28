# Provider discovery — synthesis

**Date:** 2026-08-28 · **Status:** decided, with three UNRESOLVED conflicts flagged inline
**Sources:** four probe briefs in `docs/research/raw/` — `disc-marketplace.md`, `disc-artifacthub.md`,
`disc-github.md`, `disc-ux-cache.md`. All four exist and were read in full; none missing.

**VERIFIED** = a brief pasted real output from a request it ran. **DOCS** = read, not executed.
**UNRESOLVED** = two briefs ran overlapping probes and got different answers.

---

## 1. Verdict in five sentences

Browsing providers is feasible, but only if the catalogue is built once in CI and shipped as a
static index — **no channel permits anonymous run-time enumeration of the provider namespace**
(`xpkg.upbound.io/v2/_catalog` → **401** even with a repo-scoped token; `api.github.com/orgs/crossplane-contrib/packages`
→ **401** unauthenticated; both VERIFIED). Two channels carry real data: the Upbound marketplace HTTP
API at `https://api.upbound.io/v2/search` is the only complete name-and-version catalogue
(**626** providers, anonymous, 30/30 requests returned 200 with no rate-limit headers — VERIFIED, but
see **UNRESOLVED-1**), and the OCI registries are the authoritative, unthrottled, anonymous source for
versions, digests and existence. GitHub is a supplement — excellent for repo metadata, family expansion
(**177/177** AWS sub-packages derived and confirmed) and licence, but a **factually wrong version source**
(its latest `provider-upjet-aws` release is `v2.7.0` while all 177 registry packages ship `v2.7.1`,
which has no Git tag and no GitHub release — both 404, VERIFIED) and blind to the ~350 family packages
that have no GitHub repository at all (`upbound/provider-aws-sqs` → **404**). Artifact Hub is a flat
no — it supports exactly 29 package kinds (`0`–`28`), none of them Crossplane, **0 of 9** target
providers are present, and there is no feature request or roadmap item (VERIFIED). Finally, the
standing negative result that `https://xpkg.upbound.io/v2/<repo>/tags/list` returns an empty tag list
is **retracted by three independent briefs**: it is an unauthenticated **401** whose body has no `tags`
key, and with a repository-scoped anonymous bearer minted at `https://xpkg.upbound.io/service/token`
(note: `/service/token`, **not** `/token`, which 404s) it returns **HTTP 200 with 446 tags**.

---

## 2. Channel comparison

Every cell traces to a probe. `[M]` = `disc-marketplace.md`, `[A]` = `disc-artifacthub.md`,
`[G]` = `disc-github.md`, `[U]` = `disc-ux-cache.md`.

### 2.1 Coverage

| Channel | Coverage | Ev. |
|---|---|---|
| **Upbound marketplace API** (`api.upbound.io`) | `filter=packageType = "Provider"` → **626**; `+ excludeFamily = true` → **151**; `packageType = "Function"` → **109**; `packageType = "Configuration"` → **76**; `account = "crossplane-contrib"` → **89** | VERIFIED [M] |
| — same, via SSR crawl instead of the API | `{"count":348,"filteredCount":153}` for `packageSearch{packageType:Provider,excludeFamily:true}`; 7 pages, 2,059,596 B HTML for 153 records; `?size=100` **ignored** (always 24) | VERIFIED [U] — **UNRESOLVED-2** vs 626/151 |
| — sitemap fallback | `marketplace.upbound.io/sitemap.xml` → 200, 11,413 B, 103 `<loc>`; **94** are `/providers/...` [M] / **95** providers [U]. 94 of 626 — badly incomplete | VERIFIED [M][U] |
| **Artifact Hub** | **Zero.** 21,142 packages across 29 kinds `0`–`28`; per-kind totals sum to exactly 21142, so the enumeration is exhaustive; **no Crossplane kind exists**. `provider-aws-sqs`, `-gcp-storage`, `-kubernetes`, `-helm`, `-terraform`, `-argocd`, `-github`, `-sql`, `-vault` → `Pagination-Total-Count: 0` for all nine. `repositories/search?name=xpkg` → **0**. Zero of 385 container-image repos reference `xpkg`/`crossplane`/`upbound` | VERIFIED [A] |
| **GitHub** | `crossplane-contrib`: 140 public repos, **87** `provider-*` (**50 active, 37 archived**), 22 `function-*`. `upbound`: 210 repos, **11** `provider-*`. `upbound/provider-{aws,azure,gcp}` all **301** → `crossplane-contrib/provider-upjet-*`. **`upbound/provider-aws-sqs` → 404** — and so do the other 176 AWS, 93 Azure and 81 GCP service packages: **≈350 of the most-used packages have no repo** | VERIFIED [G] |
| — third party outside both orgs | `oracle/`, `grafana/`, `yandex-cloud/`, `scaleway/`, `SAP/`, `valkiriaaquatica/` — none follows `provider-*`; finding them needs **authenticated** code search (`search/code?q="kind: Provider" filename:crossplane.yaml path:package` → `total_count 459`) | VERIFIED [G] |
| **OCI registry** | **Cannot enumerate.** `xpkg.upbound.io/v2/_catalog?n=50` with an anon token → **401** `{"Type":"registry","Name":"catalog","Action":"*"}`. Per-repo coverage is complete and authoritative. `ghcr.io/crossplane-contrib` holds **409** `provider-*` packages (179 `aws-*`, 99 `azure-*`, 82 `gcp-*`, 20 `alibabacloud-*`) but only via the **auth-gated** GitHub Packages API | VERIFIED [M][U][G] |

### 2.2 Versions available

| Channel | Versions | Ev. |
|---|---|---|
| **Upbound marketplace API** | `GET /v1/packageMetadata/{acct}/{repo}` → 200, 377 B, flat newest-first `{"versions":["v1.3.0","v1.2.1",…]}` — **27** for `crossplane-contrib/provider-kubernetes`. `GET /v2/packageMetadata/{acct}/{repo}/{ver\|latest}` → 200, 642 B, `{"currentVersion","digest","endOfLife":"2028-02-10","endOfSupport":"2027-08-10","familyRepoKey","hasSignature","publishedAt","tier"}`. The v2 list form is 30 KB and noisier — use v1 | VERIFIED [M] |
| **Artifact Hub** | Schema is ideal — `available_versions[]` complete and inline (**2,713** entries, which is why the detail response is **412,325 B**) — but there is no Crossplane content to version | VERIFIED [A] |
| **GitHub** | **Wrong, in both directions.** `provider-upjet-aws`: GitHub 152 tags / OCI 102 version tags / intersection **82**; 20 OCI-only including `v1`, `v2` and **`v2.7.1`**. `releases/latest` → `v2.7.0`; `git/ref/tags/v2.7.1` → **404**; `releases/tags/v2.7.1` → **404**. `provider-helm`: GitHub 71 / OCI 52 / intersection 22 — **7 GitHub releases (`v0.19.1`–`v0.20.3`) have no pullable package on any registry**. `provider-kubernetes`: 56 / 96 / intersection **27**. `provider-keycloak`: OCI **1039** version tags vs GitHub 88. Release assets: **1 of 60** repos | VERIFIED [G] |
| **OCI registry** | Authoritative. `upbound/provider-aws-sqs/tags/list` → 200, **446 tags** = 344 cosign `.sig`/`.att`/`.sbom` (**77%**) + **102** non-cosign (100 semver-shaped + floating `v1`,`v2`). `provider-family-aws` → 541 tags / 180 version tags, incl. a parallel `-cve` rebuild line (`v2.7.0-cve`) and commit-suffixed builds (`v2.6.0-1533af9f9678`). `crossplane/crossplane` → 3,578 tags / 99.5 KB / 2.87 s. **Registry-dependent:** `provider-helm` has 52 version tags on `xpkg.upbound.io` and **10** on `ghcr.io`; `provider-kubernetes` has 9 tags on ghcr vs **27** versions on the marketplace — **ghcr is a partial mirror** | VERIFIED [M][G][U]; **UNRESOLVED-4** on 102 vs 101 |

### 2.3 Auth needed

| Channel | Auth | Ev. |
|---|---|---|
| **Upbound marketplace API** | **None** for `/v2/search`, `/v1|v2/packageMetadata`, `/v1/packages/.../resources`. But `/v1/repositories/{acct}` and `/v2/repositories/{acct}` → **401** even for a public org. **No CORS at all**: `OPTIONS /v2/search` → **405**; `GET` with `Origin: http://localhost:8080` → 200 with **no `Access-Control-Allow-Origin`** — a browser cannot call it, the Go binary must proxy | VERIFIED [M] |
| **Artifact Hub** | None for every read endpoint. `X-API-KEY-ID`/`X-API-KEY-SECRET` exist but authorise **write** ops only; **no documented higher rate-limit tier** | VERIFIED reads [A]; DOCS on keys [A] |
| **GitHub** | None for `orgs/{org}/repos`, `repos/{r}/contents/cmd/provider`, and all of `raw.githubusercontent.com`. **Required** for `orgs/{o}/packages?package_type=container` (**401** → 200 with token, then 439 container packages) and `search/code` | VERIFIED [G][U] |
| **OCI registry** | Anonymous bearer, but the challenge must be answered exactly. `xpkg.upbound.io/v2/` → **401** with `www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io"`. `xpkg.upbound.io/token?...` → **404**. A **scopeless** token is issued (200) but 401s on every repo. `xpkg.crossplane.io/v2/` advertises `realm="https://ghcr.io/token"` — mint at ghcr, the token is accepted verbatim and returns byte-identical output | VERIFIED [M][G][U] |

### 2.4 Rate limits

| Channel | Limits | Ev. |
|---|---|---|
| **Upbound marketplace API** | **None observed.** 30 consecutive `/v2/search` → 30×200, no 429, no `Retry-After`. **No rate-limit headers of any kind** — only `x-envoy-upstream-service-time`, `server: istio-envoy`. Latency 0.26–0.48 s. `size` caps at 500 (`size=1000` → 400 `number must be at most 500`) | VERIFIED [M] |
| **Artifact Hub** | **≈50 origin-bound requests / 30 s.** First **429** after 53 requests in 30.1 s (~1.8 req/s); reproduced. Recovery **46 s and 56 s**. The 429 has an **empty body, no `Retry-After`, no `X-RateLimit-*`** — a naive `json.load()` throws `JSONDecodeError` instead of surfacing the throttle. **CDN hits do not count**: 80 identical requests in 6.4 s (12.5 req/s) → 80 cache-hits, 0 429s. A throttled client at 0.67 req/s never tripped it. `limit` max **60** | VERIFIED [A] |
| **GitHub** | **60/hr unauthenticated core, per IP**, `x-ratelimit-limit/remaining/used/reset`. Exhaustion returns **403, not 429** — a handler that only checks 429 misreports it. Fixed hourly bucket: a burst locks the user out for up to ~30 min (observed reset 1833 s away). Search: **10/min unauth, 30/min auth**. Authenticated core 5000/hr. **`raw.githubusercontent.com` is budget-free** — 25 GETs, `used` delta **0**; Fastly, `cache-control: max-age=300` | VERIFIED [G][U]; secondary limits DOCS [G] |
| **OCI registry** | **None observed, on either registry.** 60 sequential anon token requests → 60×200 in 17 s; 177 parallel existence checks in 4.6 s; 12 rapid `tags/list` all 200; **zero `RateLimit-*` or `Retry-After` headers**. Nothing to read reactively — politeness must be by construction | VERIFIED [G][U] |

### 2.5 Licence for programmatic use

| Channel | Licence / terms | Ev. |
|---|---|---|
| **Upbound marketplace API** | `robots.txt` → 200, 158 B: `Disallow: /` for **GPTBot, ClaudeBot, Google-Extended**; **`User-agent: * / Allow: /`**; sitemap declared. `api.upbound.io/robots.txt` → **404**. Terms at **`https://www.upbound.io/terms-conditions`** → 200, last-modified Aug 20 2026; keyword scan **all zero**: `scrap` 0, `crawl` 0, `robot` 0, `automated` 0, `rate limit` 0, `bot ` 0. Nearest clause is §2.1 (reverse-engineering) and §2.3 (comply with "standard published policies") — a **subscription agreement binding a "Customer"**, not a browsewrap over anonymous HTTP. `/legal/terms-and-conditions` → 404 | VERIFIED [M]; **UNRESOLVED-5** vs [U] |
| **Artifact Hub** | Code **Apache-2.0**; **CNCF Incubating**. **No terms of service at all** — the JS bundle's complete legal link set is linuxfoundation.org, /trademark-usage, LF privacy policy, cncf.io/projects, twitter. `robots.txt` → 200 but the body is the **SPA HTML shell**, so there are no crawl directives either way. Caching is actively assumed (`cache-control: max-age=300` + three purpose-built dump endpoints). **Caveat:** indexed content is third-party and carries its own per-package `license` field, often `null` — a mirror must carry it through | VERIFIED [A] |
| **GitHub** | AUP defines scraping as automated extraction and states **"Scraping does not refer to the collection of information through our API."** API terms in ToS §H; prohibition is "excessive automated bulk activity". Repo content: **49/50** active `crossplane-contrib/provider-*` are Apache-2.0 (1 has no licence file); **11/11** `upbound/provider-*` Apache-2.0. `raw.githubusercontent.com` has no separate terms and, as a CDN rather than an API, is **more** exposed to the bulk clause | DOCS on AUP/ToS [G]; VERIFIED on licences [G] |
| **OCI registry** | No registry-specific terms probed by any brief. Anonymous pull is the documented install path for both. **Package content is Apache-2.0** — `meta.crossplane.io/license: Apache-2.0` in the in-band package meta | VERIFIED (package licence) [U]; terms **not probed** |

### 2.6 Freshness

| Channel | Freshness | Ev. |
|---|---|---|
| **Upbound marketplace API** | Live, with a per-package `updatedAt` (`"2026-08-23T23:08:42Z"`) and `publishedAt` (`"2026-08-21"`). Carries `endOfLife`/`endOfSupport`, so a picker can grey out dead versions | VERIFIED [M] |
| **Artifact Hub** | N/A — no content. The one near-miss, 16 KCL modules modelling Crossplane CRDs, is **2023–2024 stale**: `crossplane-provider-upjet-aws` at `1.23.0`, `crossplane-provider-kubernetes` at `0.18.0` | VERIFIED [A] |
| **GitHub** | Repo metadata is live (`pushed_at`, `archived`, `stargazers_count`) and is the **only** source of "this provider is dead" — 37 archived contrib providers. But version data lags the registry (§2.2). Curated lists are worse: `docs.crossplane.io` community-extensions page last touched **2026-04-29 (~4 months)**, is itself a `gh api orgs/crossplane-contrib/repos` scrape, and `grep -cE 'xpkg\.|ghcr\.io'` → **0** — it carries **zero install references**. Every `awesome-crossplane` is dead 2–4 years; `crossplane-contrib/awesome-crossplane` and `crossplane/awesome-crossplane` both **404** | VERIFIED [G] |
| **OCI registry** | Authoritative and instantaneous — it is where `v2.7.1` exists and GitHub does not | VERIFIED [G] |

### 2.7 Mechanical repo→OCI-ref resolution

| Channel | Resolution | Ev. |
|---|---|---|
| **Upbound marketplace API** | **Not needed — it returns the ref.** `repoKey` (`"upbound/provider-azapi"`) + registry host is the ref, and `pkgDigest` pins it with no registry round-trip. `familyRepoKey` names the family package in the same response — a one-request answer to "which second package do I need", without parsing `crossplane.yaml` | VERIFIED [M] |
| **Artifact Hub** | Right shape, no content: `content_url` is a fully-formed OCI ref for OCI packages (`oci://ghcr.io/quenchworks/charts/crossplane:0.0.2`). For non-OCI packages `install` is a **Markdown blob**, not structured data — never parse it | VERIFIED [A] |
| **GitHub** | **82%, and not statically decidable.** Over all 60 active provider repos: `Counter({'makefile': 46, None: 11, 'guess-xpkg': 1, 'readme': 1, 'guess-ghcr': 1})` → **49/60**. Rule: `first_token(XPKG_REG_ORGS_NO_PROMOTE \|\| XPKG_REG_ORGS) + "/" + first_token(XPKGS \|\| PROJECT_NAME)`, one pass of `$(VAR)` expansion. **A naive `xpkg.upbound.io/<gh-org>/<gh-repo>` is wrong for ≥12 of the 49** — 6 live on ghcr.io, 3 in third-party OCI orgs (`civo`, `equinix`, `upbound`), 4 where the package name differs, 2 where the Makefile is stale. The killer: `provider-sonarqube/.github/workflows/ci.yml:357` sets `XPKG_REG_ORGS` from a **GitHub Actions expression conditional on whether a CI secret is set** — nothing in the repo says which branch was taken. 10 of the 11 misses have 0 releases or were abandoned 2+ years ago; the only genuine miss is `provider-checkly`, which ships its package as a **GitHub release asset** and advertises a placeholder ref that does not exist. **Family expansion is fully mechanical and unavailable anywhere else**: `ls cmd/provider/` minus `monolith`, `config`→`provider-family-<n>` → AWS **177/177**, Azure **93/93**, GCP **81/81** confirmed present in the registry in 4.6 s. Absence of `cmd/provider/` is the correct family-vs-single discriminator. `package/crossplane.yaml` `metadata.name` matches the registry basename **36/36, 0 disagreements**, but misses the three upjet monorepos (generated at build time). GitHub **topics are useless** — 16 of 85 repos have any | VERIFIED [G] |
| **OCI registry** | **The oracle that closes the gap.** `GET xpkg.upbound.io/service/token?scope=repository:<repo>:pull&service=xpkg.upbound.io` → **200** if the repo exists, **404** if not — one request, no auth, no rate limit. Turns an 82% guess into a 100%-correct catalogue with an honest "publishes no package" state. **Caveat:** on Upbound a nonexistent repo yields token-404 and then `tags/list` **401**, so "gone" and "private" are indistinguishable; ghcr correctly returns **404**. A valid repo with a bad tag returns **404 `MANIFEST_UNKNOWN` `"detail":"unknown tag=v99.0.0"`** | VERIFIED [G][U] |

### 2.8 UNRESOLVED

| # | Conflict | Positions | How to settle |
|---|---|---|---|
| **1** | Does a public marketplace API exist? | `[M]`: **VERIFIED** `GET https://api.upbound.io/v2/search?type=packages&size=1` → **200**, `count: 626`, anonymous; found via the marketplace CSP `connect-src ... api.upbound.io` and the JS bundle's `this.search="/v2/search"`. `[U]` C2: **VERIFIED 404** for six shapes and concludes *"no public marketplace API exists"* — but the six were `marketplace.upbound.io/api/*`, `api.upbound.io/v1/marketplace/*` and `/_next/data/…`, **none of which is `api.upbound.io/v2/search`**. Likely reconcilable (different paths), not proven | One curl. Until then, every design decision below must survive `[M]` being wrong — hence the SSR-crawl fallback in §3.3 |
| **2** | How many providers exist? | `[M]`: `packageType = "Provider"` → **626**; `+ excludeFamily` → **151**; a top-level `?query=` is ignored and returns **820** packages. `[U]`: the SSR payload for the same nominal query reports `count: 348, filteredCount: 153`, and separately **409** `provider-*` in ghcr contrib. 626 vs 348 for the same filter is a real gap; 151 vs 153 is a minor one | Re-run both, same day, and diff the `repoKey` sets. Treat **500–900** as the planning range for index sizing either way |
| **3** | Does `tags/list` honour `?n=`/`last=` on `xpkg.upbound.io`? | `[M]`: **VERIFIED** `?n=100` → exactly 100 tags; `?n=100&last=v2.0.0` → 19 tags starting `["v2.0.1","v2.0.2","v2.1.0"]`. `[G]`: **VERIFIED** `?n=50` → a page plus `link: …rel="next"`. `[U]`: **VERIFIED** `?n=100` and `?n=1000` both return the **same 446 tags and no `Link` header** — "the registry ignores `n`" | Moot for the design: **all three agree that omitting `n` returns everything in one response**, so omit it. Also note `[G]`'s trap — tags sort lexicographically, so page 1 of `?n=50` is 100% cosign noise |
| **4** | Version-tag count on `upbound/provider-aws-sqs` | `[M]` and `[G]`: 446 total, 344 cosign, **102** real. `[U]`: 446 total, 135 `.sig` + 135 `.att` + 74 `.sbom` = 344, then "**101** real version tags plus the floating aliases `v1` and `v2`" — which totals 103. Off-by-one somewhere | Cosmetic. Use `446 − 344 = 102 non-cosign tags` and derive the rest by filter |
| **5** | Does Upbound publish terms of service? | `[M]`: **VERIFIED 200** at `https://www.upbound.io/terms-conditions`, quoted §2.1/§2.3. `[U]`: **VERIFIED 404** at `www.upbound.io/legal`, `upbound.io/terms-of-service`, `www.upbound.io/terms-and-conditions`, `marketplace.upbound.io/terms`, concluding no ToS exists | Reconciled by URL — the live document is `/terms-conditions` (no "and"). Treat `[M]`'s reading as the operative one; `[U]`'s four 404s stand as separate true negatives |
| **6** | Sitemap provider count | `[M]`: 94 of 103 `<loc>` are `/providers/...`. `[U]`: 95 providers, 6 functions, 1 configuration, 1 root | Immaterial — either way it is ~15% of the catalogue and unusable as an index |

---

## 3. Recommended design

### 3.1 Static index, not live queries

**Decision: a static, CI-rebuilt index file, fetched over plain HTTPS, plus a copy embedded in the
binary.** Three measured reasons:

1. **No anonymous enumeration path exists at run time.** `_catalog` → 401, GitHub Packages → 401
   (both VERIFIED). Live browse would require shipping a credential, or hitting `api.upbound.io`
   (UNRESOLVED-1) / scraping SSR from every user's machine at N× users — which `[U]` explicitly
   declines to ship in a distributed binary given the `robots.txt` posture.
2. **It is tiny** (§3.2).
3. **Offline is the requirement, not a nice-to-have.** A live-query design makes discovery a network
   dependency by construction.

This also resolves the one genuine policy divergence between the briefs: `[M]` recommends building the
tool on `api.upbound.io`; `[U]` recommends no per-user marketplace traffic at all. **Both are satisfied
by moving the aggregate load into the daily build job** — ~1,800 registry requests happen once per day
from one IP with one `User-Agent`, instead of once per user per keystroke.

### 3.2 Size — measured, not estimated

| Tier | Contents | 153 providers (measured) | →350 pkgs | →900 pkgs |
|---|---|---|---|---|
| **A** search only | id, registry, latest, tier, downloads, 140-char description | **27,554 B raw / 5,241 B gzip** (180 B/pkg) | ~63 KB / ~12 KB gz | ~162 KB / ~31 KB gz |
| **B** A + every version | + `versions[]` (~100 each) | 166 KB raw / **7.2 KB gzip** | ~380 KB / ~16 KB gz | ~978 KB / **~42 KB gz** |

VERIFIED [U], from the real 153-provider crawl. **Ship tier B** — version strings compress almost
perfectly, so the whole version history costs ~11 KB over tier A at the 900-package upper bound, and it
makes `cf provider versions` work fully offline. Context: krew's index is 210 KB for 402 plugins (5×
larger); `bitnami-index.yaml` on the research machine is **26 MB** (620× larger).

Realistic cardinality: **500–900** entries after dedup (626 marketplace providers / 348 packages —
UNRESOLVED-2 — unioned with 409 ghcr `provider-*`).

### 3.3 Where it is fetched from, and how it is built

**Published** at two URLs backed by one git repo, with a `mirrors[]` array inside the index so the
*next* fetch can rotate without a client upgrade:

```
https://raw.githubusercontent.com/koorikla/cf-index/main/index-v1.json.gz     # ~40 KB
https://raw.githubusercontent.com/koorikla/cf-index/main/index-v1.json.sha256
```

`raw.githubusercontent.com` serves `ETag` + `cache-control: max-age=300`, needs no auth, and **does not
consume the 60/hr API budget** (VERIFIED [G]: 25 GETs, `used` delta 0).

**Also `go:embed` a seed copy of the index in the binary**, so first run works offline with zero
network and zero prior `cf index update`. This is where the "4-months-stale curated list" problem gets
solved: the build refreshes it, not the user's laptop.

**Built daily by a GitHub Action** in the `cf-index` repo:

| Step | Source | Requests | Auth |
|---|---|---|---|
| 1. Enumerate | `api.upbound.io/v2/search?type=packages&size=500&filter=packageType = "Provider"` | **2** (626 results) | none |
| 1-fallback | if UNRESOLVED-1 resolves against us: SSR crawl of `marketplace.upbound.io/providers?page=N`, 7 pages / 2.06 MB | 7 | none |
| 2. Contrib names | `api.github.com/orgs/crossplane-contrib/packages?package_type=container` | ~5 | `GITHUB_TOKEN` (free in Actions) |
| 3. Family expansion | `api.github.com/repos/{r}/contents/cmd/provider` for the 3 upjet monorepos | 3 | none |
| 4. repo→ref hypothesis | `raw.githubusercontent.com/{r}/{branch}/Makefile`, `package/crossplane.yaml`, `.github/workflows/*.yml` | ~200 | none, budget-free |
| 5. **Validate every entry** | `xpkg.upbound.io/service/token?scope=repository:{repo}:pull` → 200/404 | ~900 | none |
| 6. Versions | `tags/list` per package, `Accept-Encoding: gzip` (30,558 → 6,767 B, 4.5×) | ~2×900 | anon bearer |
| 7. In-band meta | full xpkg pull for new/changed digests only — 5 requests, 28 KB each | 5×Δ | none |

~15 min serial, ~2 min at concurrency 8. **Step 5 is the highest-value step**: anything that fails
validation is published as `"status":"unresolvable"` rather than becoming a silent landmine in a picker.

**Index schema v1** (every field populated from something a brief actually retrieved):

```json
{
  "schemaVersion": 1,
  "generatedAt": "2026-08-28T04:00:00Z",
  "mirrors": ["https://cf-index.example.dev/index-v1.json.gz"],
  "publishers": {
    "upbound": {"registry": "xpkg.upbound.io", "tier": "official",
      "signing": {"identity": "https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main",
                  "issuer": "https://token.actions.githubusercontent.com"}},
    "crossplane-contrib": {"registry": "xpkg.crossplane.io", "tier": "community", "signing": null}
  },
  "providers": [{
    "publisher": "upbound", "name": "provider-aws-sqs", "short": "aws-sqs",
    "friendly": "Provider AWS (sqs)", "family": "provider-family-aws",
    "dependsOn": [{"provider": "xpkg.upbound.io/upbound/provider-family-aws", "version": "v2.4.0"}],
    "license": "Apache-2.0", "source": "github.com/crossplane-contrib/provider-upjet-aws",
    "maintainer": "Upbound <support@upbound.io>", "crossplaneVersion": ">=v1.12.1-0",
    "capabilities": ["SafeStart"], "downloads": 1961953, "updatedAt": "2026-08-23T23:08:42Z",
    "signed": true, "status": "ok",
    "latest": "v2.7.1", "versions": ["v2.7.1", "v2.7.0", "…"],
    "digests": {"v2.7.1": "sha256:e3aaedccfcc3022bed7763fb3f5a48b4ce5ae915e6dc5b2032688cb06f8aaf11"}
  }]
}
```

`friendly`, `family`, `dependsOn`, `license`, `source`, `maintainer`, `crossplaneVersion`,
`capabilities` come from the **in-band** `meta.pkg.crossplane.io/v1` document (VERIFIED [U]);
`downloads`, `tier`, `updatedAt` from the marketplace; `versions`/`digests` from `tags/list` +
manifest. Field naming deliberately tracks Artifact Hub's package model so a future integration would
be a straight mapping.

**`digests` is the load-bearing field** — it lets `cf` resolve a tag to a digest offline and pin it,
closing the mutable-tag hole (GHSA-wfqx-gjrf-g28r).

**Multiple indexes, krew-style:** `cf index add <name> <url>`. An enterprise publishes its own index
JSON and its providers become addressable as `<index>/<publisher>/<name>`. This is ~40 lines of
documentation, not a protocol.

### 3.4 Cache location, TTLs, offline behaviour

Root `$CF_CACHE`, default `$XDG_CACHE_HOME/cf` (`~/Library/Caches/cf` on macOS).

| Layer | Path | Key | Soft TTL | Hard TTL | On expiry |
|---|---|---|---|---|---|
| **L0** index | `index/<name>/index-v1.json` + `.etag` | index name | **24 h** | never | conditional GET `If-None-Match`; on failure serve stale + warn once |
| **L1** tag→digest | `resolve/<registry>/<repo>/<tag>.json` | registry+repo+tag | **1 h** floating / **never** exact semver | 30 d | re-resolve; on failure serve stale + warn |
| **L2** package content | `pkg/<registry>/<repo>/<digest>/package.yaml` | **content digest** | never | never | immutable; LRU GC above a size cap |
| **L3** derived schemas | `schema/<digest>/<group>/<version>/<kind>.json` | content digest | never | never | rebuildable from L2 |
| **L4** signature verdicts | `verify/<digest>.json` | content digest | 7 d | never | re-verify; stale shown as "verified 12 days ago" |

L1's split is the point: **an exact semver tag on a content-addressed registry is immutable in practice
and should be cached forever; a floating tag is a pointer.** Detect syntactically — `^v\d+$`,
`^v\d+\.\d+$`, `latest`, `stable`, `main` are floating; a full `vX.Y.Z` is not.

**Read-through from `~/.crossplane/cache` opportunistically** so `cf` and `crossplane xpkg get-crds`
warm each other (18 MB / 11 packages on the research machine, VERIFIED). **But never write there and
never trust a `@<floating-tag>` directory** — `~/.crossplane/cache/xpkg.upbound.io/upbound/provider-aws-sqs@v2/`
exists on that machine right now and is permanently stale by construction.

**Exact offline behaviour — a three-state contract.** Every read returns `(value, freshness)` with
freshness ∈ {`fresh`, `stale(age)`, `absent`}:

- **`fresh`** → normal output, no annotation.
- **`stale`** → **full normal output**, exit **0**, plus one line to stderr:
  `note: provider index is 9 days old (offline); run 'cf index update' when connected`.
  Never a prompt, never a blocking retry.
- **`absent`** → the only failure, and it must distinguish "never fetched" from "fetch failed":
  `error: no provider index cached. 'cf provider search' needs an index; run 'cf index update' (needs network), or 'cf index add <name> <file://path>' for an air-gapped index.`

Escalation by age, never blocking: `<24 h` silent · `1–7 d` one-line note · `>7 d` prominent note naming
`cf index update` · `>30 d` note also raises that the index URL may have moved, pointing at
`cf index status` for the last error.

**Timeouts: 3 s connect / 8 s total** for index refresh, then fall through to cache. A tool that hangs
30 s on a captive portal is worse offline than one with no network code.

**Serve stale on error, forever.** Terraform's own registry ships
`cache-control: public, max-age=30, stale-while-revalidate=1800, stale-if-error=31536000` (VERIFIED off
the wire [U]) — one year of serve-stale-on-error. Adopt the same posture client-side: **a failed refresh
must never invalidate what is already cached.**

**`--offline` / `CF_OFFLINE=1`** makes any network attempt an error rather than a fallback, for CI
reproducibility. **`CF_NO_AUTO_UPDATE=1`** from day one — Homebrew's blocking auto-update is the
anti-pattern. Background refresh at most once per 24 h, triggered by any `cf provider *` command,
**never blocking**; print the result of the *previous* refresh, not this one.

**Air gap:** `cf index export ./cf-bundle` → `index-v1.json` + every referenced package layer;
`cf index add corp file://./cf-bundle` on the far side. Digest-addressed, so verifiable on arrival with
no network. (Borrowed wholesale from `terraform providers mirror`.)

### 3.5 CLI surface

One search verb, not helm's two. Every resolution prints what it resolved to. Ambiguity is an error
with a copy-pasteable fix.

| Command | Network | Offline behaviour |
|---|---|---|
| `cf provider search <term>` | none | reads L0; warns if stale |
| `cf provider search --all <term>` | none | includes family members (default hides them) |
| `cf provider list --available [--publisher X] [--tier official] [--signed]` | none | reads L0 |
| `cf provider versions <ref>` | none (index carries all versions) | works offline |
| `cf provider info <ref>` | none for index fields; L2 for CRD counts | marks uncached fields `(not cached)` |
| `cf provider add <ref>` | yes, unless L2 hit | `--offline` errors unless the digest is cached |
| `cf provider pin <short> <publisher>` | none | writes `.cf/providers.yaml` |
| `cf index update [--all]` | **yes** | the only command that *must* have network |
| `cf index status` | none | per-index: name, url, age, provider count, last error |
| `cf index add\|remove\|list <name> <url>` | on add only | `file://` accepted |
| `cf index export <dir>` | yes | air-gap bundle |

```
$ cf provider search sqs

  NAME              PUBLISHER              LATEST   SIGNED  DOWNLOADS  DESCRIPTION
→ aws-sqs           upbound     official   v2.7.1   yes       1.9M     Manage AWS sqs services
  aws-lambda        upbound     official   v2.7.1   yes       655K     Manage AWS lambda services
  aws               crossplane-contrib     v0.59.0  no        3.0M     Community AWS provider (deprecated)

  index: 4 hours old · 153 providers · 'cf provider add aws-sqs' to use the first result
```

```
$ cf provider add aws-sqs

  resolved  aws-sqs → xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1
            digest  sha256:e3aaedccfcc3022bed7763fb3f5a48b4ce5ae915e6dc5b2032688cb06f8aaf11
            signed  cosign · github.com/upbound/upbound-official-build (verified)
            family  also fetching xpkg.upbound.io/upbound/provider-family-aws:v2.4.0 (required by dependsOn)

  fetched   provider-aws-sqs     184 KB   8 CRDs   (5 requests, 28 KB over the wire)
  fetched   provider-family-aws   12 KB   2 CRDs
```

The family fetch is **automatic and announced** — spec §4 already establishes that
`crossplane xpkg get-crds` does not resolve `spec.dependsOn`, and the index carries the edge.

**Index-vs-registry authority at add time:** with no explicit version, `cf provider add` does a **live
`tags/list`** (2 requests, ~600 ms, 6.8 KB gzipped — VERIFIED [U]) and takes the newest, falling back to
the index's `latest` when offline. **The index exists for browsing, never for pinning.**

### 3.6 Short-name resolution and disambiguation

**The problem is measured:** of 137 distinct provider repository names across 153 marketplace entries,
**15 (11%) are published by more than one account** (VERIFIED [U]). `provider-kubernetes` and
`provider-helm` ship from **both** `crossplane-contrib` and `upbound`; `provider-minio` from three.
And the signals **disagree**: contrib's `provider-kubernetes` has 6,947,609 downloads vs upbound's
1,590,224, but upbound's is tier `official`, signed, and one patch newer. `provider-databricks`: the
official one is `v0.1.11`/3,692 downloads, the community one `v2.4.2`/2,435. `provider-vultr`: 137 vs
**6** downloads — neither is "the" one. **Any silent tiebreak is wrong for someone who will not notice.**

**Reference grammar, parsed left to right, first match wins:**

| # | Form | Example | Rule |
|---|---|---|---|
| 1 | **Full OCI ref** — contains `:`, or first segment contains `.` | `xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1` | passed through untouched; **needs no index at all** |
| 2 | **Index-qualified** — 3 segments | `corp/upbound/aws-sqs` | `<index>/<publisher>/<name>`, krew's scheme |
| 3 | **Publisher-qualified** — 2 segments | `upbound/aws-sqs` | **the canonical short form**; unambiguous by construction |
| 4 | **Bare short name** — 1 segment | `aws-sqs` | convenience; resolves **only if exactly one match**, else hard error |

**Version suffix uses `@`, not `:`** — `upbound/aws-sqs@v2.7.1` — so the parser can tell at a glance
which grammar it is in.

**Normalisation**, applied identically to the index's `short` field and to user input:
(1) lowercase, `_`→`-`; (2) strip leading `provider-`, `crossplane-provider-`, `provider-upjet-`;
(3) strip trailing `-provider`; (4) collapse repeated `-`. So `provider-aws-sqs`→`aws-sqs`,
`crossplane-provider-castai`→`castai`. The index **publishes** `short` so the client never re-derives
it — a normalisation change is then an index rebuild, not a client upgrade.

**Disambiguation:**

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

Non-negotiable: **no default is applied**, the *disagreeing* signals are shown side by side, and both
fixes are copy-pasteable. Zero matches → suggest by edit distance + substring, exit 1.

**Pins** (`.cf/providers.yaml`, checked into git) are the only legitimate scope for a default — the
team decided, and the decision sits next to the compositions it affects. A global
`~/.config/cf/pins.yaml` ranks below the project file.

```yaml
apiVersion: cf/v1
pins: {kubernetes: crossplane-contrib, helm: crossplane-contrib}
indexes: {default: "github.com/koorikla/cf-index@2026-08-28"}   # optional, for reproducible CI
```

**Ranking is display-only.** The index carries a `rank` (publisher ∈ {upbound, crossplane-contrib} →
tier `official`>`partner`>`community` → signed>unsigned → downloads) used **solely** for search result
ordering and **never** for silent selection. Say so in `--help`, or it quietly becomes a resolution rule.

**Two hard rules:**
1. **`cf` always prints the full ref it resolved to**, in every mode including the GUI. Short names are
   an input convenience, never an output.
2. **Everything written to disk — Composition, lockfile — carries the full ref plus the digest, never
   the short name.** A `.cf` directory checked into git must reproduce byte-identically on a machine
   with a different index, different pins, or no index at all.

---

## 4. What this changes in the spec

Edits to `docs/superpowers/specs/2026-08-27-compositionfactory-design.md`.

### §4 Schema subsystem

1. **Add a "Discovery" subsection** before "Delivery", stating: providers are found through a
   **static index built in CI and embedded in the binary**, never through live enumeration — because
   `xpkg.upbound.io/v2/_catalog` → 401 and `api.github.com/orgs/{o}/packages` → 401 unauthenticated
   (both VERIFIED). Index is ~40 KB gzipped for the 900-package upper bound.
2. **Retract the "empty tag list" premise** wherever it appears. Replace with: `tags/list` returns
   **HTTP 200 / 446 tags** for `upbound/provider-aws-sqs` once the bearer challenge is answered; the
   realm is `https://xpkg.upbound.io/service/token` (**not** `/token`, which 404s) and the token must
   be **repository-scoped** (a scopeless token is issued and then 401s everywhere).
3. **Add to the stack rule:** use `google/go-containerregistry`'s `remote.List` for tags, never a
   hand-rolled `net/http` call — hand-rolled code unmarshals the 401 error body into
   `struct{Tags []string}` and silently gets `nil`. **Treat `len(tags)==0` as an error to investigate,
   never as "no versions".**
4. **Extend item 1 ("Follow `spec.dependsOn` yourself")** with the cheaper path now known:
   `GET https://api.upbound.io/v2/packageMetadata/{acct}/{repo}/{ver|latest}` → 200, 642 B, returns
   **`familyRepoKey`** — one anonymous request names the family package, no `crossplane.yaml` parse.
5. **Add item 4: filter cosign sidecars before anything else.** 344 of 446 tags on
   `provider-aws-sqs` match `^sha256-[0-9a-f]{64}\.(sig|att|sbom)$` (**77%**). Also: tags sort
   lexicographically, so a paginated first page is 100% noise — **omit `?n=` and take the whole list**.
6. **Strengthen item 3 (digest-pin)** to a TOCTOU rule: resolve tag→digest **once**, then use the
   digest for every subsequent request including the signature lookup, and write the digest to the
   lockfile — per Crossplane advisory **GHSA-wfqx-gjrf-g28r**.
7. **Add a second, complementary schema source (candidate, pending a byte-diff):**
   `GET https://api.upbound.io/v1/packages/{acct}/{repo}/{ver}/resources` → 200, ~3 KB CRD index, and
   `.../resources/{group}/{kind}` → 200, ~20 KB **full CRD including `openAPIV3Schema`**, anonymous,
   verified for both `upbound/provider-aws-sqs` and `crossplane-contrib/provider-kubernetes`. Cheaper
   than the xpkg pull for one or two kinds; more expensive for a whole provider. Keep both paths.
8. **Add the cache-layer table from §3.4 above**, plus the warning that
   `~/.crossplane/cache/.../provider-aws-sqs@v2/` is tag-keyed and therefore permanently stale — read
   through it opportunistically, **write digest-keyed**.
9. **Add a registry-aliasing note:** `xpkg.crossplane.io` authenticates against `ghcr.io` and is the
   same repository; always read the realm from `WWW-Authenticate`, never derive it from the hostname.
   And ghcr is a **partial mirror** — 9 tags for `provider-kubernetes` vs 27 versions on the marketplace.

### §11 Interfaces

10. **Replace the CLI line.** From `cf provider add <xpkg-ref>` to:
    `cf provider search|list|versions|info|add|pin` · `cf index update|status|add|remove|list|export` ·
    `cf k8s use <ver>` · `cf gen [--check]` · `cf validate` · `cf adopt` · `cf serve` · `cf mcp`,
    with global `--offline` / `CF_OFFLINE=1` and `CF_NO_AUTO_UPDATE=1`.
11. **State the reference grammar and the two hard rules** from §3.6 — four forms, `@` for version,
    bare names error on ambiguity, `cf` always prints the resolved full ref, and only full ref +
    digest are ever written to disk.
12. **Add a hard architecture constraint under GUI:** `api.upbound.io` returns **no CORS headers**
    (`OPTIONS` → 405; `GET` with `Origin` → 200 but no `Access-Control-Allow-Origin`, VERIFIED). The
    browser cannot call it. `cf serve` must proxy every discovery call — which reinforces the existing
    "HTTP is a thin adapter over `internal/`" rule rather than contradicting it.
13. **Add to GUI:** the palette is **synchronous and local** (900 records fuzzy-matched in
    microseconds — no debounce, no spinner, no loading state); a multi-publisher collision renders as
    **one expandable row, never two lookalike rows**; the staleness badge is always visible and
    clickable, never a modal; offline is a badge, not a blocker.
14. **Add a provenance rendering rule:** the signature verdict (cosign, verifiable in-process via
    `sigstore/sigstore-go` with **no** Rekor round-trip — the bundle is self-contained) and the vendor
    tier claim (`OFFICIAL`) **must not share a badge style**. Upbound signs keyless with identity
    `https://github.com/upbound/upbound-official-build/.github/workflows/supplychain.yml@refs/heads/main`,
    issuer `https://token.actions.githubusercontent.com`; **crossplane-contrib does not sign at all**
    (0 `.sig`/`.att` across all 18 tags of `ghcr.io/crossplane-contrib/provider-aws-s3`). Report that as
    a fact, not a warning. Prefer the **in-band** `meta.crossplane.io/*` values over the index's copies.
15. **Add: never hotlink `iconURL`.** The marketplace icon URL is a signed URL with `Expires=` ~5 min —
    hotlinking gives broken images and leaks per-user browsing to Upbound. Fetch icons at index build
    time and re-host. HTML-escape all third-party descriptions unconditionally.
16. **Add to MCP:** `provider_search` and `provider_versions` read the **local index only, no network**,
    alongside the existing `schema_search` / `kind_describe` / `kind_fields`.

### §12 Milestones

17. **M1** — extend "xpkg ingest + index + lock" to include **consuming** the provider index:
    embedded seed index, `cf provider search`, and the four-form short-name resolver with the
    ambiguity error. Proves: `cf provider add aws-sqs` works on a laptop with the network off.
18. **New milestone between M1 and M2 (or a parallel track): the `cf-index` build job.** Separate repo,
    daily GitHub Action, seven steps per §3.3, with **step 5 (registry validation of every entry)
    non-optional**. Proves: the catalogue is honest — no unresolvable entry ever reaches a picker.
19. **M5** — add to "distribution": `cf index export` air-gap bundle, `cf index add file://`, and
    in-process cosign verification via `sigstore-go`.
20. **§13 Risks** — add two: *(a)* discovery depends on an **undocumented, unversioned, single-consumer**
    API (`api.upbound.io/v2/search`: no OpenAPI at any of `/openapi.json`, `/openapi.yaml`,
    `/v2/openapi.json`, `/swagger.json`, `/docs` — all 404); *(b)* the index is a third-party mirror
    whose freshness is a build-job SLO, not a client guarantee.
21. **§14 Open questions** — add the four in §6 below; §6.1 (does `api.upbound.io/v2/search` exist)
    blocks nothing but changes the build job's step 1.

---

## 5. Risks

1. **Staleness.** The index is a snapshot; `v2.8.0` can ship the morning after a build.
   *Mitigations, all in the design:* age is displayed everywhere and escalates by band; `cf provider add`
   with no explicit version does a **live `tags/list`** (2 requests, ~600 ms) rather than trusting the
   index; a failed refresh never invalidates cache (`stale-if-error` posture); the index revision is
   pinnable in `.cf/providers.yaml` for CI. Precedent for what happens without this: the official
   Crossplane docs community list is **~4 months stale, carries zero install refs, and lists five
   providers that have never shipped a package**; every `awesome-crossplane` is 2–4 years dead.
2. **Rate limits.** GitHub unauthenticated is **60/hr per IP** on a fixed hourly bucket and returns
   **403, not 429**, on exhaustion — one brief burned the entire budget doing this research. Artifact
   Hub 429s after ~53 requests/30 s with an **empty body and no `Retry-After`**. `xpkg.upbound.io`
   exposes **no rate-limit headers at all**, so `cf` cannot be a good citizen reactively.
   *Mitigations:* all file reads via `raw.githubusercontent.com` (budget-free); concurrency cap 4 per
   registry host and a ~10 req/s token bucket; reuse the ~30-min bearer per `(registry, repo, action)`;
   `Accept-Encoding: gzip` (4.5× on `tags/list`); descriptive `User-Agent`
   `cf/<version> (+https://github.com/koorikla/compositionfactory)`; check the status code **before**
   parsing; retry only 429/503 with jittered backoff, never 401/403/404; and above all, **the ~1,800
   aggregate requests live in the daily build job, not in N users' laptops**.
3. **A third party going away.** `api.upbound.io/v2/search` is **undocumented, has no published
   OpenAPI, and has exactly one known consumer** (the marketplace web app) — Upbound can change or gate
   it without notice. `github.com/upbound/up` is now **404** and the CLI appears to have gone
   closed-source, which is a live example of this happening. `marketplace.upbound.io/_next/data/...`
   works but is coupled to a `buildId` that changes every deploy — do not ship it.
   *Mitigations:* the client never talks to the marketplace at all (only the build job does); the seed
   index is embedded in the binary; the build job has a documented SSR fallback; `mirrors[]` lets the
   fetch URL rotate without a client release; and **discovery failure must never fail a `cf gen`**.
   The `cf provider add <full-oci-ref>` path (grammar form 1) works with **no index and no marketplace**,
   forever.
4. **Terms of service.** Upbound's terms (`https://www.upbound.io/terms-conditions`) contain **zero**
   occurrences of `scrap`/`crawl`/`robot`/`automated`/`rate limit`, bind a "Customer" under a
   subscription agreement rather than an anonymous visitor, and `robots.txt` explicitly allows
   `User-agent: *` while blocking only AI-training crawlers — so the position is defensible, but §2.1's
   "reverse engineer … underlying structure" clause is the weak point, since the filter grammar was
   recovered by reading the marketplace's minified JS. §2.3 also incorporates undefined "standard
   published policies" by reference. GitHub's AUP is explicit the other way: *"Scraping does not refer
   to the collection of information through our API."* Artifact Hub has **no ToS at all** (moot).
   *Mitigations:* keep the footprint at one build job per day with an identifying `User-Agent`; open an
   issue on `upbound/upbound` asking whether the search API is fair game — cheap goodwill that converts
   an assumption into a documented answer; carry per-package `license` strings through into any
   redistributed index; and be ready to fall back to the GitHub+registry path, which resolves 49/60
   repos plus 351/351 family members with no Upbound dependency at all.
5. **Honesty risk in the UI.** `upbound/provider-aws-s3` and `crossplane-contrib/provider-aws-s3` are
   built from **the same source repo** (`meta.crossplane.io/source` identical in both, VERIFIED) but
   differ in signing and in `dependsOn` pinning (`v2.4.0` vs `>= 0.0.0`). A UI showing only
   "official vs community" implies a code difference that does not exist. Show `source:` and the
   signature verdict. Likewise, **downloads exist only for Upbound-hosted packages** — a contrib-only
   provider renders blank and reads as "unpopular". Show them for both sources or neither, label the
   source, and never let them feed resolution.

---

## 6. Open questions

1. **Does `https://api.upbound.io/v2/search` actually exist?** (UNRESOLVED-1) One brief has a 200 with
   `count: 626`; another concluded no public API exists after six 404s on different paths. One curl
   settles it. It does not block the design — it decides whether the build job's step 1 is 2 requests
   against a JSON API or a 7-request / 2.06 MB SSR crawl.
2. **What is the true provider count?** (UNRESOLVED-2) 626 vs 348 for the same nominal filter, and
   151 vs 153 with `excludeFamily`. Re-run both on the same day and diff the `repoKey` sets. Affects
   index sizing only within the already-planned 500–900 range.
3. **Is `/v1/packages/{a}/{r}/{v}/resources/{group}/{kind}` byte-identical to the xpkg-extracted CRD,
   or a lossy re-serialisation?** Must be diffed before it is trusted as a schema source (spec edit 7).
   High value if it holds: it is anonymous, ~20 KB, and works for exactly the upjet providers where
   `doc.crds.dev` returns an empty body.
4. **Does `type=resources` on `/v2/search` let us search by CRD kind across all providers?**
   ("Who provides a `Queue`?") The 400 error leaks the enum
   `["packages","resources","charts","vulnerabilities"]`, so the type exists. Untested. This would be a
   genuinely better UX than searching by provider name, and would change what the index needs to carry.
5. **Where does the `cf-index` repo live, and who owns the build job's failure?** A daily Action in
   `koorikla/cf-index` is assumed above. If the index goes unmaintained, `cf` degrades to the embedded
   seed plus form-1 full refs — acceptable, but it should be a stated SLO, not an emergent property.
6. **Does `sigstore-go` verify these specific Upbound bundles end to end in Go?** One brief decoded the
   certificate and confirmed the `dev.sigstore.cosign/bundle` layer annotation is present and
   self-contained (so no Rekor round-trip), but did not run a verification. Blocks spec edit 19.
7. **What are the long-run rate limits on `xpkg.upbound.io`?** Only 12- and 60-request bursts were
   tested. A 2,000-request index build is a different question — run it once, observe, and throttle
   accordingly before scheduling it daily.
8. **`managedResourceDefinitions` appears in the `/resources` index alongside `customResourceDefinitions`.**
   A Crossplane v2 MRD concept that interacts with spec §5's `.m.` fork and with §14's existing
   "does `cf` ever read a cluster?" question. Worth understanding before M2.
