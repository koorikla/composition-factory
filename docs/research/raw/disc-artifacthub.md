# Provider discovery: Artifact Hub as a Crossplane package index

**Research date:** 2026-08-27/28 · **Status:** complete · **Verdict: NO. Artifact Hub cannot serve as a Crossplane provider index.**

All findings below marked **VERIFIED** were produced by running the request from this machine (macOS, `curl` / `python3 urllib`, no auth, no proxy) on 2026-08-27/28. **DOCS** means read from the Artifact Hub OpenAPI spec or repo docs without an accompanying live call.

---

## Decisions this enables

1. **Do not build against the Artifact Hub API for provider discovery — close this avenue.** Artifact Hub supports exactly 29 package kinds (`0`–`28`) and **none of them is Crossplane**. This is not a coverage gap that could be worked around with a clever query: there is no Crossplane repository kind to register a repository under, so no `xpkg` can ever appear. VERIFIED against the live kind facet, the OpenAPI enum, and `docs/repositories.md`.
2. **Cross-check result: 0 of 9 target providers found.** `provider-aws-sqs`, `provider-gcp-storage`, `provider-kubernetes`, `provider-helm`, `provider-terraform`, `provider-argocd`, `provider-github`, `provider-sql`, `provider-vault` — all absent. Neither `crossplane-contrib` nor `upbound` exists as a publisher; `upbound-stable` is registered only as a **Helm chart** repo (`https://charts.upbound.io/stable`). Zero repositories anywhere on the hub point at `xpkg.upbound.io`, `xpkg.crossplane.io`, or an `xpkg` OCI path.
3. **Don't wait for it either.** Zero open feature requests for Crossplane support in `artifacthub/hub` (GitHub issue search: 2 hits, neither relevant), no `crossplane_*_repositories.md` doc, no mention in the roadmap docs. There is no signal this is coming.
4. **Steal the data model, not the data.** Artifact Hub's package schema is a near-perfect match for the browse-and-pick UI we want, and it proves the shape works: OCI-hosted packages already carry a fully-formed ref in `content_url` (e.g. `oci://ghcr.io/quenchworks/charts/crossplane:0.0.2`), plus `available_versions[]`, `display_name`, `description`, `license`, `logo_image_id`, `repository.{name,url,organization_name,verified_publisher,official}`, `maintainers[]`, `links[]`, `keywords[]`. Model `cf`'s own provider index on these field names (§3) so a future Artifact Hub integration would be a straight mapping if it ever lands.
5. **Adopt the bulk-dump pattern for whatever index we do build.** Artifact Hub's own maintainers push high-volume consumers off per-search queries and onto single-request dumps: `/api/v1/harbor-replication` returns **476,248 entries, 11.9 MB gzipped, in 3.2 s** (VERIFIED). That is the right ergonomics for `cf` — ship/refresh one static index file rather than fan out per-search — and it is the pattern to imitate regardless of source. It is also **Helm-only**, so it is not usable here.

---

## 1. The kind taxonomy — there is no Crossplane kind

### 1a. Live kind facet (VERIFIED)

```
GET https://artifacthub.io/api/v1/packages/search?facets=true&limit=1
→ 200, no auth
```

Response header `Pagination-Total-Count: 21142` (total packages on the hub). The `kind` facet, in full:

| id | name | total | | id | name | total |
|---|---|---|---|---|---|---|
| 0 | Helm charts | 17784 | | 15 | Kyverno policies | 564 |
| 1 | Falco rules | 23 | | 16 | Knative client plugins | 5 |
| 2 | OPA policies | 3 | | 18 | Argo templates | 11 |
| 3 | OLM operators | 454 | | 19 | KubeArmor policies | 24 |
| 4 | Tinkerbell actions | 11 | | 20 | KCL modules | 348 |
| 5 | Krew kubectl plugins | 410 | | 21 | Headlamp plugins | 107 |
| 6 | Helm plugins | 56 | | 22 | Inspektor gadgets | 52 |
| 7 | Tekton tasks | 411 | | 23 | Tekton stepactions | 20 |
| 8 | KEDA scalers | 5 | | 24 | Meshery designs | 330 |
| 10 | Keptn integrations | 41 | | 27 | Bootable containers | 1 |
| 11 | Tekton pipelines | 9 | | 28 | Kagent agents | 1 |
| 12 | Containers images | 298 | | | | |
| 13 | Kubewarden policies | 125 | | | | |
| 14 | Gatekeeper policies | 49 | | | | |

Kinds `9` (CoreDNS plugins), `17` (Backstage plugins), `25` (OpenCost plugins), `26` (Radius recipes) are defined but currently hold **0** packages, so they are omitted from the facet. I also brute-forced `kind=0..30` individually and read `Pagination-Total-Count` off each; the per-kind totals sum to exactly **21142**, matching the unfiltered total — so the enumeration above is **complete and exhaustive**. Kinds ≥ 29 return 0.

**No Crossplane kind exists.**

### 1b. Authoritative enum (DOCS — `docs/api/openapi.yaml`, lines 5243–5272)

Fetched from `https://raw.githubusercontent.com/artifacthub/hub/master/docs/api/openapi.yaml` (191,525 bytes, HTTP 200):

```
Repository kind:
  * `0`  - Helm charts            * `15` - Kyverno policies
  * `1`  - Falco rules            * `16` - Knative client plugins
  * `2`  - OPA policies           * `17` - Backstage plugins
  * `3`  - OLM operators          * `18` - Argo templates
  * `4`  - Tinkerbell actions     * `19` - KubeArmor templates
  * `5`  - Krew kubectl plugins   * `20` - KCL packages
  * `6`  - Helm plugins           * `21` - Headlamp plugins
  * `7`  - Tekton tasks           * `22` - Inspektor gadgets
  * `8`  - KEDA scalers           * `23` - Tekton stepactions
  * `9`  - Core DNS plugins       * `24` - Meshery designs
  * `10` - Keptn integrations     * `25` - Opencost plugins
  * `11` - Tekton pipelines       * `26` - Radius recipes
  * `12` - Container images       * `27` - Bootable containers
  * `13` - Kubewarden policies    * `28` - Kagent agents
  * `14` - Gatekeeper policies
```

`grep -i crossplane openapi.yaml` → 3 hits, all inside an unrelated `/harbor-replication` response *example* that happens to use the Crossplane **Helm chart** as sample data. Nothing structural.

The string-form kind param (`RepositoryKindParam`) enum is likewise complete and Crossplane-free: `helm, opa, falco, olm, tbaction, krew, helm-plugin, tekton-task, keda-scaler, coredns, keptn, tekton-pipeline, container, kubewarden, gatekeeper, kyverno, knative-client-plugin, backstage, argo-template, kubearmor, kcl, headlamp, inspektor-gadget, tekton-stepaction, meshery, opencost, radius, bootc, kagent`.

### 1c. Repository docs (VERIFIED — `docs/repositories.md`, 10,176 bytes)

The doc lists a per-kind onboarding guide for all 28 supported repository kinds. `grep -i crossplane` → **zero hits**. Compare: every supported kind has a `<kind>_repositories.md` file. There is no `crossplane_packages_repositories.md`.

### 1d. Is it coming? (VERIFIED)

```
GET https://api.github.com/search/issues?q=repo:artifacthub/hub+crossplane
→ 200, total_count: 2
  closed 2025-12-06 #4641 [OFFICIAL] Crossview
  closed 2022-01-18 #1791 [feature] Improve filtering to include CNCF project ...
```

Neither is a request for Crossplane package support. `in:title` search → `total_count: 0`. **No roadmap item, no feature request, no plan.**

---

## 2. Coverage cross-check — 0/9 providers present

### 2a. Exact-name search (VERIFIED)

Every query below: `GET /api/v1/packages/search?ts_query_web=<name>&limit=10` → HTTP **200**, no auth.

| Query | `Pagination-Total-Count` | Present as an xpkg? |
|---|---|---|
| `provider-aws-sqs` | **0** | ❌ absent |
| `provider-gcp-storage` | **0** | ❌ absent |
| `provider-kubernetes` | **0** | ❌ absent |
| `provider-helm` | **0** | ❌ absent |
| `provider-terraform` | **0** | ❌ absent |
| `provider-argocd` | **0** | ❌ absent |
| `provider-github` | **0** | ❌ absent |
| `provider-sql` | **0** | ❌ absent |
| `provider-vault` | **0** | ❌ absent |
| `provider-aws` | **0** | ❌ absent |
| `provider-azure` | **0** | ❌ absent |
| `crossplane-contrib` | **0** | ❌ no such publisher |
| `xpkg` | **0** | ❌ nothing |
| `upbound` | **3** | ❌ 2× Helm chart (`upbound-stable`), 1× OLM operator — all `universal-crossplane`/`crossplane`, none an xpkg |

`ts_query_web` uses Postgres `websearch_to_tsquery`, which treats a hyphenated token as a phrase, so I re-ran every provider as a de-hyphenated multi-word query to rule out a tokenisation artifact. Same conclusion — the only hits are KCL modules (§2c) and unrelated Helm charts (`kubedb-provider-aws`, `kubeform-provider-gcp`, `node-provider-labeler`).

### 2b. Repository-level search (VERIFIED)

```
GET /api/v1/packages/search?ts_query_web=crossplane&limit=20   → 200, total 56
GET /api/v1/repositories/search?name=crossplane&limit=20        → 200, total  7
GET /api/v1/repositories/search?name=upbound&limit=20           → 200, total  1
GET /api/v1/repositories/search?name=xpkg&limit=20              → 200, total  0
GET /api/v1/repositories/search?kind=12&limit=60                → 200, total 385  (all container-image repos)
```

Every one of the 7 `crossplane`-named repositories, with its kind and URL:

```
kind=0  (Helm)     crossplane                     -> https://charts.crossplane.io/master/
kind=0  (Helm)     stable-crossplane              -> https://charts.crossplane.io/stable
kind=0  (Helm)     crossplane-iam-pod-role        -> https://explorium-ai.github.io/crossplane-iam-pod-role/
kind=0  (Helm)     quench-crossplane              -> oci://ghcr.io/quenchworks/charts/crossplane
kind=21 (Headlamp) crossplane-explorer-headlamp   -> https://github.com/vinish86/crossplane-explorer-headlamp.git
kind=21 (Headlamp) crossplane-headlamp-plugin     -> https://github.com/openmcp-project/crossplane-headlamp-plugin/artifacthub
kind=21 (Headlamp) headlamp-plugin-crossplane     -> https://github.com/builver/headlamp-plugin-crossplane
```

The single `upbound` repository: `kind=0 upbound-stable -> https://charts.upbound.io/stable` — the Helm chart repo, not the xpkg registry.

I also scanned all 385 **Container images** (`kind=12`) repositories, since that kind accepts an `oci://` URL and is the only structural analogue to an xpkg registry. **Zero** contain `xpkg`, `crossplane`, or `upbound` in their URL. Sample of what that kind actually holds: `oci://quay.io/kevchu3/act-ubi-job-container`, `oci://index.docker.io/artifacthub/ah`, `oci://ghcr.io/astrovm/amyos`. Nobody has attempted to shoehorn xpkgs in this way — and it would not help if they had, because the container-image kind carries no CRD/schema metadata.

### 2c. The one genuine near-miss: KCL modules (VERIFIED — and a dead end)

```
GET /api/v1/packages/search?kind=20&repo=kcl-module&ts_query_web=crossplane&limit=60
→ 200, Pagination-Total-Count: 16
```

```
crossplane                        2.0.2      crossplane-provider-sql           0.7.2
crossplane-http                   1.0.8      crossplane-provider-terraform     0.10.2
crossplane-provider-aws           0.36.4     crossplane-provider-vault         1.0.2
crossplane-provider-azure         0.20.2     crossplane_provider_keycloak      2.16.0
crossplane-provider-gcp           0.22.2     crossplane-provider-upjet-aws     1.23.0
crossplane-provider-helm          0.13.2     crossplane-provider-upjet-gcp     1.0.5
crossplane-provider-http          1.0.8      crossplane-provider-upjet-github  0.18.4
crossplane-provider-kubernetes    0.18.0     crossplane-xnetwork-kcl-function  0.0.2
```

These are **KCL language modules from `github.com/kcl-lang/modules`** that model Crossplane provider CRDs as KCL schemas. They are not xpkgs and carry no OCI xpkg reference — their `install` field is `kcl mod add crossplane-provider-aws:0.36.4`. They are also **badly stale**: `crossplane-provider-upjet-aws` sits at `1.23.0` and `crossplane-provider-kubernetes` at `0.18.0`, while the real providers are on v1.x–v2.x families. Latest version timestamps run 2023–2024. Not a usable index, and the granularity is wrong (no per-service family packages like `provider-aws-sqs`).

---

## 3. Field coverage — the schema is right, the content is missing

Everything a browse-and-pick UI needs *does* exist in the Artifact Hub package model. This is worth recording because it is the reference data model to imitate.

### 3a. Search-result summary object (VERIFIED)

`GET /api/v1/packages/search?ts_query_web=crossplane&limit=20` → 200. One real element, untrimmed:

```json
{
  "package_id": "f09eed90-405a-4fb0-98b4-4b3c1fdf6f82",
  "name": "crossplane",
  "normalized_name": "crossplane",
  "category": 2,
  "logo_image_id": "10efdd8e-9c51-4da9-b188-0dc80d05e7d4",
  "stars": 49,
  "description": "Crossplane is an open source Kubernetes add-on that enables platform teams to assemble infrastructure from multiple vendors, and expose higher level self-service APIs for application teams to consume.",
  "version": "2.5.0-rc.0.24.g0e4f8c1d7",
  "app_version": "2.5.0-rc.0.24.g0e4f8c1d7",
  "license": "Apache-2.0",
  "deprecated": false,
  "has_values_schema": false,
  "signed": false,
  "security_report_summary": { "low": 0, "high": 0, "medium": 0, "unknown": 1, "critical": 0 },
  "production_organizations_count": 1,
  "ts": 1787661869,
  "repository": {
    "url": "https://charts.crossplane.io/master/",
    "kind": 0,
    "name": "crossplane",
    "official": false,
    "repository_id": "1cc43cf4-8f93-4299-8902-350d0443de26",
    "scanner_disabled": false,
    "organization_name": "helm",
    "verified_publisher": false,
    "organization_display_name": "Helm"
  }
}
```

Note: `kind` is **not** a top-level field on the summary — it lives at `repository.kind`. Easy thing to get wrong.

### 3b. Package detail object (VERIFIED)

`GET /api/v1/packages/{kindName}/{repoName}/{packageName}` — e.g. `/api/v1/packages/helm/crossplane/crossplane` → 200, **412,325 bytes**.

Top-level keys returned:
```
all_containers_images_whitelisted, app_version, available_versions, category,
containers_images, contains_security_updates, content_url, data, deprecated,
description, digest, has_changelog, has_values_schema, home_url, is_operator,
keywords, license, logo_image_id, maintainers, name, normalized_name, package_id,
prerelease, production_organizations_count, readme, repository,
security_report_created_at, security_report_summary, signed, stats, ts, version
```

Trimmed real body (readme / 2,713-element version list elided):

```json
{
  "package_id": "f09eed90-405a-4fb0-98b4-4b3c1fdf6f82",
  "name": "crossplane",
  "description": "Crossplane is an open source Kubernetes add-on ...",
  "logo_image_id": "10efdd8e-9c51-4da9-b188-0dc80d05e7d4",
  "keywords": ["cloud","infrastructure","services","application","database","..."],
  "home_url": "https://crossplane.io",
  "version": "2.5.0-rc.0.24.g0e4f8c1d7",
  "app_version": "2.5.0-rc.0.24.g0e4f8c1d7",
  "digest": "ae46926c6f3b6595c3287c8b853356f547fca36ee2cad07963e9fd5ded2c9cfd",
  "license": "Apache-2.0",
  "signed": false,
  "content_url": "https://charts.crossplane.io/master/crossplane-2.5.0-rc.0.24.g0e4f8c1d7.tgz",
  "maintainers": [{ "name": "Crossplane Maintainers", "email": "crossplane-info@lists.cncf.io" }],
  "repository": {
    "repository_id": "1cc43cf4-8f93-4299-8902-350d0443de26",
    "name": "crossplane",
    "url": "https://charts.crossplane.io/master/",
    "kind": 0,
    "verified_publisher": false,
    "official": false,
    "organization_name": "helm",
    "organization_display_name": "Helm"
  },
  "stats": { "subscriptions": 17, "webhooks": 1 },
  "available_versions": [
    { "version": "2.5.0-rc.0.24.g0e4f8c1d7", "app_version": "2.5.0-rc.0.24.g0e4f8c1d7",
      "contains_security_updates": false, "prerelease": false, "ts": 1787661869 },
    { "version": "2.5.0-rc.0.20.gc9f6124af", "app_version": "2.5.0-rc.0.20.gc9f6124af",
      "contains_security_updates": false, "prerelease": false, "ts": 1787641622 }
    /* ... 2711 more ... */
  ]
}
```

**`available_versions` is complete and inline** — 2,713 entries for this package, which is what makes the response 412 KB. Design note for `cf`: if we build a version list, paginate or lazily fetch it; do not inline every tag.

### 3c. The OCI install reference (VERIFIED — the important one)

For an OCI-hosted package, `content_url` is a **fully-formed, directly usable OCI reference**:

```
GET /api/v1/packages/helm/quench-crossplane/crossplane → 200
  repository.url : "oci://ghcr.io/quenchworks/charts/crossplane"
  content_url    : "oci://ghcr.io/quenchworks/charts/crossplane:0.0.2"
  install        : null
```

This is exactly the field a `cf provider add` flow would consume. For non-OCI packages, `install` is instead a **Markdown blob**, not structured data:

```json
"install": "#### Add `crossplane-provider-aws` with tag `0.36.4` as dependency\n```\nkcl mod add crossplane-provider-aws:0.36.4\n```\n..."
```

So: use `content_url` (structured), never `install` (prose). If Crossplane support ever landed, `content_url` would be where `xpkg.upbound.io/upbound/provider-aws-sqs:v2` appears.

### 3d. Lightweight summary endpoint (VERIFIED)

`GET /api/v1/packages/helm/crossplane/crossplane/summary` → 200, **987 bytes** (vs 412 KB for the full detail). Returns the `PackageSummary` schema — ideal for a list view.

### 3e. Logos (VERIFIED)

`logo_image_id` resolves to `https://artifacthub.io/image/{logo_image_id}`:

```
GET https://artifacthub.io/image/10efdd8e-9c51-4da9-b188-0dc80d05e7d4     → 200 image/png 3453 B
GET https://artifacthub.io/image/10efdd8e-9c51-4da9-b188-0dc80d05e7d4@2x  → 200 image/png 6575 B
```
`@2x` and `@3x` density variants work. No auth.

### 3f. UI-field checklist

| UI need | Artifact Hub field | Present? |
|---|---|---|
| Display name | `display_name` (detail), `name` | ✅ |
| Description | `description` | ✅ |
| Publisher / org | `repository.organization_name` / `organization_display_name` / `user_alias` | ✅ |
| Trust signal | `repository.verified_publisher`, `repository.official`, `signed`, `stars` | ✅ |
| Latest version | `version` (+ `app_version`) | ✅ |
| All versions | `available_versions[]` (complete, inline) | ✅ |
| Repository URL | `repository.url` | ✅ |
| Licence | `license` (SPDX string) | ✅ |
| OCI ref / install | `content_url` (structured for OCI) · `install` (Markdown prose otherwise) | ✅ / ⚠️ |
| Logo | `logo_image_id` → `/image/{id}[@2x\|@3x]` | ✅ |
| Homepage | `home_url`, `links[]` | ✅ |
| Search facets | `?facets=true` → org / kind / repo / category / license | ✅ |
| Sort | `SortParam` (`relevance`, `stars`) | ✅ |
| **Crossplane packages** | — | ❌ **none indexed** |

### 3g. Search parameters (DOCS — openapi.yaml lines 931–955)

`/packages/search` accepts: `offset`, `limit`, `facets`, `ts_query_web`, `kind` (repeatable), `category`, `user`, `org`, `repo`, `license`, `capabilities`, `deprecated`, `operators`, `verified_publisher`, `official`, `cncf`, `sort`.

---

## 4. Rate limits — measured

### 4a. Documented position (DOCS — quoted verbatim)

From `docs/faq.md`, § "What are the API rate limits?":

> "The exact numbers are not documented because they are updated every now and then and vary depending on the endpoint used and the current service status. There are some integration endpoints that allow dumping a lot of content in a single request, which may be handy in some cases."

From `https://artifacthub.io/docs/topics/infrastructure/`:

> "Both CloudFront and the load balancer have associated a set of web ACLs rules to rate limit and block certain traffic patterns."

Historical context (`FairwindsOps/nova` issue #214): an Artifact Hub maintainer reported receiving "~120K requests per hour from Nova installations (and growing fast!)" and asked the tool to migrate to a bulk-dump endpoint. Their consistent guidance to tool authors is **dump, don't poll**.

### 4b. Observed limit (VERIFIED — measured twice)

Test: sequential single-threaded `GET /api/v1/packages/search?limit=1&offset=<i>` with a unique `offset` each time so CloudFront cannot serve from cache.

```
25 requests OK, elapsed 15.0 s   (x-cache: Miss from cloudfront)
50 requests OK, elapsed 29.0 s   (x-cache: Miss from cloudfront)
FIRST 429 after 53 requests in 30.1 s  →  ~1.8 req/s sustained
```

**Observed budget: ≈50 origin-bound requests per 30 s.** A separate earlier run (~58 mixed requests in ~90 s) tripped the same limit, so this is reproducible.

**Recovery: 46 s and 56 s** in two independent measurements (polled at 5 s intervals until the first 200).

**A throttled client at 1.5 s between requests (0.67 req/s) never tripped it** across 100+ requests over the whole session.

### 4c. The 429 response (VERIFIED)

```
HTTP/2 429
server: CloudFront
date: Thu, 27 Aug 2026 21:37:39 GMT
content-length: 0
x-cache: Error from cloudfront
via: 1.1 d056c091eefb07376350368f992d1b38.cloudfront.net (CloudFront)
x-amz-cf-pop: HEL51-P4
```

**Empty body. No `Retry-After`. No `X-RateLimit-*` headers.** A client must back off blind. This bit me mid-session: a naive `json.load()` on the empty body throws a confusing `JSONDecodeError` rather than surfacing the 429. Any client we write must check the status code before parsing.

### 4d. CDN cache hits do NOT count against the limit (VERIFIED — key finding)

Successful responses carry `cache-control: max-age=300`. I fired **80 identical requests in 6.4 s (≈12.5 req/s)** immediately after a cooldown:

```
CACHED-BURST done: 80 cache-hits, 0 misses, 0 429s, 6.4 s
```

All 80 returned `x-cache: Hit from cloudfront`; **zero 429s**. The limiter is enforced on origin-bound traffic only. Practical consequence: repeated identical queries are cheap; it's *distinct* queries (the pagination crawl, per-package details) that burn budget.

### 4e. Auth and API keys

- **No auth is required** for any read endpoint used here — search, detail, summary, repositories, facets, images, and all three bulk dumps returned 200 anonymously. VERIFIED.
- An API-key concept exists (DOCS, openapi.yaml line 3923): `X-API-KEY-ID` + `X-API-KEY-SECRET` headers. These authorise **user/organisation write operations** (managing your own repositories, subscriptions, webhooks). There is **no documented higher rate-limit tier**, and the FAQ's answer to "how do I avoid rate limits" is the bulk endpoint, not a key. Do not expect a key to help.

### 4f. Pagination (VERIFIED)

```
GET /packages/search?limit=61                    → 400  {"message":"invalid input: invalid limit (0 < l <= 60)"}
GET /packages/search?limit=60&offset=0           → 200  60 packages, total 21142
GET /packages/search?kind=20&limit=60&offset=300 → 200  48 packages, total 348
GET /packages/search?kind=0&limit=20&offset=17800→ 200   0 packages (past the end, no error)
GET /packages/search?limit=1&offset=999999       → 200   0 packages (no deep-pagination cap)
```

Max `limit` is **60** (DOCS confirms `maximum: 60`). Total count comes from the `Pagination-Total-Count` response header, not the body. No deep-offset cap. A full crawl of the hub would be 353 requests at `limit=60` — which at the safe 1.5 s cadence is ~9 minutes and would very likely be seen as abusive. Hence §6.

---

## 5. Licence and terms of use

- **Software licence: Apache-2.0.** `https://raw.githubusercontent.com/artifacthub/hub/master/LICENSE` → Apache License 2.0. VERIFIED.
- **Governance:** "Artifact Hub is a [CNCF Incubating Project](https://www.cncf.io/projects/)." — `artifacthub/hub` README, line 55. VERIFIED.
- **There is no Terms of Service.** I grepped the site's JS bundle (`https://artifacthub.io/static/js/main.s06IYoki.js`) for every legal/footer link. The complete set:
  ```
  https://linuxfoundation.org/
  https://linuxfoundation.org/trademark-usage
  https://www.linuxfoundation.org/legal/privacy-policy
  https://www.cncf.io/projects/
  https://twitter.com/cncfartifacthub
  ```
  No terms-of-service, no acceptable-use policy, no API terms. VERIFIED.
- **`robots.txt` does not exist.** `GET https://artifacthub.io/robots.txt` → **200**, but the body is the SPA HTML shell (the React catch-all route), not a robots file. There are therefore **no crawl directives** — neither permission nor prohibition. VERIFIED.
- **Caching results locally:** nothing forbids it, and the design actively assumes it — `cache-control: max-age=300` on API responses, plus three purpose-built bulk-dump endpoints whose entire reason for existing is that downstream tools mirror the data (Harbor's replication adapter and FairwindsOps/nova both do exactly this). The FAQ *recommends* the dump endpoint as the way to "get all charts listed on artifacthub.io without hitting rate limits".
- **Caveat for any redistribution:** the *indexed content* is third-party and carries its own per-package `license` field (`"license": "Apache-2.0"` etc., often `null`). Artifact Hub's Apache-2.0 licence covers its code, not the packages it lists. If `cf` ever shipped a mirrored index, it would need to carry those per-package licence strings through — which the API does provide.
- **Practical read:** querying it politely from an OSS tool is unobjectionable and clearly anticipated. Crawling it wholesale via `/packages/search` is not — use a dump endpoint if one existed for your kind. It doesn't for ours, and the whole question is moot given §1.

---

## 6. Bulk / dump endpoints — they exist, and they are Helm-only

Three "Integrations" endpoints (DOCS: openapi.yaml lines 3659, 3739, 3800). All three tested anonymously:

| Endpoint | Status | Size (gzip / raw) | Entries | Time | Scope |
|---|---|---|---|---|---|
| `/api/v1/harbor-replication` | **200** | 11.9 MB / 83.5 MB | **476,248** | 3.16 s | Helm charts, **every version** |
| `/api/v1/helm-exporter` | **200** | 386 KB / 2.6 MB | **18,188** | 0.22 s | Helm charts, latest version only |
| `/api/v1/nova` | 200 (DOCS shape) | — | — | — | Helm charts, latest + metadata |

All VERIFIED for the first two.

`/helm-exporter` — real trimmed body (spec description: *"Get the latest version available of all charts listed in Artifact Hub"*):

```json
[
  { "name": "cp4d-deployer", "version": "1.0.0",
    "repository": { "name": "cloud-native-toolkit", "url": "https://charts.cloudnativetoolkit.dev" } },
  { "name": "operator", "version": "7.1.1",
    "repository": { "name": "minio-operator", "url": "https://operator.min.io" } }
]
```

`/harbor-replication` — real trimmed body:

```json
{ "repository": "witcom-gmbh",
  "package": "command-source-connector",
  "version": "0.4.0",
  "url": "https://witcom-gmbh.github.io/witcom-charts/command-source-connector-0.4.0.tgz" }
```

**There is no generic, kind-parameterised dump.** Every one of these is hardcoded to Helm charts (`kind=0`) — they were built for specific downstream consumers (Harbor's replication adapter, Fairwinds' `helm-exporter` and `nova`). There is no `?kind=` parameter and no equivalent for KCL, Krew, container images, or anything else. So even in a hypothetical world where Artifact Hub indexed Crossplane packages, the only bulk path would be a 353-request paginated crawl unless the maintainers added a dedicated endpoint.

**The pattern is still the lesson:** one request → 386 KB gzipped → the entire latest-version index of 18,188 packages, in 0.22 s. That is the ergonomic target for whatever provider index `cf` ends up shipping or refreshing.

---

## 7. Every endpoint tested

All against `https://artifacthub.io`, anonymous, no auth header, from macOS via `curl` / `python3 urllib`.

| # | URL | Status | Auth | Notes |
|---|---|---|---|---|
| 1 | `/api/v1/packages/search?limit=1` | 200 | none | `Pagination-Total-Count: 21142`, `cache-control: max-age=300` |
| 2 | `/api/v1/packages/search?limit=1&kind=N` for N=0..30 | 200 | none | per-kind totals; sum == 21142; N≥29 → 0 |
| 3 | `/api/v1/packages/search?facets=true&limit=1` | 200 | none | authoritative live kind id→name map (§1a) |
| 4 | `/api/v1/packages/search?ts_query_web=crossplane&limit=20` | 200 | none | total **56**, zero xpkgs |
| 5 | `/api/v1/packages/search?ts_query_web=provider-*` ×11 | 200 | none | **all total 0** (§2a) |
| 6 | `/api/v1/packages/search?kind=20&repo=kcl-module&ts_query_web=crossplane&limit=60` | 200 | none | total 16 KCL modules (§2c) |
| 7 | `/api/v1/packages/search?limit=61` | **400** | none | `{"message":"invalid input: invalid limit (0 < l <= 60)"}` |
| 8 | `/api/v1/packages/search?limit=1&offset=999999` | 200 | none | 0 results, no deep-pagination cap |
| 9 | `/api/v1/repositories/search?limit=3` | 200 | none | total **6605** repositories hub-wide |
| 10 | `/api/v1/repositories/search?name={crossplane,upbound,xpkg}` | 200 | none | 7 / 1 / **0** — none is an xpkg registry |
| 11 | `/api/v1/repositories/search?kind=12&limit=60` | 200 | none | 385 container-image repos, zero xpkg |
| 12 | `/api/v1/packages/kcl/kcl-module/crossplane-provider-aws` | 200 | none | detail shape; `install` is Markdown prose |
| 13 | `/api/v1/packages/helm/crossplane/crossplane` | 200 | none | **412,325 B**; 2,713 `available_versions` |
| 14 | `/api/v1/packages/helm/quench-crossplane/crossplane` | 200 | none | `content_url: oci://ghcr.io/...:0.0.2` ✅ |
| 15 | `/api/v1/packages/helm/crossplane/crossplane/summary` | 200 | none | **987 B** — lightweight list-view variant |
| 16 | `/api/v1/helm-exporter` | 200 | none | 18,188 entries, 386 KB gz, 0.22 s |
| 17 | `/api/v1/harbor-replication` | 200 | none | 476,248 entries, 11.9 MB gz, 3.16 s |
| 18 | `/image/10efdd8e-…` and `…@2x` | 200 | none | `image/png`, 3,453 B / 6,575 B |
| 19 | `/robots.txt` | 200 | none | **returns SPA HTML** — no robots file exists |
| 20 | `/api/v1/docs` | 200 | none | Swagger UI HTML shell, not the spec |
| 21 | any endpoint at >~1.8 req/s | **429** | none | empty body, no `Retry-After`, recovers in 46–56 s |
| 22 | `raw.githubusercontent.com/.../docs/api/openapi.yaml` | 200 | none | 191,525 B — the real spec |
| 23 | `raw.githubusercontent.com/.../docs/repositories.md` | 200 | none | 10,176 B — zero `crossplane` hits |
| 24 | `raw.githubusercontent.com/.../docs/faq.md` | 200 | none | 19,517 B — the rate-limit quote |
| 25 | `api.github.com/search/issues?q=repo:artifacthub/hub+crossplane` | 200 | none | `total_count: 2`, neither relevant |

Endpoints that do **not** exist, tried and failed: `raw.githubusercontent.com/.../api/openapi.yaml` (404), `.../internal/hub/repository.go` (404), `.../internal/hub/hub.go` (404) — the spec lives at `docs/api/openapi.yaml`.

---

## What this means for compositionfactory

**Artifact Hub is out.** It is not a partial fit or a fallback — the data simply is not there, and no query, kind filter, or registration workaround changes that. Provider discovery must come from elsewhere: the GitHub org listings (`crossplane-contrib`, `upbound`), the Upbound Marketplace API, or a curated index we maintain.

Three things worth carrying forward:

1. **Model our index on Artifact Hub's package schema** (§3f). It is a mature, CNCF-incubated design for exactly this problem, the field names are good, and `content_url` proves the OCI-ref-as-first-class-field shape works. If Crossplane support ever landed, our mapping would be trivial.

2. **Build around a bulk snapshot, not per-query fan-out** (§6). One gzipped index file, refreshed periodically, matches both the offline-first constraint and what Artifact Hub's maintainers learned the hard way from Nova. Whatever source we pick, prefer one large fetch over N small ones.

3. **Whatever HTTP source we do use, assume opaque rate limiting.** The 429 here has an empty body, no `Retry-After`, and no rate-limit headers (§4c). Our client needs status-code checking before parsing, exponential backoff on 429, and — per the standing constraint — a clean fall-through to the local cache when discovery is unreachable. Discovery stays a convenience; `cf provider add <exact-ref>` must keep working with the network off.

**Cross-references for the other discovery briefs:** the `xpkg.upbound.io` empty-tag-list negative result and the `doc.crds.dev` empty-body result are established elsewhere and unaffected by anything here. The one novel lead this brief surfaced — `github.com/kcl-lang/modules`, which contains KCL schemas generated from Crossplane provider CRDs for ~14 providers — is stale (2023–2024) and lacks per-service family granularity, so it is not a viable schema source, but it is evidence that CRD-derived schema generation for Crossplane providers has been done before and is worth a glance if the schema-extraction path ever needs a reference implementation.
