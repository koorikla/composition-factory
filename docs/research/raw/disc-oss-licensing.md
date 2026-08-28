# Discovery: OSS vs Proprietary in the Crossplane provider landscape

Research for **compositionfactory** provider catalogue. Date of investigation: 2026-08-28.
Method markers: **[VERIFIED]** = I executed it against the live registry/API/cluster and am reporting observed output. **[DOCS]** = vendor documentation or legal text, quoted, not independently confirmed. **[JUDGEMENT]** = my inference, flagged as such.

Legal-adjacent. Licences and terms are quoted verbatim with URLs. I am not a lawyer and nothing here is legal advice.

---

## Decisions this enables

1. **Do not treat "published by Upbound" as "proprietary".** The user's own `upbound/provider-aws-sqs` declares `meta.crossplane.io/license: Apache-2.0` and names `github.com/crossplane-contrib/provider-upjet-aws` as its source, in-package. **[VERIFIED]** The user is already running Apache-2.0-sourced software. A naive "block xpkg.upbound.io" rule would be factually wrong.

2. **The OSS equivalent is not an alternative — it is the same build.** At matching version v2.4.0, all 8 CRDs in `xpkg.upbound.io/upbound/provider-aws-sqs` and `xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs` are **byte-identical** (SHA-256 per CRD document). The entire package delta is **17 diff lines**: 10 lines of Upbound support/marketing annotations plus the `dependsOn` registry and version pin. **[VERIFIED]**

3. **Q6 answered: no friction. Contrib is NOT v1-only.** `crossplane-contrib/provider-aws-sqs` ships `sqs.aws.m.upbound.io` **Namespaced** CRDs (`Queue`, `QueuePolicy`, `QueueRedrivePolicy`, `QueueRedriveAllowPolicy`, all `v1beta1`) and the family ships `aws.m.upbound.io` `ProviderConfig`/`ClusterProviderConfig`. Identical to Upbound's. Everything validated against `sqs.aws.m.upbound.io` keeps working verbatim. **[VERIFIED]**

4. **OCI labels are useless for licence detection; the Crossplane package meta is the real signal, with ~75% coverage.** No `org.opencontainers.image.licenses` or `.source` on any image I inspected, Upbound or contrib, and no manifest-level annotations at all. `meta.crossplane.io/license` was present on 9 of 12 packages probed. Artifact Hub does not index Crossplane packages at all. GitHub API is the fallback and recovered all 3 gaps correctly. **[VERIFIED]**

5. **Recommended policy: label, don't hide.** Default catalogue = `crossplane-contrib` + `crossplane` + curated vendor orgs, with a machine-derived licence badge and a separate `Commercial build` badge driven by `meta.upbound.io/verification: Official`. Upbound Official entries stay visible and clearly marked, with a one-click "switch to the OSS build of the same source" action — which is a genuine no-op for the user's stack. Hiding them would hide the two providers the user actually runs.

---

## 0. The user's actual cluster state (read-only, nothing modified)

```
$ kubectl get providers.pkg.crossplane.io
NAME                          PACKAGE                                              INSTALLED  HEALTHY
provider-aws-sqs              xpkg.upbound.io/upbound/provider-aws-sqs:v2          True       True
upbound-provider-family-aws   xpkg.upbound.io/upbound/provider-family-aws:v2.4.0   True       True

$ kubectl get deploy -A -l app=crossplane
crossplane-system  crossplane  xpkg.crossplane.io/crossplane/crossplane:v2.4.0
```
**[VERIFIED]**

Two notes that matter downstream:

- Crossplane core itself is already pulled from `xpkg.crossplane.io` — the OSS registry. Only the two AWS providers come from `xpkg.upbound.io/upbound/`.
- **The `:v2` floating tag currently resolves to v2.4.0, not to the newest release.** `crane digest` gives `sha256:e3aaedccfcc3022bed7763fb3f5a48b4ce5ae915e6dc5b2032688cb06f8aaf11` for both `:v2` and `:v2.4.0`, while `:v2.7.1` is a different digest. **[VERIFIED]** So the user's two providers are version-consistent at v2.4.0, but `:v2` is not tracking latest. Worth surfacing in the tool as a "floating tag, currently behind" warning.

13 `*.upbound.io` CRDs are established in the cluster (8 sqs + 5 family), exactly matching what the packages ship. **[VERIFIED]**

---

## 1. The actual licence of the providers the user is already running

### 1.1 The source repo

`github.com/upbound/provider-aws` **is** `github.com/crossplane-contrib/provider-upjet-aws`. The repo was transferred/renamed; GitHub's API follows the rename and returns the new identity:

```
$ curl -sL https://api.github.com/repos/upbound/provider-aws | jq '{full_name, fork, parent, license}'
{
  "full_name": "crossplane-contrib/provider-upjet-aws",
  "fork": false,
  "parent": null,
  "license": { "key": "apache-2.0", "name": "Apache License 2.0", "spdx_id": "Apache-2.0", ... }
}
```
**[VERIFIED]** Same for `upbound/provider-gcp` → `crossplane-contrib/provider-upjet-gcp` and `upbound/provider-azure` → `crossplane-contrib/provider-upjet-azure`. `fork: false` and `parent: null` confirm this is a rename, **not a fork** — there is no separate upstream.

There is no separate closed source tree for the AWS family. The package itself says so — see 1.3.

### 1.2 The LICENSE file, verbatim

- **Name:** Apache License, Version 2.0
- **SPDX id:** `Apache-2.0`
- **URL:** https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/LICENSE
- **Raw:** https://raw.githubusercontent.com/crossplane-contrib/provider-upjet-aws/main/LICENSE

Opening text, verbatim:

> Apache License
> Version 2.0, January 2004
> <http://www.apache.org/licenses/>
>
> TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION
>
> 1. Definitions.
>
> "License" shall mean the terms and conditions for use, reproduction, and distribution as defined by Sections 1 through 9 of this document.
>
> "Licensor" shall mean the copyright owner or entity authorized by the copyright owner that is granting the License.

**[VERIFIED]** 73 lines, SHA-256 `6cf5ac83bdef379bb5116970e50718d8c6a0a259f8f50622bfb8f78891230037`.

Detail worth recording: this file is a **Markdown-reflowed rendition** of Apache-2.0, not the canonical plaintext from apache.org (whose SHA-256 is `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30`). The terms are Apache-2.0; only the whitespace/formatting differs. **A byte-hash comparison against the canonical text is therefore NOT a valid licence-detection method** — this would produce a false "unknown" on a correctly-licensed repo. Use GitHub's licence classifier or an SPDX matcher that normalises whitespace. **[VERIFIED]**

`provider-family-aws` is built from the same repo (it is a subpackage of the same build), so the same LICENSE applies.

### 1.3 What the package itself declares — the strongest evidence

Extracted from the actual image the user is running, `xpkg.upbound.io/upbound/provider-aws-sqs:v2`. This is the complete `meta.pkg.crossplane.io/v1 Provider` object, verbatim, every field:

```yaml
apiVersion: meta.pkg.crossplane.io/v1
kind: Provider
metadata:
  annotations:
    auth.upbound.io/group: aws.upbound.io
    friendly-name.meta.crossplane.io: Provider AWS (sqs)
    meta.crossplane.io/description: |
      Upbound's official Crossplane provider to manage Amazon Web Services (AWS)
      sqs services in Kubernetes.
    meta.crossplane.io/license: Apache-2.0
    meta.crossplane.io/maintainer: Upbound <support@upbound.io>
    meta.crossplane.io/readme: |2

      Provider AWS is a Crossplane provider for [Amazon Web Services
      (AWS)](https://aws.amazon.com/) developed and supported by Upbound.
      Available resources and their fields can be found in the [Upbound
      Marketplace](https://marketplace.upbound.io/providers/upbound/provider-aws).
      If you encounter an issue please reach out on support@upbound.io email
      address. This is a subpackage for the sqs API group.
    meta.crossplane.io/source: github.com/crossplane-contrib/provider-upjet-aws
    meta.upbound.io/hardening: |
      - CVE Remediation
      - Backporting
      - FIPS Compatibility
    meta.upbound.io/host: |
      - XP
      - UXP
      - Spaces
    meta.upbound.io/support: Upbound
    meta.upbound.io/verification: Official
  creationTimestamp: null
  labels:
    pkg.crossplane.io/provider-family: provider-family-aws
  name: provider-aws-sqs
spec:
  capabilities:
  - SafeStart
  crossplane:
    version: '>=v1.12.1-0'
  dependsOn:
  - provider: xpkg.upbound.io/upbound/provider-family-aws
    version: v2.4.0
```
**[VERIFIED]**

**Upbound's own Official package declares `license: Apache-2.0` and points `source` at `crossplane-contrib`.** That is the single most important fact in this brief.

### 1.4 NOTICE — the MPL-2.0 dependency

The repo ships a NOTICE alongside LICENSE. Verbatim opening:

> This project is a larger work that combines with software written by third
> parties, licensed under their own terms.
>
> Notably, this larger work combines with the following Terraform components,
> which are licensed under the Mozilla Public License 2.0 (see
> <https://www.mozilla.org/en-US/MPL/2.0/> or the individual projects listed
> below).
> <https://github.com/hashicorp/terraform-provider-aws>
> <https://github.com/hashicorp/terraform>
> ...

**[VERIFIED]** (https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/NOTICE)

So the honest licence string for the *shipped artifact* is not simply "Apache-2.0" — it is **Apache-2.0 with bundled MPL-2.0 components**. MPL-2.0 is OSI-approved and file-level copyleft; it does not infect the tool. But a catalogue that prints a bare "Apache-2.0" badge is slightly overstating. **[JUDGEMENT]** Recommendation in §5: badge the declared licence, and surface NOTICE presence as a "bundled third-party components" note rather than trying to compute a composite SPDX expression.

Also note the NOTICE file itself is `SPDX-License-Identifier: CC0-1.0` — an SPDX scanner pointed at the repo root will see two identifiers. Read the top-level LICENSE, not every SPDX tag.

### 1.5 Is the *image* distributed under different terms from the *source*?

This is the crux of the user's question, and the answer is: **Upbound asserts that it is, for their build.**

Upbound's provider documentation, verbatim:

> "Official Providers are commercially licensed builds of Crossplane providers."

> Official Providers "may be closed source, or they may be downstream from open source (such as the case for provider-family-aws)"

**[DOCS]** — https://docs.upbound.io/manuals/packages/providers/

That second quote is decisive and directly on point: Upbound explicitly names `provider-family-aws` — one of the two packages the user runs — as the **downstream-from-open-source** case, not the closed-source case.

The FAQ draws the community/official line:

> "The community providers are free-use images built and published by the Crossplane community. Official Providers are built and published by Upbound, and have a commercial license."

**[DOCS]** — https://docs.upbound.io/manuals/packages/providers/faq/

And the Upbound Software License Agreement (the EULA that Upbound applies to "Upbound Software"), https://licenses.upbound.io/upbound-software-license.html, verbatim:

> **Section 1 — SCOPE OF LICENSE:** "This license applies to the Upbound software and any related updates, components, and organization (the 'Product')"

> **Section 1:** "You may not: reverse engineer, decompile or disassemble the software, or otherwise attempt to derive the source code for the software except and solely to the extent required by third-party licensing terms governing use of certain open source components"

> **Section 1:** "You may not share, publish, rent or lease the software, or provide the software as a stand-alone offering for others to use."

> **Section 2.3 — Third-Party Components:** "The software may include third-party components with separate legal notices or governed by other agreements, as may be described in the Third Party Notices file accompanying the software."

> **Section 5 — INTELLECTUAL PROPERTY RIGHTS:** "You may make a commercial redistribution of the Products only if permitted under a separate written agreement with Upbound"

**[DOCS]**

Upbound's general Terms & Conditions (https://www.upbound.io/terms-conditions) contains **no** clause specific to the marketplace registry, to pulling packages, or to anonymous use. The only adjacent text is a SaaS-module clause requiring open source software be "specifically identified in the Order Form and agreed to by Customer," and a free/trial account termination right in §1.2. **[DOCS]** I could not find a registry-specific EULA or click-through gate on `xpkg.upbound.io` itself. **[VERIFIED — absence of a gate; see §2.2]**

**How to read this, precisely:**

- The **source** is unambiguously Apache-2.0. Not disputed by anyone, including Upbound's own package metadata.
- The **Upbound-built binary artifact** is one Upbound describes as commercially licensed, and their EULA §1 restricts redistribution and providing it as a stand-alone offering.
- EULA §1 and §2.3 both carve out open source components as governed by their own terms.
- **[JUDGEMENT]** There is genuine tension here: Apache-2.0 §4 grants any recipient the right to redistribute the work in Object form, and a downstream distributor generally cannot retract that grant on the Apache-2.0-covered code itself. What Upbound can restrict is their trademarks, their support relationship, and any non-Apache material they add. Which body of terms governs a given anonymous `docker pull` of `upbound/provider-aws-sqs` is **not something I can resolve from public documents, and not something this tool should assert.**

**The practical consequence for compositionfactory is that this ambiguity is entirely avoidable** — see §3. The identical build is available from `crossplane-contrib` with zero legal ambiguity, and switching is a no-op.

---

## 2. What is genuinely proprietary in the Crossplane ecosystem

### 2.1 Official vs community — the machine-detectable boundary

Comparing the meta objects of `upbound/provider-aws-sqs:v2.4.0` against `crossplane-contrib/provider-aws-sqs:v2.4.0`, the **complete** difference is:

```diff
-     meta.upbound.io/hardening: |
-       - CVE Remediation
-       - Backporting
-       - FIPS Compatibility
-     meta.upbound.io/host: |
-       - XP
-       - UXP
-       - Spaces
-     meta.upbound.io/support: Upbound
-     meta.upbound.io/verification: Official
    ...
-   - provider: xpkg.upbound.io/upbound/provider-family-aws
-     version: v2.4.0
+   - provider: xpkg.crossplane.io/crossplane-contrib/provider-family-aws
+     version: '>= 0.0.0'
```
17 diff lines total, across the entire package. **[VERIFIED]**

`meta.upbound.io/verification: Official` was present on every `xpkg.upbound.io/upbound/*` package I probed and on **zero** `crossplane-contrib` packages. **[VERIFIED]** This is the reliable programmatic flag for "this is a vendor commercial build."

What Upbound sells is therefore **the build and the support wrapper** — CVE remediation, backporting, FIPS compatibility, long-term support — not different functionality. The CRDs are identical (§3.3).

### 2.2 Is authentication actually enforced? Tested, and: NO

Documentation says it is:

> "You need an account on Upbound to pull an Official Provider." **[DOCS]** — docs.upbound.io/manuals/packages/providers/

> "Yes, however, it's only the latest release that can be pulled anonymously. When a new release _N_ is published, access is cut off from the _N-1_ for anonymous and Individual tier users." **[DOCS]** — the providers FAQ

I tested this with a **clean empty Docker config** (`DOCKER_CONFIG` pointed at a directory containing only `{}`), so no ambient credentials.

**Manifest access across versions of `xpkg.upbound.io/upbound/provider-aws-sqs`:**

| Tag | Anonymous result |
|---|---|
| v2.4.0 | **OK** |
| v2.3.0 | **OK** |
| v2.2.0 | **OK** |
| v2.1.0 | **OK** |
| v2.0.0 | **OK** |
| v1.21.0 | **OK** |
| v1.17.0 | **OK** |

**[VERIFIED]** Latest published tag is v2.7.1, so v1.17.0 is many releases behind N-1 and should have been cut off per the FAQ. It was not.

**Full blob (not just manifest) anonymous pulls — 3+ candidate Official providers as required:**

| Reference | Anonymous full pull |
|---|---|
| `xpkg.upbound.io/upbound/provider-aws-sqs:v2` | **OK — 272 MB** |
| `xpkg.upbound.io/upbound/provider-family-aws:v2.4.0` | **OK — 272 MB** |
| `xpkg.upbound.io/upbound/provider-gcp-storage:v2.0.0` | **OK — 80 MB** |
| `xpkg.upbound.io/upbound/provider-aws-sqs:v1.17.0` (old) | **OK — 208 MB** |
| `xpkg.upbound.io/upbound/provider-azure-network:v2.0.0` | OK (manifest + config) |

**[VERIFIED]**

The registry issues an anonymous bearer token on request, with no credentials supplied. Decoded JWT payload:

```json
{
  "aud": "xpkg.upbound.io",
  "iss": "xpkg-auth-service",
  "sub": "anonymous",
  "access": [ { "type": "repository", "name": "upbound/provider-aws-sqs", "actions": ["pull"] } ],
  "upb": { "rels": null }
}
```

Using that token, `GET /v2/upbound/provider-aws-sqs/manifests/v1.17.0` returns **HTTP 200**. **[VERIFIED]**

**Conclusion: as of 2026-08-28, `xpkg.upbound.io` grants anonymous `pull` on Official Providers at every version I tested. The documented account requirement is not currently enforced.** This is a *behaviour* observation, and behaviour can change without notice — Upbound has announced enforcement changes before. A catalogue must not treat "the pull succeeded" as evidence of a licence grant, and must not hard-code the assumption that it will keep succeeding.

### 2.3 The three categories, properly separated

The user's framing was right to suspect these get conflated. They are distinct:

| Category | What it means | Concrete examples | Evidence |
|---|---|---|---|
| **Requires payment** | Money, or you cannot use it at all | Upbound Spaces; Upbound SaaS managed control planes; Enterprise/Business Critical support tiers; access to N-1 and older Official Provider versions *as documented* | **[DOCS]** |
| **Requires an account** | Free registration, no money | Upbound Individual tier for Official Providers; documented as required for any Official Provider pull | **[DOCS]** — and **not enforced in practice [VERIFIED]** |
| **Freely pullable but commercially licensed** | Anyone can `docker pull` it; the vendor asserts commercial terms on the artifact | `xpkg.upbound.io/upbound/provider-*` — **this is the category the user's two providers actually fall into** | **[VERIFIED]** pull; **[DOCS]** terms |

This third category is the one that matters and the one that is easiest to get wrong. It is not "proprietary software" in the sense of closed source — the source is Apache-2.0 and Upbound says so themselves. It is an OSS codebase with a vendor's commercial build terms layered on the binary.

### 2.4 Provider images requiring authentication

I found **none** among Crossplane providers. Every reference I tested — Upbound Official, crossplane-contrib on `xpkg.crossplane.io` (GHCR-backed), and crossplane-contrib mirrored on `xpkg.upbound.io` — pulled anonymously. **[VERIFIED]**

Failures I did observe were *absence*, not *auth*, and the error strings distinguish them cleanly — useful for the catalogue's error handling:

- Non-existent repository → `unexpected status code 404 Not Found` from the token service
- Non-existent tag on a real repo → `MANIFEST_UNKNOWN: manifest unknown; unknown tag=...`
- GHCR repo that does not exist under that name → `DENIED: requested access to the resource is denied` (**note: this reads like an auth failure but is actually a missing repo** — `xpkg.crossplane.io/crossplane-contrib/provider-upjet-aws` returns DENIED because contrib publishes the per-service split, not a monolithic `provider-upjet-aws` package)

**[VERIFIED]** That last one is a real trap for a catalogue: **do not report GHCR `DENIED` as "requires authentication."** It usually means the package name is wrong.

### 2.5 UXP, Spaces, Crossplane Enterprise

| Thing | Licence / status | Evidence |
|---|---|---|
| `crossplane/crossplane` (core) | **Apache-2.0**, active (pushed 2026-08-27) | **[VERIFIED]** GitHub API |
| `crossplane/upjet` (codegen framework) | **Apache-2.0**, active | **[VERIFIED]** |
| `upbound/universal-crossplane` (UXP) | **Apache-2.0** — but **ARCHIVED** | **[VERIFIED]** GitHub API `archived: true` |
| Upbound Spaces | **No public source repository.** Only `upbound/spaces-reference-architecture` (Apache-2.0) and `upbound/platform-ref-upbound-spaces` (no licence) exist publicly — these are reference architectures, not the product | **[VERIFIED]** GitHub search of the `upbound` org |
| Upbound SaaS / control planes / support tiers | Commercial, subscription | **[DOCS]** |

**UXP being archived is notable** and should be reflected in the catalogue: it is no longer an active recommendation. Upstream `crossplane/crossplane` is the live Apache-2.0 core, and that is exactly what the user is already running (`xpkg.crossplane.io/crossplane/crossplane:v2.4.0`).

**Upbound Spaces is the genuinely proprietary product in this ecosystem.** It is closed-source and paid. It is also irrelevant to a provider catalogue — it is a control-plane hosting product, not a provider.

---

## 3. The OSS alternatives, mapped

### 3.1 Same codebase, not a fork — and Upbound says so

`crossplane-contrib/provider-upjet-aws` is not a fork of an Upbound repo, and it is not independent. **It IS the repo formerly at `upbound/provider-aws`.** `fork: false`, `parent: null`, and the old URL resolves to it. **[VERIFIED]**

Confirmed from three independent directions:

1. GitHub API rename resolution (§1.1). **[VERIFIED]**
2. Upbound's own Official package declares `meta.crossplane.io/source: github.com/crossplane-contrib/provider-upjet-aws`. **[VERIFIED]**
3. Upbound's FAQ, verbatim:
   > "Use the community-built, free access releases of providers published to `crossplane-contrib`. If you're not interested in a subscription, you should use the new releases of **the same provider source** published under the `crossplane-contrib` org, available on both the Upbound Marketplace (`xpkg.upbound.io`) and `xpkg.crossplane.io`."

   **[DOCS]** — emphasis mine. Upbound is directly recommending the contrib packages to non-subscribers.

The same holds for GCP and Azure: `upbound/provider-gcp` → `crossplane-contrib/provider-upjet-gcp`, `upbound/provider-azure` → `crossplane-contrib/provider-upjet-azure`, both Apache-2.0, both non-forks. **[VERIFIED]**

### 3.2 Who publishes, where, and exact OCI refs

Contrib packages are published to **two** registries, both anonymously pullable:

| Registry | Backing | Anonymous pull |
|---|---|---|
| `xpkg.crossplane.io/crossplane-contrib/*` | **GHCR** (`ghcr.io`, revealed by its token endpoint) | **VERIFIED OK** |
| `xpkg.upbound.io/crossplane-contrib/*` | Upbound Marketplace mirror | **VERIFIED OK** |

Exact refs, all confirmed to exist and pull anonymously **[VERIFIED]**:

```
xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs:v2.7.0
xpkg.crossplane.io/crossplane-contrib/provider-family-aws:v2.7.0
xpkg.crossplane.io/crossplane-contrib/provider-gcp-storage:v3.0.0
xpkg.crossplane.io/crossplane-contrib/provider-azure-network:v2.7.0
xpkg.crossplane.io/crossplane-contrib/provider-kubernetes:v1.3.0
xpkg.crossplane.io/crossplane-contrib/provider-helm:v1.4.0
xpkg.crossplane.io/crossplane-contrib/provider-terraform:v1.2.0

# mirrored, same content:
xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v2.7.0
xpkg.upbound.io/crossplane-contrib/provider-family-aws:v2.7.0
xpkg.upbound.io/crossplane-contrib/provider-helm:v1.4.0
```

The mirror matters for the user specifically: **they can keep the `xpkg.upbound.io` registry host and change only the org path from `upbound` to `crossplane-contrib`.** No new registry to allow-list, no airgap/mirror reconfiguration. **[VERIFIED]**

### 3.3 What would change if the user switched — nothing

**Granularity: contrib ships the SAME per-service split.** This was the thing most worth checking and the answer is unambiguous. Contrib does **not** ship one monolithic provider. Tags on `xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs`:

```
v1.23.0-crossplane-v2-preview.0, v1.23.0, v1.24.0-crossplane-v2-preview.0,
v2.0.0, v2.1.0, v2.1.1, v2.2.0, v2.3.0, v2.4.0, v2.5.0, v2.6.0, v2.7.0
```
**[VERIFIED]** Same package name, same family pattern (`provider-family-aws`), same version line.

**API surface: byte-identical.** Comparing `upbound/provider-aws-sqs:v2` (= v2.4.0) against `crossplane-contrib/provider-aws-sqs:v2.4.0`, SHA-256 of each CRD document:

| CRD | Result |
|---|---|
| `queues.sqs.aws.m.upbound.io` | **IDENTICAL** |
| `queues.sqs.aws.upbound.io` | **IDENTICAL** |
| `queuepolicies.sqs.aws.m.upbound.io` | **IDENTICAL** |
| `queuepolicies.sqs.aws.upbound.io` | **IDENTICAL** |
| `queueredrivepolicies.sqs.aws.m.upbound.io` | **IDENTICAL** |
| `queueredrivepolicies.sqs.aws.upbound.io` | **IDENTICAL** |
| `queueredriveallowpolicies.sqs.aws.m.upbound.io` | **IDENTICAL** |
| `queueredriveallowpolicies.sqs.aws.upbound.io` | **IDENTICAL** |

And the family package:

| CRD | Result |
|---|---|
| `providerconfigs.aws.m.upbound.io` | **IDENTICAL** |
| `clusterproviderconfigs.aws.m.upbound.io` | **IDENTICAL** |
| `providerconfigusages.aws.m.upbound.io` | **IDENTICAL** |
| `providerconfigs.aws.upbound.io` | **IDENTICAL** |
| `providerconfigusages.aws.upbound.io` | **IDENTICAL** |

**[VERIFIED]**

So, itemising the user's question directly:

| Dimension | Change on switching |
|---|---|
| API group | **None.** Still `sqs.aws.m.upbound.io` and `aws.m.upbound.io`. The `.upbound.io` suffix is just the group domain string baked into the generated types — it carries no licensing meaning. |
| Kinds | **None.** `Queue`, `QueuePolicy`, `QueueRedrivePolicy`, `QueueRedriveAllowPolicy`; `ProviderConfig`, `ClusterProviderConfig`, `ProviderConfigUsage`. |
| Versions | **None.** `v1beta1`, served + storage, in both. |
| Scopes | **None.** Namespaced/Cluster split identical. |
| Granularity | **None.** Per-service split in both. |
| Registry | `upbound` → `crossplane-contrib` in the org path. Host can stay `xpkg.upbound.io`. |
| `dependsOn` | Contrib uses `>= 0.0.0` on the family rather than Upbound's exact `v2.4.0` pin. |
| Available versions | Contrib is at v2.7.0; the user's `:v2` tag is at v2.4.0. |

The migration is a two-line edit:

```yaml
# provider-family-aws
package: xpkg.crossplane.io/crossplane-contrib/provider-family-aws:v2.4.0
# provider-aws-sqs
package: xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs:v2.4.0
```

Staying on v2.4.0 keeps the CRDs byte-identical to what is currently established in the cluster — a genuine no-op. **[VERIFIED]** **[JUDGEMENT]** on operational safety: the family and the service provider must be migrated **together**, because the contrib service package's `dependsOn` targets the contrib family. Migrating only one risks the package manager installing a second family provider alongside the first, with two controllers reconciling the same `ProviderConfig` CRDs. This is a real hazard the tool should guard against.

---

## 4. Machine-detectable licence signals

Evaluated in the order requested, with real data.

### 4.1 OCI image labels / manifest annotations — UNUSABLE

`org.opencontainers.image.licenses`: **absent everywhere.**
`org.opencontainers.image.source`: **absent everywhere.**
Manifest-level `annotations`: **absent entirely** (`"annotations": "NONE"`) on both Upbound and contrib images.

The only labels present are Crossplane's internal layer index. Full label set on `upbound/provider-aws-sqs:v2`:

```json
{
  "io.crossplane.xpkg:sha256:04115f...": "base",
  "io.crossplane.xpkg:sha256:78b7f6...": "schema.go",
  "io.crossplane.xpkg:sha256:848608...": "schema.python",
  "io.crossplane.xpkg:sha256:ac405a...": "schema.kcl",
  "io.crossplane.xpkg:sha256:b50bf2...": "schema.json",
  "io.crossplane.xpkg:sha256:ed182b...": "upbound"
}
```

Contrib is the same shape, just fewer layers:
```json
{
  "io.crossplane.xpkg:sha256:d9db3e...": "base",
  "io.crossplane.xpkg:sha256:ed182b...": "upbound"
}
```

**[VERIFIED]** across `upbound/provider-aws-sqs`, `upbound/provider-family-aws`, `upbound/provider-aws-s3`, `upbound/provider-gcp-storage`, `upbound/provider-azure-network`, and contrib equivalents.

Incidental corroboration of the shared build pipeline: layer `sha256:ed182b7a3dc0caf51f905d7b2800c8a8590e02752e78e8ced665d083a6843be8` (the `upbound` annotated layer) is **the same digest** in both the Upbound Official and the crossplane-contrib image. **[VERIFIED]**

**Verdict: cannot drive a badge. Zero coverage.**

### 4.2 The Crossplane package meta object — PRIMARY SIGNAL, ~75% coverage

`meta.pkg.crossplane.io/v1 Provider` carries `meta.crossplane.io/license` and `meta.crossplane.io/source`. Full field list is in §1.3.

Extraction is cheap: read the manifest, find the layer annotated `io.crossplane.xpkg: base` (~20 KB), fetch **that blob only**, gunzip, untar, read `package.yaml`. **No 270 MB image pull needed.** I built and ran exactly this probe. **[VERIFIED]**

Results across 12 successfully probed packages:

| Package | `license` | `source` | `verification` |
|---|---|---|---|
| `upbound/provider-aws-sqs:v2` | Apache-2.0 | crossplane-contrib/provider-upjet-aws | **Official** |
| `upbound/provider-aws-sqs:v2.7.1` | Apache-2.0 | crossplane-contrib/provider-upjet-aws | **Official** |
| `upbound/provider-gcp-storage:v2.0.0` | Apache-2.0 | crossplane-contrib/provider-upjet-gcp | **Official** |
| `upbound/provider-azure-network:v2.0.0` | Apache-2.0 | crossplane-contrib/provider-upjet-azure | **Official** |
| `contrib/provider-aws-sqs:v2.4.0` | Apache-2.0 | crossplane-contrib/provider-upjet-aws | — |
| `contrib/provider-aws-sqs:v2.7.0` | Apache-2.0 | crossplane-contrib/provider-upjet-aws | — |
| `contrib/provider-family-aws:v2.7.0` | Apache-2.0 | crossplane-contrib/provider-upjet-aws | — |
| `contrib/provider-gcp-storage:v3.0.0` | Apache-2.0 | crossplane-contrib/provider-upjet-gcp | — |
| `contrib/provider-azure-network:v2.7.0` | Apache-2.0 | crossplane-contrib/provider-upjet-azure | — |
| `contrib/provider-kubernetes:v1.3.0` | Apache-2.0 | crossplane-contrib/provider-kubernetes | — |
| `contrib/provider-helm:v1.4.0` | Apache-2.0 | crossplane-contrib/provider-helm | — |
| `contrib/provider-terraform:v1.2.0` | **(empty)** | github.com/upbound/provider-terraform → **404** | — |
| `contrib/provider-keycloak:v3.0.0` | **(empty)** | **(empty)** | — |
| `contrib/provider-nop:v0.4.0` | **(empty)** | **(empty)** | — |

**[VERIFIED]** — 9 of 12 carry a licence annotation. Three do not, and one of the nine carries a **stale source URL**: `github.com/upbound/provider-terraform` returns **HTTP 404** (unlike `upbound/provider-aws`, which redirects correctly). The real repo is `crossplane-contrib/provider-terraform`. **[VERIFIED]**

**Verdict: best available signal, but must not be the only one.** Roughly a quarter of packages will fall through, and source URLs can rot.

### 4.3 Artifact Hub — UNUSABLE, zero coverage

**Artifact Hub does not index Crossplane packages at all.** There is no Crossplane repository kind. Querying the kind facets for a Crossplane provider search returns only Headlamp plugins, Helm charts, KCL modules and OLM operators. **[VERIFIED]**

(My first attempt used kind `8`, which is KEDA scalers — that returned KEDA data, not Crossplane. Re-checking the facet list confirmed no Crossplane kind exists.)

I also probed the Upbound Marketplace for a public JSON API (`/api/v1/packages/...`, `/api/v1/accounts/...`) — both return the Next.js SPA HTML shell, not JSON. **[VERIFIED]** No usable public API there either.

**Verdict: drop it. It cannot contribute.**

### 4.4 GitHub LICENSE via API — BEST FALLBACK

`GET /repos/{owner}/{repo}` returns `.license.spdx_id`, and `GET /repos/{owner}/{repo}/license` adds `html_url`. It follows renames — which is what makes `upbound/provider-aws` resolve correctly.

Coverage across the whole `crossplane-contrib` org, `provider-*` repos **[VERIFIED]**:

```
total provider-* repos: 87   |   active (non-archived): 50
licence, ALL:    { Apache-2.0: 82, NONE: 5 }
licence, ACTIVE: { Apache-2.0: 49, NONE: 1 }
ACTIVE with NO licence: ['provider-dynatrace']
```

**98% of active contrib providers are Apache-2.0 with a single detectable outlier.**

And it recovered **all three** packages that lacked a meta annotation:

| Package | GitHub fallback |
|---|---|
| `provider-terraform` | Apache-2.0 — https://github.com/crossplane-contrib/provider-terraform/blob/main/LICENSE |
| `provider-keycloak` | Apache-2.0 — https://github.com/crossplane-contrib/provider-keycloak/blob/main/LICENSE |
| `provider-nop` | Apache-2.0 — https://github.com/crossplane-contrib/provider-nop/blob/main/LICENSE |

**[VERIFIED]**

Caveats: unauthenticated rate limit is 60 req/hr and **I hit it during this research** — the catalogue must cache aggressively and/or use a token. And per §1.2, do **not** hash-compare LICENSE against canonical Apache text; contrib repos ship a Markdown-reflowed variant that would false-negative.

### 4.5 Recommended resolution chain

```
1. meta.crossplane.io/license from the package base layer   → ~75% hit, authoritative-ish
2. GitHub API on meta.crossplane.io/source                  → covers most of the rest
   (guard: source URL may 404 — provider-terraform does)
3. GitHub API on {publisher-org}/{package-name} convention  → catches stale/missing source
4. UNKNOWN                                                  → explicit, never guessed
```

Independently, always compute:
```
commercial_build = (meta.upbound.io/verification == "Official")
```
This is orthogonal to the licence and must be displayed separately. **[VERIFIED]** as reliably present/absent.

Steps 1 and 4 need no network beyond the registry; step 2–3 need GitHub with caching. All of step 1 is a ~20 KB blob fetch, so a full catalogue refresh is cheap.

---

## 5. Recommended policy for the catalogue

### 5.1 Default catalogue publishers

**In by default:**
- `crossplane-contrib/*` — 98% Apache-2.0 verified, community-governed
- `crossplane/*` — core, functions, upjet; Apache-2.0 verified

**In by default, badged as a commercial build:**
- `upbound/*` — because the user runs two of these and hiding them would break their own workflow. Every one I probed declares Apache-2.0 source.

**Opt-in per publisher:** any other vendor org (`vshn`, `stuttgart-things`, etc.) — allowed but not enabled until the user adds it, with licence resolved by the same chain.

**[JUDGEMENT]** — the choice to include `upbound/*` by default rather than behind a flag is a judgement call, and it is the one I would most expect the user to want to overrule. My reasoning: the user's stated preference is to "scan OSS providers", and Upbound's AWS/GCP/Azure providers *are* OSS at source by their own declaration. Excluding them would exclude the user's own installed packages, which reads as a bug rather than a policy.

### 5.2 Two independent badges, never merged

| Badge | Source | Values |
|---|---|---|
| **Licence** | resolution chain §4.5 | `Apache-2.0` / `MIT` / … / **`Unknown`** |
| **Build** | `meta.upbound.io/verification` | `Community` / **`Commercial build`** |

Keeping these separate is the whole point of this research. `Apache-2.0 + Commercial build` is a real and common combination — it is exactly what the user is running — and collapsing the two into one "is it OSS?" flag is what produced the original confusion.

Show provenance on hover/detail: which step of the chain produced the licence, and a link to the actual LICENSE file. A badge the user cannot audit is worse than no badge.

Where a NOTICE file exists, add a quiet **`bundles third-party components`** marker rather than attempting a composite SPDX expression (§1.4). **[JUDGEMENT]**

### 5.3 Non-OSS entries: shown with a badge, not hidden

- **Never silently hide.** A provider missing from a catalogue with no explanation is indistinguishable from a broken catalogue.
- Provide a filter: `Licence: [Any | OSI-approved only]`, default **Any**, persisted.
- Add an **"OSS build available"** affordance. When a package carries `verification: Official` *and* its `meta.crossplane.io/source` points at a `crossplane-contrib` repo, offer the contrib equivalent inline. For the user's stack this resolves to a byte-identical package — a strong, honest recommendation, not a downgrade.
- If a genuinely closed-source provider is ever encountered (source annotation absent AND no resolvable public repo AND `verification: Official`), badge it **`Proprietary`** and exclude it from the default filter — but still list it.

### 5.4 When licence is UNKNOWN

Never guess, never infer from the publisher org, never infer from a successful anonymous pull.

- Badge **`Licence: Unknown`** in a warning colour, not a neutral one.
- Show what was tried: "no `meta.crossplane.io/license`; source repo `github.com/upbound/provider-terraform` returned 404; no LICENSE found at `crossplane-contrib/provider-terraform`" — a concrete diagnostic string.
- Still installable, with a one-line confirmation.
- Excluded from `OSI-approved only`.
- Link out to the repo and registry so the user can check by hand.

**Do not treat pull success as a licence signal.** §2.2 shows anonymous pull succeeds on packages Upbound documents as subscription-gated. Pullability and licensing are independent axes.

### 5.5 Implementation notes

- Cache the resolved licence per **immutable digest**, not per tag. Tags move — `:v2` currently points at v2.4.0, not the newest v2.7.1. **[VERIFIED]**
- Warn on floating tags. `:v2` resolving to a release three versions behind is a genuine footgun the tool can surface for free.
- Fetch only the `base`-annotated layer for meta extraction.
- Do not surface GHCR `DENIED` as "auth required" — it usually means the package name is wrong (§2.4).
- Re-check the auth posture periodically. §2.2 is a point-in-time observation; Upbound has changed enforcement before and documents an intent to enforce.
- The tool being MIT/Apache-2.0 is unaffected by any of this. Reading public registry metadata and displaying a licence string creates no derivative work of the providers.

---

## 6. Counter-consideration, stated honestly

**The specific worry — that crossplane-contrib providers might be v1-only and lack the `.m.` namespaced variants that Crossplane v2 and the existing design depend on — is verifiably unfounded.**

`xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs:v2.4.0` ships:

```
group=sqs.aws.m.upbound.io   kind=Queue                   scope=Namespaced  v1beta1
group=sqs.aws.m.upbound.io   kind=QueuePolicy             scope=Namespaced  v1beta1
group=sqs.aws.m.upbound.io   kind=QueueRedrivePolicy      scope=Namespaced  v1beta1
group=sqs.aws.m.upbound.io   kind=QueueRedriveAllowPolicy scope=Namespaced  v1beta1
group=sqs.aws.upbound.io     kind=Queue                   scope=Cluster     v1beta1
... (4 cluster-scoped legacy equivalents)
```

Byte-identical to Upbound's. The family package likewise ships `aws.m.upbound.io` `ProviderConfig` (Namespaced) and `ClusterProviderConfig` (Cluster), also byte-identical. **[VERIFIED]**

Every contrib package I probed with a v2 release had namespaced groups present (`provider-family-aws`, `provider-gcp-storage`, `provider-azure-network`, `provider-kubernetes` v1.3.0, `provider-helm` v1.4.0, `provider-terraform` v1.2.0, `provider-keycloak` v3.0.0 with 17 namespaced groups). **[VERIFIED]** Contrib even tags explicit `v1.23.0-crossplane-v2-preview.0` builds, showing v2 support was tracked deliberately.

**So: an OSS-preferring default creates no friction with the user's existing setup.** Everything validated against `sqs.aws.m.upbound.io` continues to work unchanged.

Honest counterweights, none of which change the recommendation:

- **Only older packages lack `.m.` variants.** `provider-helm:v0.21.0` has zero namespaced groups; `v1.4.0` has them. `provider-nop:v0.4.0` has none. The catalogue should therefore compute and display **"Crossplane v2 namespaced: yes/no"** per version by counting `.m.` groups in the CRD set — I did exactly this and it is cheap. This is a more useful compatibility signal than the licence badge for day-to-day use. **[JUDGEMENT]**
- **The user validated against Upbound's v2.4.0 specifically.** Contrib v2.4.0 is byte-identical, so migration at the same version carries essentially no API risk. Contrib's *latest* (v2.7.0) is not what was validated — the tool should default a switch to the **same version**, not the newest.
- **Migrate family and service provider together** (§3.3) or risk two family providers fighting over the same CRDs.
- **Losing Upbound's build warranty.** Switching forfeits the CVE remediation / backporting / FIPS wrapper Upbound advertises. For a solo/dev control plane that is likely irrelevant; for a regulated production platform it may not be. This is the user's call, not the tool's.
- **The `.upbound.io` API group suffix is permanent either way.** Even on fully OSS contrib packages the groups remain `sqs.aws.m.upbound.io`. If part of the motivation was to get Upbound out of the manifests entirely, that is not achievable — the group name is baked into the generated types. Worth saying plainly so the user is not surprised.
- **Point-in-time risk.** §2.2's anonymous-pull result is today's behaviour. If Upbound enforces the documented account requirement, `upbound/*` entries would break for unauthenticated users while `crossplane-contrib/*` would not. This is an argument *for* the OSS-preferring default, and for a "verify pullability" check in the catalogue rather than a cached assumption.

---

## Appendix: reproduction commands

All registry work used an empty Docker config to guarantee anonymity:

```bash
export DOCKER_CONFIG=/tmp/anon && mkdir -p $DOCKER_CONFIG && echo '{}' > $DOCKER_CONFIG/config.json

crane config   xpkg.upbound.io/upbound/provider-aws-sqs:v2 | jq '.config.Labels'
crane digest   xpkg.upbound.io/upbound/provider-aws-sqs:v2
crane pull     xpkg.upbound.io/upbound/provider-aws-sqs:v1.17.0 old.tar   # 208 MB, anonymous, OK
crane ls       xpkg.crossplane.io/crossplane-contrib/provider-aws-sqs

# anonymous token scope
curl -s "https://xpkg.upbound.io/service/token?scope=repository%3Aupbound%2Fprovider-aws-sqs%3Apull&service=xpkg.upbound.io"

# cheap meta extraction: base layer only (~20 KB, not the 270 MB image)
D=$(crane digest --platform linux/amd64 "$REF")
BASE=$(crane manifest "${REF%%:*}@$D" | jq -r '.layers[]|select(.annotations."io.crossplane.xpkg"=="base")|.digest')
crane blob "${REF%%:*}@$BASE" | gunzip | tar -xO package.yaml

gh api repos/crossplane-contrib/provider-upjet-aws -q '.license.spdx_id'
```

Working files: `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/ossresearch/`
(`probe.sh` — base-layer meta probe; `extract.py` — package.yaml extraction; `crds.py` — group/kind/scope listing; `diffcrd.py` — per-CRD SHA-256 comparison)

Environment gotcha for anyone re-running these: the shared scratchpad contains a `struct.py` that shadows the stdlib module and prints on import, breaking `import tarfile`. Run from an isolated directory.

### Sources

- [crossplane-contrib/provider-upjet-aws](https://github.com/crossplane-contrib/provider-upjet-aws) · [LICENSE](https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/LICENSE) · [NOTICE](https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/NOTICE)
- [Official Providers | Upbound Documentation](https://docs.upbound.io/manuals/packages/providers/)
- [Official Providers FAQ | Upbound Documentation](https://docs.upbound.io/manuals/packages/providers/faq/)
- [An Update on Upbound's Official Providers](https://www.upbound.io/blog/an-update-on-upbounds-official-providers)
- [Upcoming Changes to Upbound Official Packages](https://www.upbound.io/blog/upbound-official-packages-changes)
- [Upbound Software License Agreement](https://licenses.upbound.io/upbound-software-license.html)
- [Upbound Terms & Conditions](https://www.upbound.io/terms-conditions)
- [Apache License 2.0 (canonical text)](https://www.apache.org/licenses/LICENSE-2.0.txt) · [MPL-2.0](https://www.mozilla.org/en-US/MPL/2.0/)
- [crossplane/crossplane](https://github.com/crossplane/crossplane) · [crossplane/upjet](https://github.com/crossplane/upjet) · [upbound/universal-crossplane (archived)](https://github.com/upbound/universal-crossplane)
