# Discovery via GitHub: crossplane-contrib, upbound, releases

Research date: 2026-08-27/28. All findings marked **VERIFIED** were executed as live HTTP
requests from this machine; **DOCS** means read, not run.

---

## Decisions this enables

1. **Use GitHub for the *repo list* only, never for the version list.** Verified: `crossplane-contrib/provider-upjet-aws` latest GitHub *release* is `v2.7.0`, but all 177 AWS family packages in the registry are at `v2.7.1`, and `v2.7.1` has **no GitHub tag and no GitHub release** (both `git/ref/tags/v2.7.1` and `releases/tags/v2.7.1` return 404). A GitHub-derived version list ships a wrong "latest".

2. **The prior "xpkg tag list is empty" result is WRONG — `tags/list` works fine and should be the version source.** Verified: `GET https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list` with an anonymous bearer token returns **HTTP 200 with 446 tags**, 102 of them real versions. The earlier empty result was one of two things: (a) no bearer token → HTTP 401 with an error body, or (b) using `?n=<small>` and reading only page 1, which is 100 % cosign `sha256-….sig/.att/.sbom` tags because those sort lexicographically before `v*`. Full detail in §3.

3. **Resolve repo → OCI ref from `Makefile` (`XPKG_REG_ORGS` + `XPKGS`), then *validate against the registry* with a 1-request oracle, then fall back.** Verified across all 60 active provider repos in both orgs: 49/60 (82 %) resolve, 46 of those from the Makefile alone. Of the 11 misses, 10 are repos with zero releases or last pushed 2020–2022 — i.e. **~49/50 of actually-shipping providers resolve**. A naive `org/repo` → `xpkg.upbound.io/org/repo` rule would be wrong for at least 12 of them (§2).

4. **Family sub-packages are 100 % derivable from the repo tree, and this is the only way to get them — they have no GitHub repos at all.** Verified: `upbound/provider-aws-sqs` → GitHub 404. The list comes from `cmd/provider/*` directories. Derived 177 AWS names, **177/177 exist in the registry** (4.6 s, no auth, no rate limit). Same rule: Azure 93/93, GCP 81/81.

5. **Ship discovery with zero GitHub credentials by using `raw.githubusercontent.com` for file reads (verified: does NOT consume the 60/hr API budget) and the registry itself for tags.** The API is needed only for the ~2-request org repo listing. Do not require a user token; do accept one as an optional accelerator. I exhausted the entire unauthenticated 60/hr budget during this single research session — that is how tight it is if you hit `api.github.com` per provider (§4).

---

## 1. Enumerating the orgs

### 1.1 `crossplane-contrib` — VERIFIED

```
GET https://api.github.com/orgs/crossplane-contrib/repos?per_page=100&page={1,2}&type=public
HTTP 200, no auth needed
link: <…?per_page=100&page=2>; rel="next", …; rel="last"
```

140 public repos, 2 pages. Response shape (trimmed, one element):

```json
{
  "name": "provider-gcp",
  "full_name": "crossplane-contrib/provider-gcp",
  "description": "Crossplane GCP provider",
  "archived": true,
  "fork": false,
  "stargazers_count": 128,
  "pushed_at": "2025-05-30T08:19:53Z",
  "default_branch": "master",
  "topics": [],
  "license": "Apache-2.0",
  "html_url": "https://github.com/crossplane-contrib/provider-gcp"
}
```

**87 repos match `provider-*`: 50 active, 37 archived.** 33 pushed in 2026.
Also 22 `function-*`.

| repo | ★ | state | last push |
|---|---:|---|---|
| provider-aws | 494 | active | 2026-08-12 |
| provider-upjet-aws | 217 | active | 2026-08-21 |
| provider-kubernetes | 197 | active | 2026-08-27 |
| provider-sql | 154 | active | 2026-08-12 |
| provider-helm | 143 | active | 2026-08-27 |
| provider-gcp | 128 | **ARCHIVED** | 2025-05-30 |
| provider-terraform | 118 | active | 2026-08-27 |
| provider-upjet-azure | 106 | active | 2026-08-26 |
| provider-argocd | 98 | active | 2026-08-24 |
| provider-azure | 94 | **ARCHIVED** | 2025-05-22 |
| provider-upjet-gcp | 94 | active | 2026-08-27 |
| provider-gitlab | 84 | active | 2026-08-27 |
| provider-http | 74 | active | 2026-08-24 |
| provider-ansible | 71 | active | 2026-05-16 |
| provider-keycloak | 71 | active | 2026-08-24 |
| provider-openstack | 67 | active | 2026-07-27 |
| provider-upjet-github | 56 | active | 2026-08-25 |
| provider-alibaba | 52 | **ARCHIVED** | 2024-07-05 |
| provider-kafka | 46 | active | 2026-08-24 |
| provider-digitalocean | 42 | **ARCHIVED** | 2025-03-07 |
| provider-cloudflare | 39 | **ARCHIVED** | 2026-03-06 |
| provider-jet-aws | 37 | **ARCHIVED** | 2022-11-07 |
| provider-spotify | 32 | active | 2025-06-17 |
| provider-github | 24 | **ARCHIVED** | 2024-10-08 |
| provider-civo | 23 | active | 2025-01-16 |
| provider-ibm-cloud | 20 | **ARCHIVED** | 2022-11-03 |
| provider-capi | 20 | active | **2020-11-29** |
| provider-rook | 18 | **ARCHIVED** | 2020-12-14 |
| provider-jet-azure | 17 | **ARCHIVED** | 2022-11-04 |
| provider-mongodbatlas | 17 | active | 2026-08-24 |
| provider-upjet-digitalocean | 17 | active | 2026-08-15 |
| provider-equinix-metal | 16 | **ARCHIVED** | 2024-02-16 |
| provider-nop | 16 | active | 2025-09-05 |
| provider-upjet-azuread | 16 | active | 2026-08-27 |
| provider-pagerduty | 15 | active | 2026-08-25 |
| provider-styra | 14 | active | **2022-12-16** |
| provider-jet-template | 14 | **ARCHIVED** | 2022-11-04 |
| provider-terraform-vsphere | 13 | **ARCHIVED** | 2021-10-12 |
| provider-cloudinit | 13 | **ARCHIVED** | 2023-03-13 |
| provider-confluent | 13 | active | 2026-01-20 |
| provider-newrelic | 12 | **ARCHIVED** | 2025-10-07 |
| provider-jet-gcp | 12 | **ARCHIVED** | 2022-11-04 |
| provider-tencentcloud | 11 | active | 2025-04-24 |
| provider-in-cluster | 10 | **ARCHIVED** | 2021-06-22 |
| provider-upjet-cloudflare | 10 | active | 2026-06-09 |
| provider-zpa | 9 | active | **2022-10-28** |
| provider-jet-equinix | 9 | active | 2024-09-11 |
| provider-sonarqube | 9 | active | 2026-08-27 |
| provider-bitbucket-server | 8 | **ARCHIVED** | 2022-11-11 |
| provider-okta | 7 | active | 2025-10-14 |
| provider-ssh | 6 | **ARCHIVED** | 2020-09-24 |
| provider-gcp-beta | 6 | active | **2022-05-27** |
| provider-jet-vault | 6 | **ARCHIVED** | 2025-10-03 |
| provider-jet-datadog | 6 | **ARCHIVED** | 2025-10-02 |
| provider-upjet-alibabacloud | 6 | active | 2026-08-27 |
| provider-talos | 6 | active | 2026-05-26 |
| provider-k3s | 6 | active | 2026-08-21 |
| provider-tinkerbell | 5 | **ARCHIVED** | 2023-03-13 |
| provider-tf-equinix-metal | 5 | **ARCHIVED** | 2024-02-16 |
| provider-kops | 5 | active | **2022-11-22** |
| provider-vultr | 5 | **ARCHIVED** | 2023-10-25 |
| provider-rancher | 4 | **ARCHIVED** | 2021-06-07 |
| provider-yandex-cloud | 4 | **ARCHIVED** | 2025-10-02 |
| provider-jet-rancher | 4 | **ARCHIVED** | 2022-08-03 |
| provider-linode | 3 | **ARCHIVED** | 2023-03-13 |
| provider-gen-aws | 3 | **ARCHIVED** | 2020-10-26 |
| provider-equinix | 3 | **ARCHIVED** | 2024-05-30 |
| provider-jet-alibaba | 3 | **ARCHIVED** | 2022-04-11 |
| provider-palette | 3 | active | 2026-07-31 |
| provider-dynatrace | 3 | active | 2026-03-10 |
| provider-checkly | 3 | active | 2026-05-07 |
| provider-secret | 2 | **ARCHIVED** | 2020-12-01 |
| provider-influxdb | 2 | active | **2022-01-20** |
| provider-jet-ec | 2 | active | **2022-03-11** |
| provider-upjet-kafka | 2 | active | 2024-03-12 |
| provider-infoblox-nios | 2 | active | 2025-10-15 |
| provider-workflows | 2 | active | 2026-02-25 |
| provider-opsgenie | 1 | **ARCHIVED** | 2020-12-01 |
| provider-terraform-aws | 1 | **ARCHIVED** | 2021-10-18 |
| provider-jet-linode | 1 | **ARCHIVED** | 2021-12-22 |
| provider-planetscale | 1 | **ARCHIVED** | 2022-05-20 |
| provider-upjet-ec | 1 | active | 2024-05-09 |
| provider-upjet-mysql | 1 | active | 2024-09-27 |
| provider-redpanda | 1 | active | 2026-08-21 |
| provider-cortex | 0 | **ARCHIVED** | 2023-08-09 |
| provider-upjet-edgeadc | 0 | active | 2024-10-04 |
| provider-upjet-zitadel | 0 | active | 2026-07-22 |

**Name-prefix filtering is unsound in both directions — VERIFIED.**
- False positive: `provider-workflows` is *"Repository for commonly shared GitHub workflows"*, not a provider.
- False negatives: `crossplane-provider-castai` and `crossplane-provider-newrelic` are real providers whose names do not start with `provider-`.

Better classifier, VERIFIED: fetch `package/crossplane.yaml` from the default branch and read `kind:`. Over the 85 active contrib repos this gives `Provider: 47, Function: 19, Configuration: 2, none: 17` and correctly picks up castai/newrelic while excluding provider-workflows. Caveat: it **misses the three big family monorepos** (`provider-upjet-aws`, `-azure`, `-gcp`) because upjet generates `crossplane.yaml` at build time — the same root cause that makes doc.crds.dev useless for them. Those three need special-casing.

Bonus, VERIFIED: `package/crossplane.yaml` `metadata.name` **equals the registry package basename in 36/36 cases with 0 disagreements**. That is the authoritative package-name source, e.g. repo `provider-upjet-azuread` → `metadata.name: provider-azuread` → `xpkg.upbound.io/upbound/provider-azuread`.

GitHub **topics are useless** — VERIFIED: only 16 of 85 active repos have any topics; the `crossplane-provider` topic sits on `xp-testing` (a test framework) and is absent from provider-kubernetes, provider-aws, provider-helm.

### 1.2 `upbound` — VERIFIED

```
GET https://api.github.com/orgs/upbound/repos?per_page=100  (3 pages) → HTTP 200
```
210 repos, matching the earlier figure. 72 match `configuration-*` / `platform-ref-*` (close to the earlier 73 — one has since been renamed or archived). Only **11 match `provider-*`**, all active:

| repo | ★ | last push |
|---|---:|---|
| provider-opentofu | 60 | 2026-08-23 |
| provider-vault | 31 | 2026-08-26 |
| provider-datadog | 9 | 2026-06-05 |
| provider-upbound | 8 | 2026-08-21 |
| provider-existing-cluster | 4 | 2024-01-22 |
| provider-dummy | 4 | 2024-01-22 |
| provider-upjet-azapi | 2 | 2026-08-24 |
| provider-upjet-nebius | 2 | 2026-08-21 |
| provider-upjet-gcp-beta | 0 | 2026-08-23 |
| provider-upjet-aws-devin | 0 | 2025-03-13 |
| provider-upjet-vultr | 0 | 2026-08-21 |

**The big official providers are NOT in the `upbound` org any more — VERIFIED.**

```
GET https://api.github.com/repos/upbound/provider-aws    → HTTP 301 → /repositories/545499499
GET https://api.github.com/repos/upbound/provider-azure  → HTTP 301
GET https://api.github.com/repos/upbound/provider-gcp    → HTTP 301
```
All three redirect to `crossplane-contrib/provider-upjet-{aws,azure,gcp}`. So the org→repo enumeration must follow 301s or the biggest providers vanish.

And the crux for the user's own example:

```
GET https://api.github.com/repos/upbound/provider-aws-sqs  → HTTP 404 Not Found
```

**`provider-aws-sqs` — the exact package in the user's `cf provider add` example — has no GitHub repository at all.** Nor do the other 176 AWS service packages, nor the 93 Azure or 81 GCP ones. **≈350 of the most-used Crossplane packages are invisible to any repo-listing approach.** Handling that is not an optimisation; it is the requirement.

`upbound/provider-upjet-aws-devin` resolves to the same OCI ref as the real AWS family — a fork/experiment that will produce a duplicate catalogue entry. Dedup on resolved ref, not on repo.

---

## 2. repo → OCI reference: is it mechanical?

Short answer: **mostly, with a validated fallback chain — never from a single source, and never from prose alone.**

### 2.1 The primary rule (Makefile) — VERIFIED on 8 hand-picked, then all 60

Every provider built on `crossplane/build` sets these in its root `Makefile`:

```
XPKG_REG_ORGS            ?= xpkg.upbound.io/crossplane-contrib index.docker.io/crossplanecontrib
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/crossplane-contrib
XPKGS                     = provider-kubernetes
```

Rule: `ref = first_token(XPKG_REG_ORGS_NO_PROMOTE || XPKG_REG_ORGS) + "/" + first_token(XPKGS || PROJECT_NAME)`, with one pass of `$(VAR)` expansion (several repos write `XPKGS = $(PROJECT_NAME)`).

The 8-provider sample, all fetched from `raw.githubusercontent.com` (no auth, no API budget):

| # | GitHub repo | branch | derived ref | correct? |
|---|---|---|---|---|
| 1 | crossplane-contrib/provider-kubernetes | main | `xpkg.upbound.io/crossplane-contrib/provider-kubernetes` | ✅ 96 version tags |
| 2 | crossplane-contrib/provider-helm | main | `xpkg.upbound.io/crossplane-contrib/provider-helm` | ✅ 52 |
| 3 | crossplane-contrib/provider-upjet-aws | main | `xpkg.upbound.io/upbound/provider-aws` (+ family) | ✅ 641 |
| 4 | crossplane-contrib/provider-sql | **master** | `xpkg.upbound.io/crossplane-contrib/provider-sql` | ✅ 157 |
| 5 | crossplane-contrib/provider-terraform | main | `xpkg.upbound.io/**upbound**/provider-terraform` | ✅ 219 |
| 6 | crossplane-contrib/provider-keycloak | main | `xpkg.upbound.io/crossplane-contrib/provider-keycloak` | ✅ 1039 |
| 7 | upbound/provider-vault | main | `xpkg.upbound.io/upbound/provider-vault` | ✅ 60 |
| 8 | upbound/provider-opentofu | main | `xpkg.upbound.io/upbound/provider-opentofu` | ✅ 34 |

**8/8 resolved mechanically, zero prose reading.** But note the traps already visible in that sample: #4 is on `master` not `main`; #5's GitHub org (`crossplane-contrib`) differs from its OCI org (`upbound`); #3's OCI *name* differs from the repo name **and** the org differs **and** it is a family that expands to 178 packages.

Do **not** use `PROJECT_REPO` as the GitHub location. `crossplane-contrib/provider-upjet-aws/Makefile` still says `PROJECT_REPO := github.com/upbound/provider-aws/v2` — the Go module path was never updated after the donation.

### 2.2 Full run over all 60 active provider repos — VERIFIED

Chain tried per repo, each candidate validated by actually listing tags in the target registry:
`Makefile` → `examples/{install,provider}.yaml` `package:` → OCI refs regexed out of `README.md` → guess `ghcr.io/<org>/<repo>` → guess `xpkg.upbound.io/<org>/<repo>`.

```
Counter({'makefile': 46, None: 11, 'guess-xpkg': 1, 'readme': 1, 'guess-ghcr': 1})
resolved: 49 / 60
```

**82 % resolved; 77 % from the Makefile alone.** Full resolved list:

```
crossplane-contrib/provider-ansible             ghcr.io/crossplane-contrib/provider-ansible
crossplane-contrib/provider-argocd              xpkg.upbound.io/crossplane-contrib/provider-argocd
crossplane-contrib/provider-aws                 xpkg.upbound.io/crossplane-contrib/provider-aws
crossplane-contrib/provider-civo                xpkg.upbound.io/civo/provider-civo               ← 3rd-party org
crossplane-contrib/provider-confluent           xpkg.upbound.io/crossplane-contrib/provider-confluent
crossplane-contrib/provider-gitlab              xpkg.upbound.io/crossplane-contrib/provider-gitlab
crossplane-contrib/provider-helm                xpkg.upbound.io/crossplane-contrib/provider-helm
crossplane-contrib/provider-http                xpkg.upbound.io/crossplane-contrib/provider-http
crossplane-contrib/provider-infoblox-nios       xpkg.upbound.io/crossplane-contrib/provider-infoblox-nios
crossplane-contrib/provider-jet-equinix         xpkg.upbound.io/equinix/provider-jet-equinix     ← 3rd-party org
crossplane-contrib/provider-k3s                 xpkg.upbound.io/crossplane-contrib/provider-k3s
crossplane-contrib/provider-kafka               xpkg.upbound.io/crossplane-contrib/provider-kafka
crossplane-contrib/provider-keycloak            xpkg.upbound.io/crossplane-contrib/provider-keycloak
crossplane-contrib/provider-kubernetes          xpkg.upbound.io/crossplane-contrib/provider-kubernetes
crossplane-contrib/provider-mongodbatlas        xpkg.upbound.io/crossplane-contrib/provider-mongodbatlas
crossplane-contrib/provider-nop                 xpkg.upbound.io/crossplane-contrib/provider-nop
crossplane-contrib/provider-okta                xpkg.upbound.io/crossplane-contrib/provider-okta
crossplane-contrib/provider-openstack           xpkg.upbound.io/crossplane-contrib/provider-openstack
crossplane-contrib/provider-pagerduty           ghcr.io/crossplane-contrib/provider-pagerduty
crossplane-contrib/provider-palette             xpkg.upbound.io/crossplane-contrib/provider-palette
crossplane-contrib/provider-sonarqube           ghcr.io/crossplane-contrib/provider-sonarqube    ← Makefile WRONG
crossplane-contrib/provider-spotify             xpkg.upbound.io/crossplane-contrib/provider-spotify
crossplane-contrib/provider-sql                 xpkg.upbound.io/crossplane-contrib/provider-sql
crossplane-contrib/provider-styra               xpkg.upbound.io/crossplane-contrib/provider-styra
crossplane-contrib/provider-talos               ghcr.io/crossplane-contrib/provider-talos        ← Makefile WRONG
crossplane-contrib/provider-tencentcloud        xpkg.upbound.io/crossplane-contrib/provider-tencentcloud
crossplane-contrib/provider-terraform           xpkg.upbound.io/upbound/provider-terraform       ← org differs
crossplane-contrib/provider-upjet-alibabacloud  xpkg.upbound.io/crossplane-contrib/provider-upjet-alibabacloud
crossplane-contrib/provider-upjet-aws           xpkg.upbound.io/upbound/provider-aws       FAMILY (178)
crossplane-contrib/provider-upjet-azure         xpkg.upbound.io/upbound/provider-azure     FAMILY (94)
crossplane-contrib/provider-upjet-azuread       xpkg.upbound.io/upbound/provider-azuread   ← name+org differ
crossplane-contrib/provider-upjet-digitalocean  xpkg.upbound.io/crossplane-contrib/provider-upjet-digitalocean
crossplane-contrib/provider-upjet-ec            xpkg.upbound.io/crossplane-contrib/provider-upjet-ec
crossplane-contrib/provider-upjet-gcp           xpkg.upbound.io/upbound/provider-gcp       FAMILY (82)
crossplane-contrib/provider-upjet-github        ghcr.io/crossplane-contrib/provider-upjet-github
crossplane-contrib/provider-upjet-kafka         xpkg.upbound.io/crossplane-contrib/provider-upjet-kafka
crossplane-contrib/provider-upjet-mysql         xpkg.upbound.io/crossplane-contrib/provider-upjet-mysql
crossplane-contrib/provider-upjet-zitadel       xpkg.upbound.io/crossplane-contrib/provider-upjet-zitadel
crossplane-contrib/provider-zpa                 xpkg.upbound.io/crossplane-contrib/provider-zpa
upbound/provider-datadog                        xpkg.upbound.io/upbound/provider-datadog
upbound/provider-dummy                          xpkg.upbound.io/upbound/provider-dummy
upbound/provider-opentofu                       xpkg.upbound.io/upbound/provider-opentofu
upbound/provider-upbound                        xpkg.upbound.io/upbound/provider-upbound
upbound/provider-upjet-aws-devin                xpkg.upbound.io/upbound/provider-aws       ← duplicate of AWS
upbound/provider-upjet-azapi                    xpkg.upbound.io/upbound/provider-azapi
upbound/provider-upjet-gcp-beta                 xpkg.upbound.io/upbound/provider-gcp-beta  FAMILY
upbound/provider-upjet-nebius                   xpkg.upbound.io/upbound/provider-nebius
upbound/provider-upjet-vultr                    xpkg.upbound.io/upbound/provider-vultr
upbound/provider-vault                          xpkg.upbound.io/upbound/provider-vault
```

One more shape worth flagging: `provider-upjet-alibabacloud`'s Makefile yields `crossplane-contrib/provider-alibabacloud`, which **404s**, while `crossplane-contrib/provider-family-alibabacloud` and `crossplane-contrib/provider-alibabacloud-ecs` both return 200 and `crossplane-contrib/provider-upjet-alibabacloud` (the repo-name guess) has 25 tags. A repo can publish a monolith *and* a family under three different names, only two of which exist. Only registry validation disambiguates.

**Naive `xpkg.upbound.io/<gh-org>/<gh-repo>` would be wrong for ≥12 of the 49** — 6 on ghcr.io instead, 3 in a third-party OCI org (`civo`, `equinix`, `upbound`), 4 where the package name differs from the repo name, and 2 where the Makefile itself is stale.

### 2.3 The 11 misses — mostly not real misses

```
provider-capi              0 releases, last push 2020-11-29
provider-jet-ec            0 releases, last push 2022-03-11
provider-upjet-edgeadc     0 releases, last push 2024-10-04
provider-upjet-cloudflare  0 releases, last push 2026-06-09
provider-dynatrace         0 releases, last push 2026-03-10   (no default branch content at all)
provider-redpanda          0 releases, 1 tag, last push 2026-08-21
provider-gcp-beta          1 release,  last push 2022-05-27
provider-influxdb          3 releases, last push 2022-01-20
provider-kops              2 releases, last push 2022-11-22
upbound/provider-existing-cluster  3 releases, last push 2024-01-22
provider-checkly           2 releases — REAL MISS, see below
```

10/11 have never shipped an installable package or were abandoned 2+ years ago. **The only genuine miss is `provider-checkly`**, and it is a fascinating one: it is the sole repo of the 60 that ships its package as a **GitHub release asset** (`provider-checkly-v0.1.1.xpkg`) and publishes to no registry. Its README and `examples/install.yaml` both advertise `xpkg.crossplane.io/crossplane-contrib/provider-checkly:v0.1.0` — a placeholder ref that does not exist (`ghcr.io/token` → **HTTP 403 DENIED**).

### 2.4 Where the mapping is recorded, ranked — VERIFIED

| source | present in | mechanical? | verdict |
|---|---|---|---|
| `Makefile` `XPKG_REG_ORGS` + `XPKGS` | 52/60 | **yes** | primary, but must be validated |
| `package/crossplane.yaml` `metadata.name` | 47/60 (misses upjet families) | **yes**, 36/36 agree with registry | best *name* source; carries no registry/org |
| `.github/workflows/*.yml` `XPKG_REG_ORGS:` | **2/60** | partly | rare, but overrides the Makefile when present |
| `examples/install.yaml` `package:` | 32/60 | yes | often a stale template placeholder (`:v0.1.0`) |
| README install snippet | 18/60 | regexable, not prose | useful tiebreak; wins for provider-talos |
| GitHub **release assets** | **1/60** | n/a | effectively never a source |
| GitHub release *body* | all | **no — prose** | free-form changelog Markdown; useless |

**The definitive proof that this is NOT purely mechanical.**
`crossplane-contrib/provider-sonarqube/.github/workflows/ci.yml:357`:

```yaml
XPKG_REG_ORGS: ${{ env.UPBOUND_MARKETPLACE_PUSH_ROBOT_USR != '' && 'xpkg.upbound.io/crossplane-contrib ghcr.io/crossplane-contrib' || 'ghcr.io/crossplane-contrib' }}
```

The publish target is **conditional on whether a CI secret is configured**. Nothing in the repository tells you which branch was taken. Only the registry does. (In fact it went ghcr-only: `xpkg.upbound.io/crossplane/provider-sonarqube` → 404, `ghcr.io/crossplane-contrib/provider-sonarqube` → 45 version tags.)

Same story for `provider-talos`: `ci.yml` sets `XPKG_REG_ORGS: ghcr.io/crossplane-contrib`, silently overriding the Makefile's `xpkg.upbound.io/...`, which 404s.

**Conclusion: repo→OCI is mechanical *as a hypothesis generator* and must be *falsified against the registry*. Never publish an unvalidated ref to the user.**

### 2.5 Family sub-packages: fully mechanical, and only obtainable this way — VERIFIED

`crossplane-contrib/provider-upjet-aws/Makefile:66-68`:
```make
SUBPACKAGES ?= monolith
ifeq ($(strip $(SUBPACKAGES)),*)
override SUBPACKAGES := $(filter-out monolith,$(shell find cmd/provider -type d -maxdepth 1 -mindepth 1 | cut -d/ -f3))
```

Rule: `ls cmd/provider/` → drop `monolith` → map `config` to `provider-family-<name>` → everything else to `provider-<name>-<dir>`.

```
GET https://api.github.com/repos/crossplane-contrib/provider-upjet-aws/contents/cmd/provider
→ HTTP 200, 178 directories: accessanalyzer … sqs … xray
```

Derived 177 refs, checked each against the registry:

```
$ cat aws_derived.txt | xargs -P12 -I{} ./exists.sh {} | awk '{print $1}' | sort | uniq -c
 177 200
real 4.6s
```

**177/177. Azure: 93/93. GCP: 81/81.** No auth, no rate limiting, under 5 s per family.
`provider-upjet-azuread` and `provider-upjet-github` have **no** `cmd/provider/` directory — that is the correct discriminator between a family and a single-package provider.

**Cheap existence oracle (1 request, no auth) — VERIFIED:**
```
GET https://xpkg.upbound.io/service/token?scope=repository:<repo>:pull&service=xpkg.upbound.io
  existing repo    → HTTP 200 {"token":…,"access_token":…,"expires_in":…,"issued_at":…}
  nonexistent repo → HTTP 404
```
A manifest GET then distinguishes tag-missing (404) from repo-missing (401, because no token was issued).

---

## 3. Versions: GitHub tags vs OCI tags

### 3.1 The negative result on `tags/list` is overturned — VERIFIED

The registry has a standard token-auth challenge that must be answered:

```
GET https://xpkg.upbound.io/v2/
→ HTTP 401
   docker-distribution-api-version: registry/2.0
   www-authenticate: Bearer realm="https://xpkg.upbound.io/service/token",service="xpkg.upbound.io"
```

Note the realm is `/service/token`, **not** the conventional `/token`. Hitting `/token` returns an empty body → empty token → every subsequent call 401s, and a naive client reports "no tags". That is almost certainly the origin of the earlier negative result.

With the token:
```
GET https://xpkg.upbound.io/v2/upbound/provider-aws-sqs/tags/list
Authorization: Bearer <anonymous token>
→ HTTP 200, no Link header, 446 tags in one response (~30 KB)
   344 are cosign sha256-….sig / .att / .sbom
   102 are real versions
```
```json
{"name":"upbound/provider-aws-sqs","tags":[
 "sha256-02b68b5…att","sha256-02b68b5…sig","sha256-0601efd…sbom", … ,
 "v0.36.0","v0.37.0","v1.0.0", … ,"v2.7.0","v2.7.1","v1","v2"]}
```

**The pagination trap — VERIFIED.** Passing `?n=50` returns a page that is 100 % cosign tags, because `sha256-…` sorts before `v…`:
```
GET …/tags/list?n=50
link: </v2/upbound/provider-aws-sqs/tags/list?last=sha256-1c8b594….att&n=50>; rel="next"
{"n":50,"first":"sha256-02b68b5….att","last":"sha256-1c8b594….att","anyv":[]}
```
So: **do not paginate; omit `n` and take the whole list.** And unauthenticated it is a clean 401, never an empty list:
```json
{"errors":[{"code":"UNAUTHORIZED","message":"authentication required",
  "detail":[{"Type":"repository","Name":"upbound/provider-aws-sqs","Action":"pull"}]}]}
```

`GET /v2/_catalog` → **HTTP 401** even with a repo-scoped token. There is no registry-wide enumeration; you still need a name source.

### 3.2 GitHub tags ≠ OCI tags, in both directions — VERIFIED, 4 providers

**`crossplane-contrib/provider-upjet-aws` → `upbound/provider-aws-sqs`**
GitHub 152 tags, OCI 102 version tags, intersection 82.

*In OCI, absent from GitHub (20):*
`v1` `v2` (floating channel tags that exist only in the registry) ·
`v1.17.4` `v1.21.5` `v1.23.1` `v1.23.2` `v2.0.1` `v2.0.2` `v2.5.1` `v2.5.2` `v2.5.3` `v2.5.4` `v2.5.5` `v2.6.1` `v2.6.2` `v2.6.3` **`v2.7.1`** — real patch releases shipped straight to the registry with no Git tag.

*In GitHub, absent from OCI (70):* every `-rc.0`, plus real releases `v1.21.1` `v1.22.0` `v1.23.0` where this service package was not rebuilt, plus all `v0.17.0`–`v0.35.0` (predating the family split).

The same comparison against `upbound/provider-family-aws` (180 OCI version tags) exposes another whole tag genus that never touches Git: **98 OCI-only tags** including `v2.7.0-cve`, `v2.5.5-cve` … (a full parallel `-cve` rebuild line), `v2.6.0-1533af9f9678` (commit-suffixed), `v2.7.1-dev-5f240214a4`, `v1.21.5-cve`, `v0.0.0-community.uxp.7.g88f57be13`.

**The decisive check:**
```
gh api repos/crossplane-contrib/provider-upjet-aws/releases/latest → "v2.7.0"
gh api …/git/ref/tags/v2.7.1                                      → HTTP 404
gh api …/releases/tags/v2.7.1                                     → HTTP 404
latest semver across ALL 177 AWS family packages in the registry  → v2.7.1  (177/177 agree)
```
GitHub says the latest is v2.7.0. The registry ships v2.7.1 for every one of the 177 packages. **A GitHub-release-driven version list is factually wrong today, for the single most-used provider.**

**`crossplane-contrib/provider-kubernetes`** — GitHub 56 tags, OCI 96, intersection **27**.
GitHub-only includes shipped releases `v0.16.0` `v0.16.1` `v0.16.2` `v0.16.3` `v0.14.2` `v0.14.3` `v0.14.4` `v0.15.2` `v0.15.3`. OCI-only is 69 `-rc0.N.g<sha>` dev builds.

**`crossplane-contrib/provider-helm`** — GitHub 71 tags, OCI 52, intersection **22**.
GitHub-only includes **`v0.19.1` `v0.19.2` `v0.19.3` `v0.20.0` `v0.20.1` `v0.20.2` `v0.20.3`** — I could not find those anywhere: not in `xpkg.upbound.io` (404), not in `ghcr.io`/`xpkg.crossplane.io` (404), not on Docker Hub (`crossplanecontrib/provider-helm` has 43 tags, none of them these). **Seven GitHub releases with no pullable package.** Offering them in a picker produces a guaranteed pull failure.

**`crossplane-contrib/provider-keycloak`** — GitHub 88 tags, OCI **1039** version tags, intersection 86.
Nearly every merge to `main` publishes a `vX.Y.Z-N.g<sha>` package. Any UI must filter to strict semver or it drowns.

### 3.3 Same provider, different tag sets per registry — VERIFIED

`provider-helm` is published to two registries with **different** contents:
```
xpkg.upbound.io/crossplane-contrib/provider-helm → 52 version tags (v0.12.0 … v1.4.0)
ghcr.io/crossplane-contrib/provider-helm         → 10 version tags (v1.x only)
```
So "which versions exist" is registry-dependent, not provider-dependent.

### 3.4 `xpkg.crossplane.io` is an alias for `ghcr.io` — VERIFIED (new)

```
GET https://xpkg.crossplane.io/v2/
→ HTTP 401
   www-authenticate: Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:user/image:pull"
```
`https://xpkg.crossplane.io/service/token?...` → **HTTP 404**; the token must be minted at `ghcr.io/token`. A ghcr-issued token then works verbatim against `xpkg.crossplane.io` (manifest GET → 200, verified for talos, sonarqube, helm). Treat `xpkg.crossplane.io/X` and `ghcr.io/X` as the same repository, mint at ghcr.

### 3.5 Rate limits on the registries — VERIFIED

60 sequential anonymous token requests to `xpkg.upbound.io`: **60× HTTP 200, 17 s total, zero rate-limit headers** (only `x-envoy-upstream-service-time`). 177 parallel existence checks completed in 4.6 s with no throttling. The registry, not GitHub, is the right hot path.

---

## 4. Rate limits

### 4.1 Unauthenticated GitHub REST — VERIFIED, 60/hr per IP

```
GET https://api.github.com/orgs/crossplane-contrib/repos?per_page=100&page=1
HTTP/2 200
x-ratelimit-limit: 60
x-ratelimit-remaining: 47
x-ratelimit-used: 13
x-ratelimit-resource: core
x-ratelimit-reset: 1787869192
x-github-api-version-selected: 2022-11-28
link: <…page=2>; rel="next", <…page=2>; rel="last"
```
```json
{"core":{"limit":60,"remaining":47,"reset":1787869192,"used":13},
 "search":{"limit":10,"remaining":10,"reset":1787866650,"used":0},
 "code_search":{"limit":60,"remaining":47,"reset":1787869192,"used":13}}
```
`/rate_limit` itself does not consume budget.

**Exhaustion returns 403, not 429 — VERIFIED (I hit it doing this research):**
```
HTTP/2 403
x-ratelimit-limit: 60
x-ratelimit-remaining: 0
x-ratelimit-used: 60
x-ratelimit-reset: 1787869192
{"message":"API rate limit exceeded for 85.253.83.87. (But here's the good news:
 Authenticated requests get a higher rate limit. …)"}
```
Reset was 1833 s away — the window is a fixed hourly bucket, not a sliding one, so a burst can lock a user out for 30 minutes. **Any error handler that only checks for 429 will misreport this as a permissions failure.** Parse `x-ratelimit-remaining` / `x-ratelimit-reset` and surface "GitHub discovery unavailable for N minutes, falling back to cache."

Authenticated (classic PAT, verified): `core 5000/hr`, `graphql 5000/hr`, `search 30/min`.
GitHub also applies undocumented **secondary** limits (concurrency, points/minute) — DOCS: max 100 concurrent requests, ~900 points/min for REST; my 16-way parallelism never tripped it.

### 4.2 The escape hatch: `raw.githubusercontent.com` — VERIFIED

```
before: {"limit":60,"remaining":25,"used":35}
… 25× GET https://raw.githubusercontent.com/crossplane-contrib/provider-helm/main/Makefile …
after:  {"limit":60,"remaining":25,"used":35}      ← delta 0
```
```
HTTP/2 200
cache-control: max-age=300
via: 1.1 varnish
x-served-by: cache-hel1410033-HEL
x-fastly-request-id: d7c72a1d…
```
**Fastly-fronted, 5-minute TTL, no auth, no `x-ratelimit-*` headers, does not touch the API budget.** Every Makefile / `crossplane.yaml` / README / workflow read in this brief went through it — hundreds of requests, zero budget consumed. The only genuine API cost is the org listing (2 pages for contrib, 3 for upbound) plus `contents/cmd/provider` per family (3 calls; and even that could be replaced by `raw` probing or the `git/trees` API).

**So the whole discovery pass costs ~5–8 API requests, well inside 60/hr.** A user-supplied token is **not required**. Design it as: unauthenticated by default; if `GITHUB_TOKEN`/`GH_TOKEN` is in the environment, use it silently for headroom. Prompting for a token to browse a public list is bad UX and would be a real adoption tax for a "just run the binary" tool.

Conditional requests: DOCS say a `304 Not Modified` from `If-None-Match` does not count against the limit. My attempt to verify was inconclusive (the weak ETag `W/"0d40…"` still returned 200), so **DOCS, not verified** — but ETags are worth implementing for the org listing regardless.

**GitHub Packages API requires auth even for public packages — VERIFIED:**
```
GET https://api.github.com/orgs/crossplane-contrib/packages?package_type=container
  unauthenticated → HTTP 401 {"message":"Requires authentication"}
  authenticated   → HTTP 200, 439 container packages (409 provider-*)
```
Shame, because it is otherwise an excellent catalogue: it lists `provider-talos`, `provider-sonarqube`, and all 82 GCP family packages by name with `visibility` and `updated_at`. Unusable for an unauthenticated tool.

### 4.3 Terms of use — DOCS

- GitHub's Acceptable Use Policies define scraping as extraction "via an automated process, such as a bot or webcrawler" and state explicitly that **"Scraping does not refer to the collection of information through our API."** REST API use inside the published rate limits is sanctioned, not scraping. Prohibited is "excessive automated bulk activity" — a per-user discovery refresh is nowhere near that.
- API Terms live in Section H of the GitHub ToS; the operative constraints are rate limits and not reselling personal information. Provider metadata is not personal information.
- Repository content licences — VERIFIED: **49/50 active `crossplane-contrib/provider-*` are Apache-2.0** (1 has no licence file), **11/11 `upbound/provider-*` are Apache-2.0**. Reading and redistributing their Makefile-derived metadata is fine with attribution.
- `raw.githubusercontent.com` has no separate published terms; it is covered by the same AUP. It is a CDN for repo content, not an API, so heavy use is more exposed to the "excessive bulk" clause than the API is — cache aggressively (the 5-min `max-age` is a hint) and never hammer it in a loop.

---

## 5. Curated lists: are any of them better than scraping?

### 5.1 Crossplane docs "Community Extension Projects" — VERIFIED, and it is *the same scrape*

`https://docs.crossplane.io/latest/learn/community-extension-projects/`
Source: `crossplane/docs` → `content/master/learn/community-extension-projects.md` (8.1 KB, 85 list entries).

The Crossplane docs team ships the generator alongside it: **`crossplane/docs/scripts/discover-community-extensions.sh`** (47 lines). Its entire body is:

```bash
gh api --paginate "orgs/crossplane-contrib/repos?type=public&per_page=100" \
  | jq -s 'add | map(select(.archived == false))
           | map({name, url: .html_url, description, topics: (.topics//[]), fork})
           | sort_by(.name)'
```

That is exactly the approach in §1.1 — the official docs list *is* a scrape of `crossplane-contrib`, with an LLM skill (`.claude/skills/community-extensions-update/`) doing the categorisation.

**Machine-readability: poor.** The published artefact is Hugo Markdown bullets, not JSON:
```markdown
## Providers
- [provider-upjet-alibabacloud](https://github.com/crossplane-contrib/provider-upjet-alibabacloud)
- [provider-ansible](https://github.com/crossplane-contrib/provider-ansible)
```
**It contains zero install references** — `grep -cE 'xpkg\.|ghcr\.io' → 0`. It cannot tell you what to pull.

**Freshness: ~4 months stale.** Last touched `2026-04-29 "docs: refresh community extension projects page"`. Drift vs a live API call today is exactly two entries: it still lists `provider-jet-rancher` (archived 2022-08-03) and is missing `provider-checkly`.

**It also lists repos that cannot be installed**: `provider-capi` (0 releases, last push 2020), `provider-dynatrace` (0 releases), `provider-upjet-cloudflare` (0 releases), `provider-jet-ec` (0 releases), `provider-upjet-edgeadc` (0 releases).

Verdict: **no advantage over calling the API yourself**, and it is staler. Its one genuine contribution is the Providers/Functions/Tools *categorisation*, which the `package/crossplane.yaml` `kind:` probe reproduces more accurately.

### 5.2 `awesome-crossplane` — stale, VERIFIED

```
crossplane-contrib/awesome-crossplane → HTTP 404
crossplane/awesome-crossplane         → HTTP 404
```
No canonical one exists. Search results:

| repo | ★ | last push | age |
|---|---:|---|---|
| DevOpsHiveHQ/awesome-crossplane | 15 | 2024-04-17 | **2 yr 4 mo** |
| luebken/awesome-crossplane-providers | 20 | 2023-06-06 | **3 yr 3 mo** |
| web-seven/awesome-crossplane | 2 | 2024-10-11 | 1 yr 10 mo |
| awesome-crossplane-providers/… | 0 | 2022-03-19 | 4 yr 5 mo |

All unmaintained, all Markdown prose, none carries OCI refs. `luebken/awesome-crossplane-providers` is conceptually the closest prior art ("Queries Github to find Awesome Crossplane Providers") and it has been dead for three years — a useful data point on how much upkeep this problem needs.

### 5.3 `crossplane-contrib/.github` profile README — does not exist, VERIFIED

```
GET https://api.github.com/repos/crossplane-contrib/.github/contents → HTTP 404
GET https://raw.githubusercontent.com/crossplane-contrib/.github/main/profile/README.md → HTTP 404
```

### 5.4 An `index.yaml`-style artefact — none found

No Helm-style index, no `catalog.json`, no OCI referrer index. `GET /v2/_catalog` on `xpkg.upbound.io` → 401. The nearest machine-readable catalogue is the GitHub Packages API (§4.2), which needs auth.

---

## 6. Beyond the two orgs: third-party providers

Actively-maintained providers live outside both orgs and **none of them follow the `provider-*` naming**:

```
oracle/crossplane-provider-oci             61★  2026-08-27
grafana/crossplane-provider-grafana        48★  2026-08-26
yandex-cloud/crossplane-provider-yc        43★  2026-08-27
scaleway/crossplane-provider-scaleway      30★  2026-08-27
SAP/crossplane-provider-btp                26★  2026-08-27
valkiriaaquatica/provider-proxmox-bpg      26★  2026-08-26
```

Running the same chain over them — VERIFIED:

| repo | source | resolved ref | tags |
|---|---|---|---|
| grafana/crossplane-provider-grafana | makefile | `xpkg.upbound.io/grafana/provider-grafana` | 438 |
| scaleway/crossplane-provider-scaleway | makefile | `xpkg.upbound.io/scaleway/provider-scaleway` | 188 |
| SAP/crossplane-provider-btp | makefile | `ghcr.io/sap/crossplane-provider-btp/crossplane/provider-btp` | 77 |
| valkiriaaquatica/provider-proxmox-bpg | makefile | `xpkg.upbound.io/valkiriaaquaticamendi/provider-proxmox-bpg` | 234 |
| yandex-cloud/crossplane-provider-yc | **readme** | `xpkg.upbound.io/yandexcloud/crossplane-provider-yc` | 13 (Makefile default `upbound/crossplane-provider-yc` → 404) |
| oracle/crossplane-provider-oci | **none** | — | README has no install ref, releases have no assets |

5/6 — the same ~83 % as the orgs. Note SAP's four-segment ghcr path and yandex's org name differing by a hyphen (`yandex-cloud` vs `yandexcloud`); both would defeat any naming heuristic.

Finding them at all requires **authenticated** search:
```
GET /search/code?q="kind: Provider"+filename:crossplane.yaml+path:package
→ total_count 459   (code search API requires auth; unauth search core is 10 req/min)
GET /search/repositories?q=topic:crossplane-provider → total_count 71
```

---

## 7. Verdict

**GitHub is a useful supplement and a trap as a primary catalogue. The registry is the primary; GitHub supplies the human layer.**

**What GitHub is genuinely good for**
- The *name seed*: which providers exist under an org. Two API calls, no auth, complete, live.
- The *repo→OCI hypothesis*: `XPKG_REG_ORGS`/`XPKGS` from the Makefile, correct for 46/60, free via `raw.githubusercontent.com`.
- The *family expansion*: `ls cmd/provider/` → 177/177 verified for AWS. **Nothing else can produce these names**, and they are ~350 of the most-wanted packages.
- The *human metadata* a registry cannot give: stars, description, archived, last-push, licence. `archived == true` and `pushed_at < 2024` are the signals that keep 37 dead providers out of a picker, and no OCI registry exposes them.

**Where it is a trap**
1. **~350 family sub-packages have no GitHub repo.** `upbound/provider-aws-sqs` → 404. Repo enumeration alone hides the single most likely thing the user wants.
2. **The version list is wrong.** GitHub's latest AWS release is v2.7.0; the registry ships v2.7.1 with no Git tag. provider-helm has 7 GitHub releases with no pullable package anywhere. provider-kubernetes/GitHub-tag intersection with OCI is 27/56. Channel tags (`v1`, `v2` — the form in the user's own example) exist *only* in the registry.
3. **The mapping is not statically decidable.** provider-sonarqube's publish target is a GitHub Actions expression keyed on a secret's presence. No amount of repo parsing resolves that.
4. **The rate limit is a cliff, not a slope.** 60/hr per IP, fixed hourly bucket, 403 on exhaustion. I burned the whole budget doing this one investigation. Per-provider API calls are not viable.
5. **Nothing curated is fresh.** The official docs list is 4 months stale and carries no install refs; every `awesome-crossplane` is 2–4 years dead.
6. **Naming heuristics fail at both ends.** `provider-workflows` is not a provider; `crossplane-provider-castai`, `crossplane-provider-newrelic`, `oracle/crossplane-provider-oci` are. Topics cover 16/85 repos and misfire.

**Recommended shape**

1. **Ship a baked-in seed catalogue** generated offline by this pipeline (org listing → Makefile/crossplane.yaml resolution → registry validation → family expansion), committed as JSON in the binary. It works at first run, offline, with zero network. This is where the ~4-months-stale problem gets solved: your build refreshes it, not the user's laptop.
2. **Refresh online opportunistically, never as a dependency.** Two unauthenticated API calls for the org listing; all file reads via `raw.githubusercontent.com`; degrade to the seed + local cache on any 403/timeout.
3. **Get versions exclusively from the registry** — `GET /v2/<repo>/tags/list` with an anonymous bearer from the `www-authenticate` realm, no `?n=`, filtering out `sha256-*` and (by default) `-rc`/`-cve`/`.g<sha>` suffixed builds. Show floating `v1`/`v2` channel tags, since that is what users type.
4. **Never surface an unvalidated ref.** One token-endpoint request per candidate (~250 ms, no auth, no rate limit) turns an 82 % guess into a 100 % correct catalogue with an honest "publishes no package" state for the other 18 %.
5. **Use GitHub metadata purely for ranking and warnings** in the picker: stars, last push, and an explicit "archived — no longer maintained" badge for the 37 dead contrib providers.

### Endpoint summary

| endpoint | auth | status | rate limit | verdict |
|---|---|---|---|---|
| `api.github.com/orgs/{org}/repos` | none | 200 | 60/hr core, 403 on exhaust | ✅ name seed, ~2–3 calls |
| `api.github.com/repos/{r}/tags` | none | 200 | same | ❌ wrong version list |
| `api.github.com/repos/{r}/releases` | none | 200 | same | ❌ wrong; assets 1/60 |
| `api.github.com/repos/{r}/contents/cmd/provider` | none | 200 | same | ✅ family expansion |
| `api.github.com/orgs/{o}/packages?package_type=container` | **required** | 401→200 | 5000/hr | ❌ auth-gated |
| `api.github.com/search/code` | **required** | 200 | 30/min | ⚠️ third-party discovery only |
| `raw.githubusercontent.com/{r}/{br}/{path}` | none | 200 | **none, budget-free** | ✅✅ all file reads |
| `xpkg.upbound.io/service/token?scope=…:pull` | none | 200 / 404 | none observed | ✅✅ existence oracle |
| `xpkg.upbound.io/v2/{repo}/tags/list` | bearer | 200 | none observed | ✅✅ **version source** |
| `xpkg.upbound.io/v2/_catalog` | bearer | 401 | — | ❌ no enumeration |
| `xpkg.crossplane.io/v2/…` | ghcr bearer | 200 | — | ✅ alias of ghcr.io |
| `ghcr.io/token` + `/v2/…/tags/list` | none | 200 / 403 | none observed | ✅ 6 contrib providers |
| `docs.crossplane.io` community list | none | 200 | — | ❌ stale, no refs |
