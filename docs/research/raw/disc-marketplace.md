# Provider Discovery — Upbound Marketplace & xpkg registry APIs

Research area: does Upbound expose a usable programmatic API for browsing/searching providers and versions?
All probes run 2026-08-27/28 from a residential IP, anonymous (no cookies, no tokens) unless stated.
Marked **VERIFIED** = I ran it and pasted real output. **DOCS** = read, not executed.

---

## Decisions this enables

1. **Build discovery on `https://api.upbound.io/v2/search` (undocumented but anonymous, unauthenticated, CORS-less, unthrottled).** It returns all 626 providers in 7 paged requests / 5.8 s, with a real filter grammar and full-text search. This is the browse-and-search backend. Do *not* scrape HTML and do *not* use `_next/data` (buildId-coupled, breaks on every marketplace deploy).
2. **Reverse the "tags/list is empty" negative result — it was an unauthenticated probe.** `xpkg.upbound.io/v2/<repo>/tags/list` returns **200 with 446 tags** once you present the anonymous bearer token from `https://xpkg.upbound.io/service/token`. It is fully OCI-conformant including `n=`/`last=` pagination. But 77% of the tags are cosign `.sig`/`.att`/`.sbom` noise and there is no "latest"/EOL signal — so prefer the marketplace API for versions and keep tags/list as the **offline-capable, vendor-neutral fallback**.
3. **`api.upbound.io/v1/packages/{acct}/{repo}/{ver}/resources/{group}/{kind}` returns the complete CRD including `openAPIV3Schema`, anonymously, in one ~20 KB request.** This is a viable *schema* source that works for exactly the upjet/Upbound providers where doc.crds.dev returns an empty body, and it works for `crossplane-contrib` too. It is a strong candidate to sit alongside the xpkg pull, not just a discovery aid.
4. **The local web UI cannot call `api.upbound.io` from the browser — there are no CORS headers at all** (OPTIONS → 405, GET with `Origin` → no `Access-Control-Allow-Origin`). The Go binary must proxy every discovery call. That is a hard architecture constraint on the web UI, and it conveniently gives you the caching layer for the offline requirement.
5. **Licence risk is low but non-zero: the API is undocumented and unversioned-in-public.** Upbound's ToS contains **zero** occurrences of scraping/crawling/robots/automated/rate-limit, and `robots.txt` blocks only AI-training crawlers while allowing `User-agent: *`. Treat the API as best-effort: cache aggressively, degrade to a bundled provider list, and never make a build fail because discovery was down.

---

## 1. Marketplace probing — what actually works

`marketplace.upbound.io` is a **Next.js Pages Router** app. It does not call `/api/...` on its own origin for package data; the CSP gave it away immediately:

```
$ curl -sSD- -o/dev/null https://marketplace.upbound.io/providers/upbound/provider-aws-sqs
HTTP/2 307
location: /providers/upbound/provider-aws-sqs/v2.7.1
content-security-policy: ... connect-src 'self' wss: api.upbound.io static.upbound.io accounts.upbound.io ...
```

**VERIFIED.** `connect-src` names `api.upbound.io` — that is the data plane. The earlier 404s on `/api/v1/packages/...` and `/api/v1/accounts/...` were 404s because those paths never existed on the *marketplace* origin; the API lives on a different host.

I pulled the JS bundles and recovered the client's base paths verbatim:

```js
// chunk 618-95dea89187199395.js  (v1 client)
this.packages       = "".concat("/v1","/packages")
this.packageMetadata= "".concat("/v1","/packageMetadata")
this.repositories   = "".concat("/v1","/repositories")
this.search         = "".concat("/v1","/search")
// (v2 client, same shape)
this.repositories="/v2/repositories"; this.packages="/v2/packages";
this.search="/v2/search"; this.packageMetadata="/v2/packageMetadata";
```

### 1a. The search endpoint — the one that matters

```
GET https://api.upbound.io/v2/search?type=packages&size=1
→ 200, application/json, ~0.26 s
```
**VERIFIED. No auth. No cookie. No API key.**

`type` is validated against an enum, and the 400 leaks it (a real OpenAPI validator is in front of this):

```
$ curl 'https://api.upbound.io/v2/search?type=Packages&size=3'
400 text/plain
parameter "type" in query has an error: value is not one of the allowed values
  ["packages","resources","charts","vulnerabilities"]
```
**VERIFIED.** Note `resources` and `vulnerabilities` are also searchable types — unexplored, possibly useful later.

Real trimmed response:

```json
{
  "packages": [
    {
      "account": "upbound",
      "repository": "provider-azapi",
      "repoKey": "upbound/provider-azapi",
      "packageType": "Provider",
      "public": true,
      "tier": "official",
      "pkgDigest": "sha256:d1b44f29bc654496b688222c68e208f335049cf8b1fdd5231bfe3f890401b062",
      "updatedAt": "2026-08-23T23:08:42Z",
      "downloadCount": 10837,
      "description": "Upbound's official Crossplane provider to manage Microsoft Azure API\nresources in Kubernetes.",
      "version": "v2.1.3",
      "annotations": { "license": ["Apache-2.0"], "verification": ["Official"], "host": ["XP","UXP","Spaces"], ... }
    }
  ],
  "count": 626, "filteredCount": null, "page": 0, "size": 24,
  "facets": { "packageType": [...], "tier": [...], "annotations.license": [...] }
}
```

Every field the UI needs is here: name, description, version, digest, tier, licence, download count, and an icon URL. `pkgDigest` lets you pin immediately without a registry round-trip.

### 1b. Filter grammar (recovered from the bundle, then verified)

The builder is a plain infix expression serialiser:

```js
// module 66655 + the XX() serialiser
if ("field" in t && "op" in t) return `${t.field} ${t.op} ${quote(t.value)}`
if ("values" in t)             return "(" + values.map(v=>`${field} = ${quote(v)}`).join(" OR ") + ")"
if ("NOT" in t)                return `NOT (${e(t.NOT)})`
if ("AND" in t)                return t.AND.map(e).join(" AND ")
```

Recognised fields: `query`, `accountName`, `tier`, `public`, `packageType`, `starred`, `excludeFamily`, `family`, `repository` (op `:`), `annotations.hardening`, `annotations.host`, `annotations.license`, `annotations.subscription`, `annotations.support`, `annotations.verification`. Operators `=`, `!=`, `:`.

Note **free text is a filter field, not a query parameter** — a top-level `?query=sqs` is silently ignored (returned all 820 packages). It must be `filter=query = "sqs"`.

VERIFIED results:

| filter | status | count | first hits |
|---|---|---|---|
| `packageType = "Provider"` | 200 | **626** | aviatrix/provider-aviatrix, crossplane-contrib/crossplane-provider-castai |
| `packageType = "Provider" AND excludeFamily = true` | 200 | **151** | upbound/provider-azapi, upbound/provider-azuread |
| `query = "sqs" AND packageType = "Provider"` | 200 | 42 | **upbound/provider-aws-sqs**, provider-aws-sns, provider-gcp-sql |
| `query = "s3" AND packageType = "Provider"` | 200 | 30 | **upbound/provider-aws-s3**, provider-aws-cur |
| `account = "crossplane-contrib"` | 200 | 89 | crossplane-provider-castai, function-auto-ready |
| `packageType = "Function"` | 200 | 109 | upbound/function-auto-ready, function-go-templating |
| `packageType = "Configuration"` | 200 | 76 | crossplane-contrib/x-generation |
| `packageType == "Provider"` (wrong op) | **400** | — | `{"status":400,"title":"Invalid Filter Expression","detail":"invalid filter syntax: ^ unexpected token = (1:13)"}` |

Relevance ranking is good — `query = "sqs"` puts `provider-aws-sqs` first, `query = "s3"` puts `provider-aws-s3` first. Good enough to ship as the search box.

### 1c. Pagination & enumeration

**VERIFIED.** `size` caps at 500 (`size=1000` → 400 `number must be at most 500`). Full enumeration of every provider:

```
page 0: +100 total=100 (count=626)   ... page 6: +26 total=626
UNIQUE PROVIDERS: 626       elapsed 5.8s wall, 7 requests, ~1.1 MB
```

At `size=500` that is **2 requests**. A full local catalogue is cheap enough to refresh on a timer and cache to disk.

### 1d. Versions

```
GET https://api.upbound.io/v1/packageMetadata/crossplane-contrib/provider-kubernetes  → 200, 377 B
{"public":true,"repoKey":"crossplane-contrib/provider-kubernetes","tier":"community","type":"provider",
 "versions":["v1.3.0","v1.2.1","v1.2.0","v1.1.0","v1.0.0","v0.18.0","v0.17.1", ... 27 total]}
```
**VERIFIED.** v1 returns a flat, newest-first string array — exactly what a version picker needs.

The v2 form is richer but noisier (30 KB, objects with `subscription_version`/`updated_at`, plus a private `relatedRepository`). **Use v1 for the version list.**

Per-version metadata:

```
GET https://api.upbound.io/v2/packageMetadata/upbound/provider-aws-sqs/latest → 200, 642 B
{"currentVersion":"v2.7.1",
 "digest":"sha256:dcce6930dfebf29dda07946babebca57fa6df4f6034e8a52501dca5eb85b97c1",
 "endOfLife":"2028-02-10","endOfSupport":"2027-08-10",
 "familyRepoKey":"upbound/provider-family-aws",
 "hasAttestation":true,"hasSignature":true,"publishedAt":"2026-08-21",
 "repoKey":"upbound/provider-aws-sqs","tier":"official","type":"provider"}
```
**VERIFIED.** `latest` is a valid version alias. Two things worth flagging:
- **`familyRepoKey` names the family package directly.** Given the known constraint that `crossplane xpkg get-crds` does not resolve `spec.dependsOn`, this is a one-request way to learn which family package to fetch second — without pulling and parsing the xpkg's `crossplane.yaml`.
- `endOfLife`/`endOfSupport` let the picker grey out or warn on dead versions.

### 1e. Bonus: full CRD schemas, anonymously

Recovered from the bundle: `${packages}/${acct}/${repo}/${ver}/resources` and `.../resources/${group}/${kind}`. These are **v1-only** (the v2 paths 404 with plain-text `404 page not found`, i.e. no such route; the v2 `/assets` route exists but returns `application/problem+json`).

```
GET /v1/packages/upbound/provider-aws-sqs/v2.7.1/resources → 200, 3.3 KB
```
```json
{"packageType":"Provider","customResourceDefinitions":[
 {"group":"sqs.aws.m.upbound.io","kind":"Queue","versions":["v1beta1"],
  "storageVersion":"v1beta1","scope":"Namespaced",
  "description":"Queue is the Schema for the Queues API. Provides a SQS resource."},
 {"group":"sqs.aws.upbound.io","kind":"Queue","versions":["v1beta1"],
  "storageVersion":"v1beta1","scope":"Cluster"}, ...],
 "compositeResourceDefinitions":[], "compositions":[], "managedResourceDefinitions":[...]}
```

```
GET /v1/packages/upbound/provider-aws-sqs/v2.7.1/resources/sqs.aws.upbound.io/Queue → 200, 20.4 KB
```
```json
{"kind":"CustomResourceDefinition","apiVersion":"apiextensions.k8s.io/v1",
 "metadata":{"name":"queues.sqs.aws.upbound.io"},
 "spec":{"group":"sqs.aws.upbound.io","names":{"plural":"queues","kind":"Queue","categories":["crossplane","managed","aws"]},
  "scope":"Cluster","versions":[{"name":"v1beta1","served":true,"storage":true,
   "schema":{"openAPIV3Schema":{"description":"Queue is the Schema for the Queues API...","type":"object", ...}}}]}}
```
**VERIFIED — the complete CRD with `openAPIV3Schema`.** Also verified against a contrib provider:
`/v1/packages/crossplane-contrib/provider-kubernetes/v1.3.0/resources/kubernetes.crossplane.io/Object` → 200, 26.7 KB, full CRD.

Note the response cleanly separates the v2 **namespaced** (`sqs.aws.m.upbound.io`, `scope: Namespaced`) from the v1 **cluster-scoped** (`sqs.aws.upbound.io`, `scope: Cluster`) API groups — useful for targeting Crossplane v2 correctly.

**Cost comparison vs. the xpkg pull:** the whole-package pull is ~20 KB / 5 requests and gives you every CRD. This API is ~20 KB *per CRD* but ~3 KB for the index. So it wins when the user wants one or two kinds (the common Composition case) and loses when they want the whole provider. Worth having both paths.

### 1f. Endpoints that do NOT work

| Endpoint | Status | Note |
|---|---|---|
| `/v1/repositories/upbound` | **401** `{"status":401,"title":"Unauthorized"}` | account repo listing needs login — not a discovery route |
| `/v1/repositories/crossplane-contrib` | **401** | same, even for a public org |
| `/v2/repositories/crossplane-contrib` | **401** | same |
| `/v2/packages/.../v2.7.1/resources` | **404** plain text `404 page not found` | route does not exist on v2 |
| `/v2/packages/.../v2.7.1/familyResources` | **404** | ditto |
| `/openapi.json`, `/openapi.yaml`, `/v2/openapi.json`, `/swagger.json`, `/docs` | **404** | no published spec |
| `?query=sqs` as a top-level param | 200 but **ignored** | returns all 820; free text must go in `filter` |

All **VERIFIED**.

`GET https://api.upbound.io/apis` → 200, but it is a Kubernetes-style `APIGroupList` for `upbound.io/v1alpha1` — the control-plane API, unrelated to the marketplace.

---

## 2. Next.js `_next/data` — works, but do not use it

**VERIFIED.** buildId `cpEmWMRO8fnP79BcxRb3m`, route `/[packageType]/[packageAccount]/[packageName]/[packageVersion]`.

```
GET /_next/data/cpEmWMRO8fnP79BcxRb3m/providers/upbound/provider-aws-sqs/v2.7.1.json → 200, 33.7 KB
GET /_next/data/cpEmWMRO8fnP79BcxRb3m/providers.json                                 → 200, 22.3 KB
GET /_next/data/cpEmWMRO8fnP79BcxRb3m/providers/upbound/provider-aws-sqs.json        → 200, 116 B
  {"pageProps":{"__N_REDIRECT":"/providers/upbound/provider-aws-sqs/v2.7.1","__N_REDIRECT_STATUS":307},"__N_SSP":true}
GET /_next/data/cpEmWMRO8fnP79BcxRb3m/index.json  → 200 but text/html (falls through to the SPA shell)
```

The payload is a **React Query dehydrated cache**, so it hands you the underlying API responses plus their query keys:

```json
["packageSearch",{"packageType":"Provider","size":24,"excludeFamily":true},false]
["packageVersions","upbound","provider-aws-sqs"]
["packageResources","upbound","provider-aws-sqs","v2.7.1"]
["packageMetadata","upbound","provider-family-aws","v2.7.1"]
```

That is how I found the REST layer, and it is a legitimate emergency fallback. **But it is coupled to `buildId`, which changes on every marketplace deploy** — you would have to scrape the HTML for the current buildId before every call. The direct `api.upbound.io` call returns the same data with no such coupling. Use the API.

---

## 3. OCI Distribution API on xpkg.upbound.io — the negative result was wrong

**This corrects the stated constraint.** `tags/list` is not empty; the earlier probe was unauthenticated.

**Step 1 — unauthenticated (what the earlier probe saw):**
```
GET https://xpkg.upbound.io/v2/  → 401
docker-distribution-api-version: registry/2.0
www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io"
{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}

GET https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list  → 401
www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io",scope="repository:upbound/provider-aws-sqs:pull"
{"errors":[{"code":"UNAUTHORIZED","message":"authentication required",
  "detail":[{"Type":"repository","Name":"upbound/provider-aws-sqs","Action":"pull"}]}]}
```
**VERIFIED.** A client that ignores the 401 body and reads `.tags` off it gets `nil` — which presents exactly as "an empty tag list". That is the bug.

**Step 2 — with the anonymous token (no credentials required to obtain it):**
```
GET https://xpkg.upbound.io/service/token?service=xpkg.upbound.io&scope=repository:upbound/provider-aws-sqs:pull
→ 200  {"access_token":"…","token":"…","expires_in":…,"issued_at":"…"}   (JWT, 796 chars)

GET https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list  -H "Authorization: Bearer <tok>"
→ 200, 27,975 bytes
```
**VERIFIED.**

```json
{"name":"upbound/provider-aws-sqs","tags":[
  "sha256-02b68b509bd5036bab01cacffc85f1adcfbb452029fccadda81983caef8fbd0e.att",
  "sha256-02b68b509bd5036bab01cacffc85f1adcfbb452029fccadda81983caef8fbd0e.sig",
  ... ,
  "v0.1.0-rc.0","v0.36.0","v0.37.0","v1.0.0","v2.0.1","v2.7.1", ...]}
```

Breakdown of the 446 tags:

| kind | count |
|---|---|
| total | **446** |
| cosign sidecars `sha256-*.sig` / `.att` / `.sbom` | **344 (77%)** |
| real version tags | **102** (100 semver-shaped) |

**Answer to "why did it look empty":** not pagination, not a non-conformant registry, not a wrong scope. **It was auth.** The registry is fully OCI-conformant — `n=` and `last=` both work:

```
?n=100                  → 200, exactly 100 tags
?n=100&last=v2.0.0      → 200, 19 tags, starting ["v2.0.1","v2.0.2","v2.1.0"]
```
**VERIFIED.** Note tags are ordered **lexicographically, not semantically**, so `last=` paging interleaves cosign tags with versions and `v0.9.0` sorts after `v0.10.0`. You must fetch all pages and sort client-side.

**Registry-wide catalogue is closed:**
```
GET https://xpkg.upbound.io/v2/_catalog?n=50  (with anon token, scope=registry:catalog:*)  → 401
{"errors":[{"code":"UNAUTHORIZED","detail":[{"Type":"registry","Name":"catalog","Action":"*"}]}]}
```
**VERIFIED.** The token service will not grant `registry:catalog:*` anonymously. So the OCI layer can enumerate *versions of a known repo* but **cannot enumerate repos** — which is precisely why discovery needs the marketplace API.

---

## 4. xpkg.crossplane.io and ghcr.io

**`xpkg.crossplane.io` is a passthrough to ghcr.io.** Its own `WWW-Authenticate` says so:

```
GET https://xpkg.crossplane.io/v2/  → 401
www-authenticate: Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull"
```
**VERIFIED.** And a token minted at `ghcr.io/token` is accepted verbatim by `xpkg.crossplane.io`, returning byte-identical output:

```
GET https://ghcr.io/token?service=ghcr.io&scope=repository:crossplane-contrib/provider-kubernetes:pull → 200 (84-char token)

GET https://ghcr.io/v2/crossplane-contrib/provider-kubernetes/tags/list            → 200, 149 B
GET https://xpkg.crossplane.io/v2/crossplane-contrib/provider-kubernetes/tags/list → 200, 149 B  (identical)

{"name":"crossplane-contrib/provider-kubernetes",
 "tags":["v0.17.0","v0.17.1","v0.18.0","v0.18.1-rc.0","v1.0.0","v1.1.0","v1.2.0","v1.2.1","v1.3.0"]}
```
**VERIFIED.** Treat them as one registry; one token path covers both.

ghcr is **much cleaner than xpkg.upbound.io** — 9 tags, all semver, zero cosign noise. And it emits a proper RFC 5988 `Link` header for pagination:

```
GET .../tags/list?n=5 → 200
link: </v2/crossplane-contrib/provider-kubernetes/tags/list?last=v1.0.0&n=5>; rel="next"
```
**VERIFIED.**

**Important caveat:** ghcr is a **partial mirror**. It lists 9 versions of `provider-kubernetes`; the marketplace API lists **27** (back to `v0.5.0`). If a user asks for an older version, ghcr alone will not have it. Another reason the marketplace API should be the primary version source, with the registry as the fallback.

---

## 5. The `up` CLI — no search/browse command exists

**`github.com/upbound/up` is now 404** (**VERIFIED** via `api.github.com/repos/upbound/up`). `upbound/upbound` exists but its own description is *"An issues-only repository for tracking issues encountered on Upbound product"* — no source. The CLI appears to have gone closed-source; community forks remain (`open-crossplane/up`, `nlinx/up`).

I cloned `open-crossplane/up` (last commit **2023-05-12**, so treat as historical) and read the command tree:

```
cmd/up/: configuration controlplane license.go login.go logout.go organization
         profile repository robot upbound uxp xpkg xpls
cmd/up/xpkg/:       batch.go build.go dep.go init.go push.go xpextract.go
cmd/up/repository/: create.go delete.go get.go list.go repository.go
```

**There is no `up search`, no `up marketplace`, no browse command.** A grep for `Search` across `cmd/` returns exactly one hit, and it is an unrelated error string in `xpkg/push.go`.

The closest thing, `up repository list`, lists repos **in your own logged-in account**:

```go
// cmd/up/repository/list.go
rList, err := rc.List(context.Background(), upCtx.Account, common.WithSize(maxItems))
// -> github.com/upbound/up-sdk-go/service/repositories, against
// internal/upbound/context.go: APIEndpoint default https://api.upbound.io
```

And I confirmed that endpoint is closed to anonymous callers (§1f: `/v1/repositories/upbound` → 401). **Conclusion: the `up` CLI is not a discovery path, and there is no CLI precedent to copy.** The marketplace web app is the only client of the search API, which is why the API is undocumented.

---

## 6. Terms of use / licence for programmatic access

**`robots.txt` — VERIFIED:**
```
$ curl https://marketplace.upbound.io/robots.txt      # 200, 158 bytes
User-agent: GPTBot
User-agent: ClaudeBot
User-agent: Google-Extended
Disallow: /

User-agent: *
Allow: /

Sitemap: https://marketplace.upbound.io/sitemap.xml
```
Only **AI-training crawlers** are disallowed. Everything else is explicitly `Allow: /`. A developer tool fetching provider metadata on a user's behalf is not GPTBot/ClaudeBot/Google-Extended. `api.upbound.io/robots.txt` → 404 (no policy asserted).

**Terms & Conditions** — <https://www.upbound.io/terms-conditions> (the marketplace footer links here; `/legal/terms-and-conditions` is a 404). Page fetched **VERIFIED**, last-modified header `Aug 20, 2026`.

Keyword scan of the full extracted text — **VERIFIED, all zero:**

| term | occurrences |
|---|---|
| `scrap` | 0 |
| `crawl` | 0 |
| `robot` | 0 |
| `automated` | 0 |
| `rate limit` | 0 |
| `bot ` | 0 |

The nearest clause is **Section 2, "RESTRICTIONS AND RESPONSIBILITIES"**, quoted verbatim:

> "2.1 Customer will not, directly or indirectly: reverse engineer, decompile, disassemble or otherwise attempt to discover the source code, object code or underlying structure, ideas, know-how or algorithms relevant to the Services or any software, documentation or data related to the Services ("Software"); modify, translate, or create derivative works based on the Services or any Software (except to the extent expressly permitted by Company or authorized within the Services); use the Services or any Software for time sharing or service bureau purposes or otherwise for the benefit of a third; or remove any proprietary notices or labels."

> "2.3 Customer represents, covenants, and warrants that Customer will use the Services only in compliance with Company's standard published policies then in effect (the "Policy") and all applicable laws and regulations."

**Reading.** This is a **subscription agreement binding a "Customer"** — a party who has signed up for paid Services. It is not a browsewrap covering anonymous public HTTP. There is no anti-automation, anti-scraping, or API-restriction clause anywhere in it. 2.3 incorporates by reference an undefined "standard published policies", and the only published policy I could find is `robots.txt`, which permits us.

The "reverse engineer … underlying structure" language in 2.1 is the one clause to be aware of — I did read the marketplace's minified JS to recover the filter grammar. That JS is served publicly and unobfuscated to every browser, and reading it is ordinary web-client interoperability rather than decompilation, but it is the weakest point of the position. Nothing in 2.1 restricts *calling* the resulting endpoints.

**Practical risk assessment.** Low legal risk, moderate **stability** risk. The API is undocumented, has no published OpenAPI spec (all spec paths 404), no versioning guarantee beyond the `/v1` vs `/v2` prefixes, and exactly one known consumer. Upbound could change or gate it without notice. Recommended posture:
- Send a descriptive `User-Agent` identifying the tool and its repo, so Upbound can contact us rather than block us.
- Cache aggressively on disk; the offline requirement already demands this.
- Treat any non-200 as "discovery unavailable", fall back to the cache, then to a bundled seed list, and never fail a generate/build on it.
- Consider opening an issue on `upbound/upbound` asking whether the search API is fair game — cheap goodwill, and it converts an assumption into a documented answer.

---

## Rate limits and operational characteristics — VERIFIED

- **30 consecutive `/v2/search` requests: 30/30 returned 200.** No 429, no backoff, no `Retry-After`.
- **No rate-limit headers of any kind.** Response headers are only `x-envoy-upstream-service-time`, `server: istio-envoy`. Nothing to introspect, so implement client-side politeness rather than header-driven throttling.
- Latency: 0.26–0.48 s per call from a residential IP.
- **No CORS.** `OPTIONS /v2/search` → **405**; `GET` with `Origin: http://localhost:8080` → 200 but **no `Access-Control-Allow-Origin`**. Browser calls are impossible; the Go binary must proxy.
- GitHub API, for contrast, is 60 req/hr unauthenticated (`x-ratelimit-limit: 60`) — far tighter than anything Upbound imposes.

**Sitemap as a weak fallback.** `https://marketplace.upbound.io/sitemap.xml` → 200, 11.4 KB, 103 URLs of which 94 are `/providers/...`. That is **94 of 626** — badly incomplete. Usable only as a last-resort seed list, not as a catalogue.

---

## Endpoint reference table

| # | URL | Status | Auth | Notes |
|---|---|---|---|---|
| 1 | `GET api.upbound.io/v2/search?type=packages&size=N&page=P&filter=…` | 200 | none | **primary discovery.** size≤500, count=626 providers |
| 2 | `GET api.upbound.io/v1/packageMetadata/{acct}/{repo}` | 200 | none | **version list**, flat newest-first string array |
| 3 | `GET api.upbound.io/v2/packageMetadata/{acct}/{repo}/{ver\|latest}` | 200 | none | digest, EOL, `familyRepoKey`, tier |
| 4 | `GET api.upbound.io/v1/packages/{acct}/{repo}/{ver}/resources` | 200 | none | CRD/XRD/Composition index, ~3 KB |
| 5 | `GET api.upbound.io/v1/packages/{acct}/{repo}/{ver}/resources/{group}/{kind}` | 200 | none | **full CRD + openAPIV3Schema**, ~20 KB |
| 6 | `GET xpkg.upbound.io/service/token?service=…&scope=repository:{repo}:pull` | 200 | none | anonymous JWT |
| 7 | `GET xpkg.upbound.io/v2/{repo}/tags/list` | **401→200** | anon token | 446 tags, 77% cosign noise; `n=`/`last=` work |
| 8 | `GET ghcr.io/token?service=ghcr.io&scope=repository:{repo}:pull` | 200 | none | anonymous token |
| 9 | `GET ghcr.io/v2/{repo}/tags/list` | 200 | anon token | clean semver, `Link: rel="next"`; partial mirror |
| 10 | `GET xpkg.crossplane.io/v2/{repo}/tags/list` | 200 | ghcr token | identical to #9 — same registry |
| 11 | `GET marketplace.upbound.io/_next/data/{buildId}/providers/{a}/{r}/{v}.json` | 200 | none | works; buildId-fragile, do not ship |
| 12 | `GET marketplace.upbound.io/sitemap.xml` | 200 | none | 94/626 providers — incomplete |
| 13 | `GET api.upbound.io/v1/repositories/{acct}` | **401** | login | not a discovery route |
| 14 | `GET xpkg.upbound.io/v2/_catalog` | **401** | denied | registry-wide enumeration closed |
| 15 | `GET api.upbound.io/{openapi.json,swagger.json,docs}` | **404** | — | no published spec |

---

## Suggested implementation shape

```
cf provider search sqs
  → GET /v2/search?type=packages&size=25&page=0
        &filter=query = "sqs" AND packageType = "Provider"

cf provider list
  → GET /v2/search?type=packages&size=500&page=0
        &filter=packageType = "Provider" AND excludeFamily = true   (151 results, 1 request)

cf provider versions upbound/provider-aws-sqs
  → GET /v1/packageMetadata/upbound/provider-aws-sqs         (flat array)
  → fallback: tags/list on xpkg.upbound.io with anon token, filter out
              /^sha256-.*\.(sig|att|sbom)$/, semver-sort client-side

cf provider add <picked>
  → GET /v2/packageMetadata/{a}/{r}/{v}   → digest (pin) + familyRepoKey (2nd fetch target)
  → then the existing 20 KB / 5-request xpkg pull
```

Cache every response under the existing cache dir keyed by URL + ETag-or-fetch-date; serve stale on any network error so the offline guarantee holds.

## Open questions for follow-up

- `type=resources` on `/v2/search` — searching **by CRD kind** across all providers ("who provides a `Queue`?") would be a genuinely better UX than searching by provider name. Untested; worth 15 minutes.
- Does the `/v1/.../resources/{group}/{kind}` CRD match the xpkg-extracted CRD byte-for-byte, or is it a lossy re-serialisation? Must diff before trusting it as a schema source.
- `managedResourceDefinitions` appeared in the `/resources` index alongside CRDs — a Crossplane v2 MRD concept worth understanding for generation.
