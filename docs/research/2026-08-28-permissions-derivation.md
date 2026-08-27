# Permissions from the Canvas — Feasibility Verdict and Design Recommendation

**Date:** 2026-08-28 · **Feature:** compositionfactory §9, "Permissions from the canvas"
**Target stack observed throughout:** Crossplane server **v2.4.0** (`kind-platform`), Crossplane CLI **v2.5.0**, k8s **v1.36.1**, rbac-manager from `xpkg.crossplane.io/crossplane/crossplane:v2.4.0`.

## Provenance legend

| Tag | Meaning |
|---|---|
| **[V]** | **VERIFIED** — the originating brief ran it against the live cluster, executed the binary, fetched the bytes, or read the source at tag `v2.4.0` |
| **[D]** | **DOCS** — read in vendor docs, a README, an issue, or source, but not executed |
| **[U]** | Explicitly flagged **unverified / estimated** by the originating brief |
| **UNRESOLVED** | Two briefs disagree; not silently reconciled |

### Source briefs

| Brief | Lines | Status |
|---|---|---|
| `raw/perm-k8s-rbac.md` | 413 | present |
| `raw/perm-cloud-iam.md` | 381 | present |
| `raw/perm-prior-art.md` | 449 | present |

**All three briefs exist and were read in full.** Nothing is missing. [V]

---

## 1. Verdict in five sentences

**Kubernetes RBAC is fully derivable, offline, at exact fidelity, and it is the highest-value thing this tool can emit** — the composed GVKs are known at design time, Kind→plural resolves at 177/177 from vendored OpenAPI v3 paths and 148/148 from the fallback guesser, the aggregation label and verb set are fixed constants, and the answer can be *verified* against a live cluster with a read-only `SubjectAccessReview` that no prior-art tool can perform. [V]

**Cloud IAM is derivable only as an approximate, review-required draft**: the chain MR kind → Terraform resource → CloudFormation type → IAM actions completes automatically for **53.2%** of `provider-upjet-aws` resources today (~70% with a bounded ~180-entry alias file, an **estimate, not measured** [U]), and where it does complete, measured fidelity against real Terraform call sites is recall 69–100% / precision 57–94% — leaking in *both* directions on every resource tested. [V]

**Neither half has prior art to reinvent**: static-from-manifests RBAC generation is an empty niche (every k8s generator derives from audit logs), there is no Terraform-resource→IAM mapping anywhere (`iam-dataset` is SDK-method-keyed and has no Terraform directory), and neither Crossplane nor Upbound publishes the IAM a provider's credentials need — the de facto community answer is `AdministratorAccess` for cloud and `cluster-admin` for k8s. [V]

**The two halves must therefore be presented differently, not uniformly**: RBAC ships as a plain, apply-ready `ClusterRole` whose per-rule states are *verified* against the cluster; IAM ships behind a visible confidence tier per statement, an `unknown` bucket rendered as a to-do list rather than an omission, and a header that says "starting point for review" rather than "least privilege".

**Scope call: RBAC in v1 (M5) as specified today; IAM deferred to M6 and gated on the alias table and the tiering UI actually existing** — RBAC costs one vendored table the schema store already carries and adds no new dependency, licence, or network fetch, while IAM adds a build-time dataset fetch, a licence question, a hand-maintained alias file, and a permanent staleness obligation.

---

## 2. Kubernetes RBAC — implementable as specified

Everything in this section is sufficient to implement `emit: {rbac: true}` without reading the briefs.

### 2.1 The artifact: exactly one aggregated `ClusterRole` per XRD

Given composed GVKs `apps/v1 Deployment`, `v1 Service`, `v1 Secret`, `networking.k8s.io/v1 Ingress`, emit: [V — `kubectl apply --server-side --dry-run=server` returned `serverside-applied (server dry run)`]

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  # <tool-prefix>:<xrd-name>:aggregate-to-crossplane
  name: compositionfactory:xmicroservices.sparky.ee:aggregate-to-crossplane
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets", "services"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

**No `RoleBinding` is needed.** Aggregation *is* the binding mechanism — `ClusterRole/crossplane` is bound by `ClusterRoleBinding/crossplane` to `ServiceAccount crossplane-system/crossplane`, and pulls in any labelled ClusterRole. [V]

### 2.2 The aggregation label — exact

```json
"aggregationRule": {
  "clusterRoleSelectors": [
    { "matchLabels": { "rbac.crossplane.io/aggregate-to-crossplane": "true" } }
  ]
}
```

Key `rbac.crossplane.io/aggregate-to-crossplane`, value the **quoted lowercase string** `"true"`. [V] `matchLabels` is exact string match: `"True"`, `"yes"`, or an unquoted YAML boolean silently do not aggregate. Docs confirm the label *"is critical"*. [D]

Sibling label keys observed on the cluster, all value `true`, none of them the right one for this job: `-to-admin` / `-edit` / `-view` / `-browse` (cluster-wide humans), `-to-ns-admin` / `-ns-edit` / `-ns-view` (per-namespace humans), `-to-allowed-provider-permissions` (provider ceiling — currently resolves to **zero rules**, selector matches nothing), `rbac.crossplane.io/system` (marker, value = provider revision name). [V]

### 2.3 Why `ClusterRole` and never `Role` — two independent reasons

1. **Aggregation only selects ClusterRoles.** The Kubernetes API has `clusterRoleSelectors` and no `roleSelectors`; a `Role` can never aggregate in, and rbac-manager creates no `RoleBinding` for the Crossplane SA. [V]
2. **Informers are cluster-wide.** `--watch-cache-namespaced` defaults `false` and is unset on the live deployment (`args: ["core","start"]`, no feature flags at all), so the realtime-composition informer does a cluster-scoped `list`/`watch`. [V]

XR scope affects *validation*, never the shape of the emitted RBAC.

### 2.4 Verbs — all seven, always

`get, list, watch, create, update, patch, delete`

| Verb | Why | Tag |
|---|---|---|
| `patch` | composed resources are written with **server-side apply**, `composition_functions.go:689`, field owner `apiextensions.crossplane.io/composed/<32-hex-of-xr>`; SSA is a PATCH so the authorizer checks `patch`, not `create` | [V] |
| `create` | SSA of a not-yet-existing object additionally requires `create` | [D] — [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) |
| `get`, `list`, `watch` | `--enable-realtime-compositions` defaults **true** (Beta) and starts an informer per composed GVK — this is what makes `list`/`watch` mandatory, not optional | [V] |
| `delete` | composed-resource GC, `composition_functions.go:1005` | [V] |
| `update` | **not used** by the composite controller — composed objects are only ever `Patch`ed, `Get`, `Delete`d — but Crossplane's preflight authorizer loops over exactly this seven-verb list and warns if any is missing, so omitting it produces permanent spurious `RoleBasedAccessControl` events | [V] |

`verbs: ["*"]` also satisfies the authorizer (it short-circuits on `*`) and is what the canonical docs example uses. [D] **Offer it as a "broad" toggle; default to the explicit seven** so a reviewer sees a least-privilege list.

### 2.5 Subresources — do not emit them for composed objects

Do **not** emit `deployments/status` or `deployments/finalizers`. Every `client.Status().Patch/Update` call in the composite controller targets the XR itself, never `cd.Resource`. [V] `/status` and `/finalizers` *are* required for XRs and MRs, but rbac-manager already generates those, so emitting them duplicates existing grants.

> **UNRESOLVED — verb template and subresources.** `perm-prior-art.md` §6.3 recommends the opposite: mirror rbac-manager's XRD role template, i.e. `verbs: ["*"]` on the resource **and** `/status`, plus `update` on `/finalizers`, and warns "do not invent a narrower set without testing it". `perm-k8s-rbac.md` §4 reaches its position by reading the composite controller source at `v2.4.0` and observing that composed-resource status is never written. The disagreement is real: prior-art generalises from what rbac-manager emits for a *different object class* (XRs, which Crossplane does own the status of); k8s-rbac reads the code path for composed resources specifically. **Recommendation: follow `perm-k8s-rbac.md` — explicit seven verbs, no subresources — because it is source-verified for the exact object class in question.** Settle it empirically before M5 ships (Open Question 1).

### 2.6 `apiGroups` and rule shape

- Core group is the **empty string `""`** — never `"core"`, never `"v1"`. Derive by splitting `apiVersion` on `/`: one segment → `""`, two → the first segment.
- `resources` are lowercase plurals with **no group suffix** (`deployments`, not `deployments.apps`).
- Deterministic ordering is a correctness requirement, not a nicety — a churning file on a `prune: true` + `selfHeal: true` ArgoCD repo is a live-cluster incident (design spec §8).

> **UNRESOLVED — rule granularity.** `perm-k8s-rbac.md` §4 says "group the rules by apiGroup, sorting resources within each" (one rule per apiGroup). `perm-prior-art.md` §6.3 says "**do not merge rules across nodes**" — Kubernetes RBAC is a pure additive union with no deny, so N un-merged rules are semantically identical to the merged form and attribution is therefore free; it explicitly calls `controller-gen`'s merging behaviour the thing to reject, having run `controller-gen` v0.16.5 and confirmed its output "contains **zero** trace of which marker produced which rule". [V]
> **Both goals are satisfiable at once and this is a resolvable design choice, not a fact conflict: emit one rule per canvas node, preceded by a YAML comment naming the node, and sort the rules by `(apiGroup, first resource, node id)`.** That is deterministic *and* attributed. Flagged UNRESOLVED because the two briefs state incompatible rules and neither tested the other's output.

### 2.7 Kind → resource-plural, offline

**Primary method — read the plural out of the OpenAPI v3 *paths*. No guessing.** [V]

The plural is not in the schema `components` (the `Deployment` schema carries only `x-kubernetes-group-version-kind`). It is in the path structure.

1. Fetch or vendor `/openapi/v3` → per-group-version documents.
2. Match the **collection** form — the path must *end* at the plural:
   ```
   ^/apis?/(?:([^/]+)/)?([^/]+)/(?:namespaces/\{namespace\}/)?([a-z0-9]+)$
   ```
3. Take the `post` operation's `x-kubernetes-group-version-kind` → the Kind. Capture group 3 → the plural.

Measured over the live cluster's 74 group-version documents: **186 GVKs** had a derivable plural; requiring the path to *end* at the plural removed all **9** subresource pseudo-kinds (`Eviction`→`pods`, `Scale`→`deployments`, `TokenRequest`→`serviceaccounts`, `PodExecOptions`/`PodAttachOptions`/`PodProxyOptions`/`PodPortForwardOptions`/`NodeProxyOptions`/`ServiceProxyOptions`) with **zero false negatives**; **177/177 real resource types correct**; on `core/v1` it yields exactly 16 composable kinds including `Endpoints → endpoints` **with no special-casing whatsoever**. [V]

Bonus: a Kind with no `post` on its collection path is not creatable, therefore not composable — grey it out in the GUI.

**Fallback for a bare Kind string** — Kubernetes' own `UnsafeGuessKindToResource` (`k8s.io/apimachinery/pkg/api/meta`):

```go
func guess(kind string) string {
    s := strings.ToLower(kind)
    if s == "" { return s }
    if strings.HasSuffix(s, "endpoints") { return s }        // unpluralizedSuffixes
    if strings.HasSuffix(s, "s")         { return s + "es" }
    if strings.HasSuffix(s, "y")         { return strings.TrimSuffix(s, "y") + "ies" }
    return s + "s"
}
```

**148/148 (100%)** against live `kubectl api-resources` (core k8s, cert-manager, Argo CD, Argo Rollouts, Kargo, Crossplane, Upbound AWS MRs) and **81/81 (100%)** against `CustomResourceDefinition.spec.names.plural` as declared by installed CRDs. [V]

**The suspected irregular cases are all already handled by those three rules** — there is no hand-written exception table to maintain:

| Kind | Rule | Plural |
|---|---|---|
| `Endpoints` | `unpluralizedSuffixes` | `endpoints` (not `endpointses`) |
| `Ingress` | `s` → `es` | `ingresses` |
| `NetworkPolicy` | `y` → `ies` | `networkpolicies` |
| `PodSecurityPolicy` | `y` → `ies` | `podsecuritypolicies` (removed in k8s 1.25) |
| `ComponentStatus` | `s` → `es` | `componentstatuses` |
| `IngressClass`, `StorageClass`, `PriorityClass` | `s` → `es` | `…classes` |
| `APIService` | default | `apiservices` |

**Residual risk, both flagged [D] and not observed on this cluster:**
- `metrics.k8s.io/v1beta1`: `NodeMetrics` → `nodes`, `PodMetrics` → `pods`. A genuine violation. Low risk — aggregated read-only API, not composable.
- **A CRD may legally declare an arbitrary `spec.names.plural` unrelated to its Kind.** Zero of 81 installed CRDs did so, but it is legal. *Mitigation: prefer the OpenAPI-path method, which reads the declared plural rather than guessing it. Fall back to `guess()` only for a Kind with no schema, and mark such rows "unverified plural" in the GUI.*

### 2.8 The failure mode without it

**The accidental allowlist.** `ClusterRole/crossplane` resolves to 16 aggregated rules; rules 8–15 come from the static `crossplane:system:aggregate-to-crossplane` role and exist for **package management** (provider Deployments, webhook Services/ServiceAccounts, connection Secrets, leader-election Leases). They incidentally authorize composition of a handful of kinds. [V — `kubectl auth can-i --as=system:serviceaccount:crossplane-system:crossplane`]

| Composed GVK | Result |
|---|---|
| `apps/v1 Deployment`, `v1 Service`, `v1 Secret`, `v1 ConfigMap`, `v1 ServiceAccount`, `apiextensions.k8s.io CustomResourceDefinition` | **works out of the box** (accidentally) |
| `v1 Event` | **partial** — `create` yes, `get`/`list`/`watch` **no** → the informer fails |
| `StatefulSet`, `DaemonSet`, `ReplicaSet`, `Pod`, `PersistentVolumeClaim`, `Namespace`, `Endpoints`, `Job`, `CronJob`, `Ingress`, `NetworkPolicy`, `Role`, `RoleBinding`, `ClusterRole`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, `cert-manager.io` `Certificate`/`Issuer`, `argoproj.io` `Application`/`Rollout` | **denied** |
| `sqs.aws.(m.)upbound.io Queue` | works (rbac-manager, via the provider's `aggregate-to-edit` role, which also carries `aggregate-to-crossplane: "true"`) |

`perm-prior-art.md` quantifies the same allowlist over a 17-kind common-composable sample: **12 of 17 (71%) are denied**. [V] The two counts are consistent — different denominators (prior-art's sample excludes CRDs and Events, k8s-rbac's includes them).

`Event` is the instructive case: a *partial* grant. **A generator must reason at verb granularity, not "is this apiGroup mentioned anywhere".**

**Why the problem stays invisible.** The live cluster's working `xmicroservices.kubernetes.sparky.ee` Composition composes exactly `Deployment` + `Service` — both accidentally allowed — and all 10 of its XRs report `SYNCED=True READY=True` with no extra ClusterRole. The two most common demo objects are in the allowlist. The failure appears the moment someone drags an `Ingress` or a `Job` onto the canvas. [V]

**Exact denial text**, captured live via `kubectl apply --server-side --dry-run=server --as=system:serviceaccount:crossplane-system:crossplane` (no persistent change): [V]

```
Error from server (Forbidden): ingresses.networking.k8s.io "rbac-probe" is forbidden:
User "system:serviceaccount:crossplane-system:crossplane" cannot patch resource "ingresses"
in API group "networking.k8s.io" in the namespace "team-a"
```

Note the denied verb is **`patch`**, not `create` — because SSA. Control: the same dry-run for `apps/v1 Deployment` returned `serverside-applied (server dry run)`.

**How it surfaces, and why it misdirects.** `IsInvalid` is handled specially; **`Forbidden` is not**, so it falls through to a fatal `ComposedResourceError` (`composition_functions.go:711`) and the reconciler marks `Synced=False` with reason `ReconcileError` (`reconciler.go:789`). [V] v2.4 added a dedicated diagnostic — on failure an authorizer re-checks the GVK and emits a second event `reasonRBAC = "RoleBasedAccessControl"` formatted at `internal/engine/errors.go:42` as: [V]

```
{user} is not allowed to [{denied verbs}] resource {plural}.{group}/{version} in namespace {ns}
```

**But it is misleading after the first reconcile** — an open bug, [crossplane/crossplane#7398](https://github.com/crossplane/crossplane/issues/7398), state OPEN. [D] The first reconcile reports the truth; every subsequent one reports an informer timeout, because the informer for the unauthorized GVK never started. The steady-state condition an operator actually sees:

```yaml
- type: Synced
  status: "False"
  reason: ReconcileError
  message: |
    cannot compose resources: cannot get existing composed resources:
    cannot get composed resource: Timeout: failed waiting for
    *unstructured.Unstructured Informer to sync
```

**This is the "why nothing happens" signature, and it points at networking / API-server health rather than RBAC.** Surface that string verbatim in the GUI with the mapping "if you see this, it means a missing ClusterRole" — it took a maintainer-adjacent issue thread to establish that link.

### 2.9 Scope validation — block it at drag time

- **A Namespaced XR can compose ONLY namespaced objects.** A cluster-scoped composed resource is a hard fatal error (`composition_functions.go:551-586`): [V]
  ```go
  errFmtNamespacedXRClusterResource =
    "cannot apply cluster scoped composed resource %q (a %s named %s) for a namespaced composite resource."
  ```
  The check is a RESTMapper lookup, and the `namespaced` boolean lives in the same OpenAPI data as the plural — **so the canvas can and should refuse the drop.**
- **The composed object always inherits the XR's namespace**; a different namespace in the template is silently overwritten with a warning (`errFmtNamespaceOverridden`). [V]
- **A Cluster-scoped XR may compose cluster-scoped objects and namespaced objects in arbitrary namespaces.** [V]

### 2.10 Per-GVK states the generator must compute

| State | Condition | GUI |
|---|---|---|
| `already-granted` | GVK in a provider-owned group (`*.upbound.io`, `*.crossplane.io`) or the XR's own group | green — "granted by rbac-manager" |
| `accidentally-granted` | Deployment / Service / Secret / ConfigMap / ServiceAccount / CRD | green — "granted by core Crossplane"; **still emit the rule** (the grant is incidental, not contractual) but badge it "already satisfied" so nobody is nagged to apply redundant YAML |
| `needs-rule` | anything else | amber — contributes a rule; this is the demo |
| `blocked` | cluster-scoped kind under a Namespaced XR | red — RBAC cannot help |

### 2.11 The correctness oracle nobody else has

`SubjectAccessReview` — read-only, fast, no mutation, works over any kubeconfig: [V]

```
kubectl auth can-i create jobs.batch --as=system:serviceaccount:crossplane-system:crossplane -n default
```

This converts the entire Kubernetes half from *inferred* to *verified*, and yields the "already satisfied" state for free. **No prior-art tool in either survey can validate its own output against the target system.** Lead with it.

*Precision note:* Crossplane's own authorizer uses a **Self**SubjectAccessReview (it asks about itself). compositionfactory is asking *on behalf of another subject*, which is a `SubjectAccessReview` with `spec.user: system:serviceaccount:crossplane-system:crossplane` — exactly what `kubectl auth can-i --as=` sends. Creating a SAR is itself an authorization-checked operation, so `cf` needs rights the operator may not have; degrade gracefully to `inferred` and **say so**, rather than pretending.

### 2.12 Prior art: none. Checked three ways. [V]

1. GitHub **repository** search — `crossplane rbac generate composition clusterrole`, `aggregate-to-crossplane generator`, `crossplane composition rbac generator`: **`total_count = 0`** for all three.
2. GitHub **code** search `aggregate-to-crossplane language:go`: **11 hits, all crossplane itself or forks** — `crossplane/crossplane`, `IBM/ibm-crossplane`, `turkenh/upbound-crossplane-experiment`, `gonzalezjp/crossplane`, `muvaf/upbound-crossplane` (all `internal/controller/rbac/...`), plus one provider (`rossigee/provider-discord`). No generator, no linter, no CLI.
3. Every k8s RBAC generator in the ecosystem is audit-log-derived (`audit2rbac`, `rbac-tool auditgen`, `Audicia`); `rbac-tool gen` is discovery-API wildcard-expansion-with-denylist ("everything except"), the opposite of least privilege from known intent; `krane`/`rakkess`/`kubectl-who-can` only analyse or query. [V/D]

Community answers on [discussion #4932](https://github.com/crossplane/crossplane/discussions/4932) recommend **binding `cluster-admin` to the Crossplane SA**. That is the state of the art. The [RBAC manager design doc](https://github.com/crossplane/crossplane/blob/main/design/design-doc-rbac-manager.md) considered a "rule driven" approach and rejected it — *"Crossplane is choosing to be opinionated about its RBAC roles at this time."* [D] **Upstream has stated it will not fill this space.**

---

## 3. Cloud IAM — approximate only: 53.2% automated coverage, recall 69–100%, precision 57–94%

This heading is the finding. Everything below qualifies it.

### 3.1 The derivation chain, hop by hop

```
MR (group, Kind)                          e.g. sqs.aws.m.upbound.io / Queue
  └─(1) generated table from zz_<kind>_terraformed.go     Apache-2.0, exact, 1,350 files
        → terraform resource name          aws_sqs_queue
        └─(2a) generated alias table + name normalisation  ~180 hand entries [U, estimated]
              → CFN typeName               AWS::SQS::Queue
              └─(3) CFN schema handlers + tagging          AWS-authored
                 → per-op action sets       [confidence HIGH]
        └─(2b) fallback: names_data.hcl regexes / iam-dataset   99.8% service resolution
              → IAM service prefix          sqs
              └─ Service Reference actions filtered by
                 IsWrite / IsList / IsTaggingOnly + verb+noun match
                 → candidate action set     [confidence LOW — label it]
  + always: sts:GetCallerIdentity, and ∪ tagging.permissions into the read set
```

**Hop 1 — MR kind → Terraform resource.**
- **It is NOT in the provider package. Negative result, stated plainly.** [V] Pulling and unpacking `xpkg.upbound.io/upbound/provider-aws-sqs:v2` (digest `sha256:e3aaedcc…`) and reading every layer: CRD `.metadata.annotations` = `{}`, `.metadata.labels` = `{}`, `names.categories` = `[crossplane, managed, aws]`, `package.yaml` Provider annotations carry `auth.upbound.io/group`, `friendly-name`, `description`, `license`, `maintainer`, `readme`, `source`, `hardening`, `host`, `support`, `verification` — **no terraform field**. Crossplane v2 `ManagedResourceDefinition.spec` = `[conversion, group, names, scope, state, versions]` — no permissions or TF field. The binary has `aws_sqs_queue` 110× as compiled Go string data, not declaratively extractable. **A compositionfactory instance that only has the installed CRDs cannot recover the Terraform resource name.**
  - *Trap:* the 9 textual `aws_sqs_queue` hits inside the CRD sit in field descriptions copied from Terraform registry docs ("It is preferred to use the `aws_sqs_queue_policy` resource instead") and reference **sibling** resources. Parsing those would be actively wrong.
- **Convention is 75.9% — not shippable.** [V] `"aws_" + group + "_" + snake(Kind)` tested against `config/generated.lst`: `793/1045`, 252 exceptions concentrated in `ec2(74) rds(19) directconnect(17) cloudwatchlogs(11) cognitoidp(9) kafka(9) neptune(9) cloudwatchevents(8) elb(8) elbv2(8) configservice(7) dynamodb(5)`. Canonical failures: `ec2/Instance → aws_instance`, `rds/Cluster → aws_rds_cluster` but `rds/Instance → aws_db_instance`, `elbv2/LB → aws_lb`.
- **What is authoritative:** `apis/cluster/<group>/<version>/zz_<kind>_terraformed.go`, **1,350 files** in `crossplane-contrib/provider-upjet-aws` (**820** in `provider-upjet-gcp`), Apache-2.0, each containing exactly: [V]
  ```go
  func (mg *Queue) GetTerraformResourceType() string {
      return "aws_sqs_queue"
  }
  ```
  **Ship a `go:generate` step that walks these paths and emits `map[GroupKind]string`.** Path gives group+kind, one regex gives the name. Refreshable per provider release. Also in-repo and machine-readable: `config/generated.lst` (JSON array, **1,029** TF names AWS / **406** GCP), `config/schema.json`, `config/provider-metadata.yaml`, `config/externalname.go`.

**Hop 2 — Terraform resource → CloudFormation type.** This is the lossy hop, and the loss is **name skew, not missing data**: `aws_rds_cluster` vs `AWS::RDS::DBCluster`. See §3.3.

**Hop 3 — CFN type → IAM actions.** Mechanical and AWS-authored. See §3.2.

### 3.2 The dataset: AWS CloudFormation registry resource schemas

**Source, public and unauthenticated, HTTP 200 [V]:** `https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip` → 2.9 MB zip, 13.9 MB unpacked, **1,722** `.json` files. Single type also available: `https://schema.cloudformation.us-east-1.amazonaws.com/aws-sqs-queue.json` (13.6 KB).

Verbatim from `aws-sqs-queue.json`: [V]

```json
"handlers": {
  "create": {"permissions": ["sqs:CreateQueue","sqs:GetQueueUrl","sqs:GetQueueAttributes","sqs:ListQueueTags","sqs:TagQueue"]},
  "read":   {"permissions": ["sqs:GetQueueAttributes","sqs:ListQueueTags"]},
  "update": {"permissions": ["sqs:SetQueueAttributes","sqs:GetQueueAttributes","sqs:ListQueueTags","sqs:TagQueue","sqs:UntagQueue"]},
  "delete": {"permissions": ["sqs:DeleteQueue","sqs:GetQueueAttributes"]},
  "list":   {"permissions": ["sqs:ListQueues"]}
},
"tagging": {"taggable": true, "tagOnCreate": true, "tagUpdatable": true,
            "tagProperty": "/properties/Tags",
            "permissions": ["sqs:TagQueue","sqs:UntagQueue","sqs:ListQueueTags"]}
```

Dataset coverage: [V]

| metric | count | % of 1,722 |
|---|---:|---:|
| schemas total | 1,722 | 100% |
| with any `handlers` | 1,577 | **91.6%** |
| with all four of create/read/update/delete permissions | 1,401 | 81.4% |
| with `tagging.permissions` | 1,068 | 62.0% |

9,033 distinct IAM actions across the corpus (create 1,577 / read 1,569 / update 1,410 / delete 1,570 / list 1,495). [V] The 145 without handlers are legacy pre-registry types: `AWS::AppMesh::Mesh`, `AWS::Pinpoint::App`, `AWS::Glue::Partition`, `AWS::MediaLive::Channel`, `AWS::ElastiCache::SecurityGroup`, `AWS::WAFRegional::*`, `AWS::Config::ConfigurationRecorder`. [V]

**The bonus that no heuristic reproduces:** handler permissions include cross-service dependencies. `AWS::Athena::WorkGroup` create needs `s3:*` + `kms:*` + `iam:*`; `AWS::EC2::Instance` needs `iam:PassRole` and `ssm:*`; `AWS::RDS::DBCluster` needs `secretsmanager:CreateSecret` and `iam:CreateServiceLinkedRole`. [V] Those are exactly what breaks real deployments.

**Distilled permissions-only index** (typeName → {create,read,update,delete,list,tagging} → actions): **1,577 entries, 1,070,392 bytes raw, 103,720 bytes gzipped.** [V] Trivially `go:embed`-able.

> **Discrepancy resolved — do not quote 91.6% as end-to-end coverage.** `perm-prior-art.md` §6.1 describes cloud IAM as "derivable at 91.6% from the CFN registry". That figure is **coverage of the CFN dataset by itself** — the fraction of CFN types carrying handler permissions. The end-to-end number, measured over the actual `provider-upjet-aws` corpus in `perm-cloud-iam.md` §4, is **53.2%**. The gap between them is entirely hop 2. Use 53.2% in any user-facing or planning claim.

### 3.3 Quantified end-to-end AWS coverage — tiered over all 1,029 upjet-aws TF resources [V]

| tier | what the user sees | count | % |
|---|---|---:|---:|
| **T1** CFN type matched strictly, has handler perms | exact per-resource CRUD + tagging action list | 471 | **45.8%** |
| **T1b** CFN type matched by suffix fuzz, has perms | same, flagged "name-matched heuristically" | 76 | **7.4%** |
| **T2** CFN type matched but schema has no handlers | falls through to T3/T5 | 61 | 5.9% |
| **T3** SAR verb heuristic finds both `Create*`+`Delete*` | plausible CRUD set, "unverified" | 84 | 8.2% |
| **T4** SAR verb heuristic finds some verbs | partial set, "incomplete" | 35 | 3.4% |
| **T5** service known, no resource-level match | `sqs:*`-style service scope + access-level filter | 178 | 17.3% |
| **T0** TF name has no service prefix (`aws_instance`, `aws_vpc`, `aws_db_*`, `aws_lb*`, `aws_cognito_*`) | nothing without a curated entry | 86 | 8.4% |
| **T6** nothing at all | nothing | 38 | 3.7% |

**Headline: 53.2% (T1 + T1b) get a real per-resource action set with zero curation.**

The T0 set is 86 names, almost all with obvious CFN counterparts (`aws_instance`→`AWS::EC2::Instance`, `aws_vpc`→`AWS::EC2::VPC`, `aws_db_instance`→`AWS::RDS::DBInstance`, `aws_lb`→`AWS::ElasticLoadBalancingV2::LoadBalancer`, `aws_cognito_user_pool`→`AWS::Cognito::UserPool`). Those plus ~100 CFN-name aliases (`cluster`↔`DBCluster`, `plan`↔`BackupPlan`, `policy`↔`ScalingPolicy`) is a **bounded ~180-line hand file** that "should lift T1 coverage to roughly 70%" — **[U] an estimate extrapolated from the miss list, not measured. Do not promise it.** The remaining ~30% are genuinely CFN-less: association/attachment sub-resources that CloudFormation models as properties of a parent (`aws_appstream_fleet_stack_association`, `aws_autoscaling_attachment`, `aws_backup_vault_policy`, `aws_api_gateway_method_settings`).

**The systematic shape of the misses**, confirmed independently by both briefs: **Terraform splits into separate resources what CloudFormation folds into attributes.** Measured against installed SQS CRDs, the naive `AWS::{service}::{Kind}` join hits 2 of 4: `Queue` ✓, `QueuePolicy` ✓, `QueueRedrivePolicy` ✗, `QueueRedriveAllowPolicy` ✗. [V] Those are not errors to hide — they are `unknown` entries to surface, and usually the parent's actions already cover them (`sqs:SetQueueAttributes` covers redrive policy).

### 3.4 Measured fidelity where the chain *does* complete — it leaks both ways [V]

"TF actual" = grepping `conn.<Method>(` in `hashicorp/terraform-provider-aws@main`; a **lower bound**, since paginator constructors never match that pattern.

| MR | CFN type | recall | precision | verdict |
|---|---|---:|---:|---|
| `sqs…/Queue` → `aws_sqs_queue` | `AWS::SQS::Queue` | 100% | 88% | 100% mechanical; only `sqs:GetQueueUrl` surplus |
| `iam…/Role` → `aws_iam_role` | `AWS::IAM::Role` | 89% | 94% | mechanical, known 2-action hole: CFN **misses** `iam:RemoveRoleFromInstanceProfile` and `iam:ListInstanceProfilesForRole`, both called on delete with `force_detach_policies` |
| `rds…/Cluster` → `aws_rds_cluster` | `AWS::RDS::DBCluster` | 87% | **57%** | misses `rds:ListTagsForResource`, `rds:PromoteReadReplicaDBCluster`; **over-grants** `rds:CreateDBInstance/DeleteDBInstance/ModifyDBInstance/CreateDBClusterSnapshot/…` because CFN's DBCluster handler also manages instances and Terraform's does not |
| `ec2…/Instance` → `aws_instance` | `AWS::EC2::Instance` | **69%** | 65% | worst of the five; misses `ec2:GetPasswordData`, `DescribeTags`, `ModifyVolume`, `Assign/UnassignPrivateIpAddresses`, `ModifyInstanceCpuOptions`, `ModifyNetworkInterfaceAttribute`, `ModifyInstanceCapacityReservationAttributes`, `CancelSpotInstanceRequests`; adds `ssm:*` |
| `s3…/Bucket` → `aws_s3_bucket` | `AWS::S3::Bucket` | — | — | **structural mismatch** |

**Mean recall ≈ 86%, mean precision ≈ 76%.**

**The S3 case is a category error and the failure mode the UI must be honest about.** CFN union CRUD = **71 actions** across `s3`, `s3tables`, `iam`, because `AWS::S3::Bucket` is a mega-resource covering versioning, replication, lifecycle, notifications, CORS, logging, inventory, metrics, ownership controls and object lock — whereas Terraform (provider v4+) split those into ~20 separate resources. An MR of Kind `Bucket` would be handed 71 actions when it needs roughly `s3:CreateBucket, DeleteBucket, ListBucket, GetBucketTagging, PutBucketTagging, GetBucketAcl`. *Mitigation: flag any resource whose CFN action count exceeds ~2× the service median as "over-broad, review".*

**Verdict: (b) — derivable as an approximate starting point a human must review.** Not high-fidelity (both directions leak on every comparable resource; nothing here can be called least-privilege), and not hand-curation-only (over half the corpus resolves with zero human input, and hand-curating 1,029 resources would be worse *and* staler — `aws-leastprivilege`/`cfnlp` reached **12 of ~1,700 CFN types**, ~1%, and is still flagged "WORK IN PROGRESS"). [V/D]

### 3.5 The read side, tags, and the hole you must patch

Upjet reconciles by calling the Terraform Read/Refresh path on **every** reconcile loop for **every** MR — so read permissions are hit continuously, not once. Getting them wrong is the *silent* failure (`Synced=False` / flapping), not a loud create error. **Read actions must never be trimmed.** This is also where `cfnlp`'s `--include-update-actions` opt-in does **not** transfer: Crossplane reconciles continuously, unlike a one-shot CFN deploy.

**The systematic hole — VERIFIED.** `AWS::RDS::DBCluster` has `handlers.read.permissions == ["rds:DescribeDBClusters"]` and `tagging.permissions == ["rds:AddTagsToResource","rds:RemoveTagsFromResource"]` — **`rds:ListTagsForResource` appears in neither**, yet `internal/service/rds/tags_gen.go` calls it on every read. An MR built from CFN's read set alone fails tag drift detection.

**Rule to implement:**
```
read_set = handlers.read.permissions ∪ tagging.permissions
         ∪ { actions matching ^List.*Tags|^ListTagsFor|^GetResources$ with IsWrite == false }
```

**Baseline, independent of what is on the canvas [V]:** `hashicorp/aws-sdk-go-base` calls `stsClient.GetCallerIdentity` during provider configuration (`awsauth.go:179`), so **`sts:GetCallerIdentity` is required by every upjet-AWS provider** (unless `skip_requesting_account_id` is set), plus `sts:AssumeRoleWithWebIdentity` under IRSA [D].

### 3.6 Supporting datasets — what each is actually for

| dataset | what it gives | what it does **not** give |
|---|---|---|
| **AWS Service Reference** — index `https://servicereference.us-east-1.amazonaws.com/` (**455 services**), per service `…/v1/sqs/sqs.json` [V] | AWS's own machine-readable Service Authorization Reference; per-action flags `IsWrite`, `IsList`, `IsPermissionManagement`, `IsTaggingOnly` (Read = `!IsWrite && !IsList`). Replaces HTML scraping; best provenance of any option | no Terraform mapping, no resource-type index |
| **`iann0036/iam-dataset`** — **MIT**, © 2021 Ian Mckay [V] | `aws/iam_definition.json` 12.4 MB, **21,820 actions** with `access_level` ∈ {Read, Write, List, Tagging, Permissions management} + `resource_types[]` with `condition_keys` and `dependent_actions` and ARN templates; `aws/map.json` 7.4 MB, **19,514 SDK methods** → IAM actions; `gcp/permissions.json` **10,129 permissions** → containing roles | **no Terraform key anywhere** — the full tree (8,574 blobs, untruncated) has no Terraform directory. Its chain is SDK method → IAM action, and "which SDK methods does this TF resource call" is exactly the missing link |
| **`hashicorp/terraform-provider-aws` `names/data/names_data.hcl`** — **MPL-2.0**, 203 KB [V] | 373 `service` + 9 `sub_service` blocks; `resource_prefix.actual` is a **regex** and `arn_namespace` is exactly the IAM action prefix. **TF resource → IAM service prefix = 1027/1029 = 99.8%**; unmapped: `aws_lb`, `aws_route` (regex anchoring; 2 hand entries) | which *actions* within the service |
| **`GoogleCloudPlatform/magic-modules`** — Apache-2.0 [V fetched] | `mmv1/products/<svc>/product.yaml` + `<Resource>.yaml` give `base_url`, `update_url`, `create_verb`, `update_verb` → `pubsub.topics.{create,get,update,delete}` deterministically. `terraform-provider-google` is *generated* from these, so the API surface is exact by construction | **untested for this purpose — the strongest untested lead** [U] |
| **`salesforce/policy_sentry`** — MIT-form, © 2019 Salesforce [V] | materially the same SAR corpus, restructured; the access-level taxonomy | no Terraform or CFN key; adds nothing over the two above |

**Checked and dismissed** — all require the resource to already exist and be exercised, which is structurally wrong for a design-time canvas: `iamlive` (MIT, 3,408 stars — proxies live SDK traffic, needs a real `terraform apply`), `iamzero` (reactive, matches access-denied errors, effectively abandoned), `airiam`, `trailscraper`, `iann0036/aws-leastprivilege` runtime mode, IAM Access Analyzer policy generation (needs up to 90 days of CloudTrail). `cloudsplaining` and `tfsec`/`checkov` consume policies rather than producing them. **Upbound and Crossplane publish nothing** — no permissions field in package, MRD, or CRD; the Upbound IRSA page says only "attach the necessary AWS permissions to this role". `awslabs/crossplane-on-eks` has 10 hand-written policy compositions (`s3-read`, `sqs-write`, …) which are **workload** IAM, not control-plane IAM. [V]

Terraform's own provider source declares nothing about IAM — the only signal is literal SDK call sites, and extracting them is Go static analysis over ~200 packages complicated by paginator constructors. [V] Upstream confirms the gap: [terraform-provider-aws#32823](https://github.com/hashicorp/terraform-provider-aws/issues/32823), "Generate a list of least permissions required to provision a stack", is an **open** enhancement request. [D]

### 3.7 Licences

| dataset | licence | usable by an MIT/Apache Go tool? |
|---|---|---|
| **CFN registry schemas** | **No licence file in the zip** [V — 1,722 files, none carries a header]. Each schema's `sourceUrl` points at `github.com/aws-cloudformation/aws-cloudformation-resource-providers-<svc>`, which are **Apache-2.0** [D] | **Yes, with care.** Safest posture: fetch at *build* time in `go:generate` and cache, or fetch at runtime with a bundled fallback — rather than vendoring the zip verbatim. Extracting only `{typeName → {op → [actions]}}` is factual data, the least copyrightable part |
| **AWS Service Reference** | AWS service data over a public endpoint, no licence file | **Yes.** Same fetch-don't-vendor posture. Best provenance of any option |
| **`iann0036/iam-dataset`** | **MIT** [V — `LICENSE`, © 2021 Ian Mckay] | **Yes**, redistribution included, with attribution |
| **`salesforce/policy_sentry`** | MIT-form [V — "Permission is hereby granted, free of charge…", © 2019 Salesforce.com] | Yes — but adds nothing over the two above |
| **`terraform-provider-aws` `names_data.hcl`** | **MPL-2.0** [V — `SPDX-License-Identifier: MPL-2.0` header] | **Care needed.** MPL-2.0 is *file-level* copyleft: shipping the file or a modified copy keeps that file under MPL and obliges source availability for it. **Cleanest path: derive the same TF-prefix → IAM-prefix table from `iam-dataset` (MIT) service prefixes plus `config/generated.lst`, and use `names_data.hcl` only to spot-check.** Alternatively isolate the derived table in its own MPL-licensed file with the header preserved |
| **`crossplane-contrib/provider-upjet-*`** (`generated.lst`, `zz_*_terraformed.go`) | **Apache-2.0** [V — `meta.crossplane.io/license: Apache-2.0`] | **Yes**, with NOTICE attribution |
| **`GoogleCloudPlatform/magic-modules`** | **Apache-2.0** [V — header in `mmv1/products/pubsub/product.yaml`] | **Yes**, with attribution |

### 3.8 GCP — materially weaker, and the two briefs measure different things

- **Naive convention** `google_<svc>_<res>` → `<svc>.<plural(res)>.{create,get,update,delete}` over the 406 `provider-upjet-gcp` TF resources [V]: full (≥3 of 4 CRUD verbs resolve) **165 = 40.6%**, partial 4 = 1.0%, no match 237 = 58.4%.
- **`iam-dataset/gcp/permissions.json`** (MIT) [V]: 10,129 permissions → containing roles, so the *minimum predefined role* is computable by set intersection. Executed demo: `pubsub.topics.create` → 25 roles, `.get` → 41, `.update` → 18, `.delete` → 20, `.getIamPolicy` → 12; roles granting all five = `roles/pubsub.admin`, `roles/owner`, + 5 service-agent roles; filtering `roles/owner` and `*serviceAgent` yields **`roles/pubsub.admin`** — clean, mechanical, correct.
- **Magic Modules is the right source and it is untested.** [U]

> **Discrepancy noted, not a conflict.** `perm-prior-art.md` cites GCP coverage as **59.1%** (`gcp/map.json`, 2.65 MB, 290 services / 10,464 API **methods**, 6,185 carrying permission data). `perm-cloud-iam.md` cites **40.6%** (naive convention over 406 upjet-gcp **TF resources**). These measure different objects: 59.1% is method-level coverage of a method-keyed map; 40.6% is the only end-to-end, resource-keyed number. **Use 40.6% for planning; it is the one that describes what a canvas node would actually get.** Both briefs agree GCP is a lower-confidence, later concern and that the UI must say so.

---

## 4. Design recommendation

### 4.1 What compositionfactory emits, and where it lives on disk

**Forced constraint: the permissions artifact cannot ship inside the Configuration package.** [V] `crossplane xpkg build` **hard-fails** on a package root containing the doc-canonical ClusterRole:

```
crossplane: error: failed to build package: failed to parse package: .../rbac.yaml position:0:
no kind "ClusterRole" is registered for version "rbac.authorization.k8s.io/v1" in scheme "pkg/runtime/scheme.go:111"
```

A parse failure, not a warning. And there is **no `permissionRequests` field** anywhere on `configurations` / `configurationrevisions` / `providerrevisions.pkg.crossplane.io` in v2.4.0 — all three CRD schemas dumped, permission-key list empty for every served version. [V] So the UX is "here are files to commit", never "it ships with your Configuration".

```
<composition-dir>/
  composition.yaml
  definition.yaml
  functions.yaml
  permissions/
    rbac.yaml              # ClusterRole. v1.
    iam-controlplane.json  # provider credential policy, per provider. v2 (M6).
    permissions.lock.json  # provenance sidecar: per-entry tier + source + node ids
    overrides.yaml         # user suppressions/additions. Hand-edited, never regenerated.
```

Four rules the layout encodes:

- **`rbac.yaml` is a single aggregating ClusterRole and nothing else** — no RoleBinding, because aggregation *is* the binding (this is the one place we deliberately diverge from `audit2rbac`, which emits Role+RoleBinding together).
- **`permissions.lock.json` keeps machine-readable provenance out of the apply-able artifacts**, so `rbac.yaml` stays clean YAML a reviewer can read and `kubectl apply -f`.
- **`overrides.yaml` is non-negotiable.** Every mature tool in this space has one — `cloudsplaining`'s `exclusions.yml` on the stated rationale *"Only you know the context behind the design of your AWS infrastructure"*, `policy_sentry`'s `exclude-actions` / `skip-resource-constraints` / `wildcard-only`. The generator must not be the final authority.
- **Never emit a default `kustomization.yaml` into this directory** (design spec §8) — it flips ArgoCD from Directory to Kustomize, after which any file absent from `resources:` is **deleted** under `prune: true`.

Stamp generated objects with identifying labels so they are recognisable and safely regenerable, per `audit2rbac`:

```yaml
labels:
  rbac.crossplane.io/aggregate-to-crossplane: "true"
  compositionfactory.io/generated: "true"
  compositionfactory.io/source-composition: <name>
```

### 4.2 Attribution per canvas node

**RBAC — one un-merged rule per node, plus a YAML comment.** RBAC rules have no per-rule metadata field, so attribution goes in comments; comments survive the file and the git diff and are stripped by `kubectl apply` — acceptable, since the reviewer is the audience, not the API server. **This is also why provenance must be comments and never annotations** (design spec §8: an annotation on a GitOps-managed object creates a perpetual sync loop).

```yaml
# node: worker-job  (batch/v1 Job)  · state: needs-rule · verified against cluster 2026-08-28
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

Because RBAC is an additive union with no deny, N un-merged rules are semantically identical to the merged form — **attribution costs nothing.** `controller-gen`, the canonical static RBAC generator, merges and alphabetises and its output contains **zero** trace of which marker produced which rule [V]; that is the gap we fill. Keep `controller-gen`'s one genuinely good habit — alphabetised apiGroups/resources/verbs and stable rule ordering — and reject its merging.

**IAM — `Sid` per node per lifecycle phase**, which is where both `cfnlp` (`"Sid": "LambdaFunction-Create1"`, `"AccessAnalyzer-Create1-reg"` — the `-reg` suffix marking registry-derived rather than curated) and `policy_sentry` (`"Sid": "SsmReadParameter"`) independently landed. **Constraint: IAM `Sid` accepts alphanumerics only** — no hyphens, dots, underscores. So `sqs-queue` → `SqsQueueCreate1`, and keep a node-id ↔ Sid map in `permissions.lock.json` rather than assuming round-tripping. Offer a consolidate toggle that merges statements and strips Sids, matching both prior-art tools' `--consolidate-policy`.

**GUI:** the artifact panel and the canvas share one selection model. Selecting a node highlights its rules/statements; selecting a rule highlights the contributing node(s). The panel header is the sentence the user wants: *"these 4 nodes require these 11 actions"*, each line badged by tier.

### 4.3 How uncertainty is surfaced — a wrong policy is worse than none

Three tiers, modelled on IAM Access Analyzer's two plus `cfnlp`'s third, with per-entry provenance in the manner of `iam-dataset` (whose GCP map tags every permission `manual` 3,849 / `restcrawliamblockv1` 2,605 / `restcrawlv1` 1,828 / `fuzzv1` 18, and where **778 entries carrying two methodologies are treated as corroborated**). [V/D]

| Tier | Meaning | Source | UI |
|---|---|---|---|
| **verified** | Confirmed against the target system, or AWS-authored | `SubjectAccessReview` yes/no; CFN handler permissions (T1) | plain |
| **inferred** | Derived by heuristic | fuzzy CFN name join (T1b); SAR verb heuristic (T3/T4); service-scope fallback (T5); `guess()`-derived plural | badged, tooltip naming the exact rule applied |
| **unknown** | No mapping found | 145 CFN types without handlers; TF-granularity kinds like `QueueRedrivePolicy`; T0/T6 | **rendered as an actionable to-do list, never dropped** |

Six non-negotiables:

1. **Split the k8s `verified` tier into `already-satisfied` vs `needs-granting`** from the live `SubjectAccessReview`. This is the differentiator no other tool has. **Degrade to `inferred` when no cluster is reachable — and say so, rather than pretending.**
2. **Never silently omit an `unknown`.** Access Analyzer renders its unknown tier as an interactive to-do list — *"Information about which actions were used might not be available for the services listed in this section. Use the menus for each service listed to manually choose the actions…"* — not as an omission. Copy that. [D]
3. **Never emit a silent wildcard.** If the derivation cannot narrow a statement, it is an `unknown` entry with a visible reason. That is the entire difference between this feature and `AdministratorAccess`.
4. **Emit `Resource: "*"` with a visible warning rather than fabricating ARNs.** Access Analyzer leaves resource ARNs as explicit placeholders; `iam_definition.json` carries ARN templates per resource type (MIT) if we later narrow them.
5. **Publish our blind spots the way AWS publishes `iam:PassRole` is not tracked by CloudTrail.** Ours today: cross-resource actions are not derivable from resource schemas; S3-style mega-resources over-grant (flag any resource whose CFN action count exceeds ~2× the service median); Terraform-granularity kinds have no CFN equivalent; GCP is 40.6% end-to-end; CRDs may declare an arbitrary plural.
6. **A header comment on every generated file** saying it is a starting point for review, not an authority — and, for `rbac.yaml`, carrying the `Informer to sync` timeout string with the note that it means a missing ClusterRole.

**Do not inherit the industry's over-grant bias.** Every tool surveyed biases toward over-granting to avoid the visible annoyance (`iamlive --force-wildcard-resource`, `rbac-tool gen`'s everything-except, and **Crossplane's own doc example using `verbs: ["*"]`**). For the k8s half we can be exact *and* safe because we can verify. For the cloud half, Crossplane's continuous reconciliation means a missing read/`Describe` action produces a permanently degraded resource rather than a clean failure — **so read actions must never be trimmed, even though that costs precision.**

### 4.4 Frame the problem on two axes — the decision everything else depends on

|  | **Control-plane** (what the provisioner needs) | **Workload** (what the running app needs) |
|---|---|---|
| **Kubernetes** | **ClusterRole for the Crossplane SA — §2. Ship first.** | Roles/SAs for the composed app |
| **Cloud** | **IAM for the provider's credentials (`sqs:CreateQueue`) — §3. Second.** | IAM for the consumer (`sqs:SendMessage`) |

**Both bottom cells are composition *content*, not side artifacts.** A workload IAM policy belongs on the canvas as an `iam.aws.upbound.io/Policy` or `sqs.aws.upbound.io/QueuePolicy` **node** — which is exactly what `crossplane-on-eks` hand-writes today. Emitting them as files would put permissions in two places and desynchronise them.

The authoring model for that cell already exists and is worth copying wholesale: **AWS SAM connectors attach permissions to *edges*, not nodes** — `Source` + `Destination` + `Permissions: [Read, Write]`, where Lambda→SQS `Read` expands to `[sqs:ReceiveMessage, sqs:GetQueueAttributes]` and `Write` to `[sqs:DeleteMessage, sqs:SendMessage, sqs:ChangeMessageVisibility, sqs:PurgeQueue]`, with ARN templating via explicit `%{Destination.Arn}` placeholders. [D] **For a node-graph GUI this is the key structural insight: workload permissions are a property of the connection between two nodes, and a canvas is the natural authoring surface for exactly that.** Note that SAM's attachment target varies per pair — sometimes an identity policy on the source's role, sometimes a **resource policy on the destination** (`AWS::SQS::QueuePolicy`, `AWS::SNS::TopicPolicy`, `AWS::Lambda::Permission`) — which in our world means emitting a composed `QueuePolicy` node, not a side-car document. AWS itself curates ~78 pairs over ~15 service types and grows by GitHub issue: **curate the edges, derive the nodes.**

---

## 5. Scope call

The asymmetry is stark enough to be the plan: **RBAC is one vendored lookup table away from done; IAM is a dataset, a licence review, a hand-maintained alias file, and a permanent staleness obligation.**

### v1 — M5, as the design spec already sequences it

**Kubernetes control-plane RBAC + `SubjectAccessReview` verification.**

Why it is cheap: the `(group, version, kind) → (plural, namespaced)` table comes from the **same vendored OpenAPI v3 data the schema store already carries** — no new dependency, no new licence, no network fetch, no hand-curation, no staleness. The verb set and label are fixed constants. Determinism falls out of sorting. The whole feature is a pure function of the canvas plus one vendored table.

Why it is valuable: it fixes a **quantified 71% silent-failure rate** on common composable kinds, the failure it fixes currently misdirects operators toward network debugging, and **no prior art competes** — the niche is empty three ways over.

Ships in v1:
- `emit: {rbac: true}` → `permissions/rbac.yaml`, one aggregated ClusterRole per XRD, one un-merged rule per node, attributed by YAML comment, deterministically sorted.
- The four per-GVK states (§2.10) in the Permissions panel.
- Drag-time block on cluster-scoped kinds under a Namespaced XR (§2.9).
- `SubjectAccessReview` verification when a kubeconfig is reachable; graceful, *stated* degradation to `inferred` when not.
- `permissions.lock.json` and `overrides.yaml` — the *mechanism* must exist in v1 even though only RBAC populates it, so M6 does not have to retrofit a provenance model into shipped artifacts.
- The `Informer to sync` mapping surfaced verbatim in the GUI.

### v2 — M6, and gated

**AWS control-plane IAM**, behind `emit: {iam: true}`, default off, shipping only when all four of these exist:

1. the `go:generate` extractor over `zz_<kind>_terraformed.go` (Apache-2.0, exact, 1,350 entries);
2. the ~180-entry alias file, **with its actual measured coverage published** rather than the 70% estimate;
3. the three-tier badge UI *and* the `unknown` to-do list — **IAM must not ship with tiers as a footnote**;
4. a resolved licence posture on the CFN zip (build-time fetch, not vendored).

Design-spec Open Question 3 asked whether a ~70%-complete policy is useful or actively misleading. **The answer this synthesis supports: useful only if the 53%/70% split is visible per statement and the remaining fraction is a to-do list.** A 70% policy presented as a policy is a liability; a 70% policy presented as 53% verified + 17% inferred + 30% to-do is a genuine time-saver on an uncontested gap.

### Later, in order

3. **Workload permissions as canvas edges** → composed `Policy`/`QueuePolicy` MR nodes, SAM-connector style, curated per pair, growing by request as AWS's own does.
4. **GCP**, explicitly lower-confidence, and only after Magic Modules is actually tested (§3.8).

### Explicitly not doing

- **Hand-curating per-resource action lists.** `cfnlp` reached 12 of ~1,700 CFN types (~1%) and is still "WORK IN PROGRESS"; SAM connectors cover ~78 pairs over ~15 services and grow by GitHub issue. **Derive from data; curate only overrides and edges.**
- **Anything runtime-observed.** `iamlive`, `iamzero`, `audit2rbac`, `rbac-tool auditgen`, Access Analyzer, CloudTrail-derived tooling all require the resource to already exist and be exercised. compositionfactory operates before anything is applied — that is the whole point.
- **Go static analysis over Terraform provider SDK call sites.** Feasible as a research project, not as a feature in a node-graph editor.

---

## 6. Open questions

1. **Which verb template for composed objects — the seven explicit verbs with no subresources, or `["*"]` + `/status` + `update` on `/finalizers`?** The briefs disagree (§2.5), and it is decidable by experiment: apply the minimal role, compose a `Job` and an `Ingress`, and check both that reconciliation succeeds *and* that no `RoleBasedAccessControl` warning appears over several reconcile cycles. **Blocking for M5.** Related: does emitting `/status` on a composed object cause any harm beyond noise?

2. **One rule per apiGroup, or one rule per node?** (§2.6.) The proposed reconciliation — one rule per node, sorted by `(apiGroup, first resource, node id)` — needs a golden test proving byte-stability across regeneration and node reordering. **Blocking for M5.**

3. **Does `cf` read a cluster?** This is design-spec Open Question 2, and the permissions feature sharpens it: `SubjectAccessReview` is the single strongest differentiator in the whole feature, and it requires `client-go` plus rights the operator may not have. Is cluster access a first-class optional adapter, or does the RBAC panel ship inferred-only in v1?

4. **What is the alias table's real coverage, and who owns it?** The ~70% figure is an estimate extrapolated from a miss list [U]. Someone must measure it, and then own refreshing it against each `provider-upjet-aws` release. What is the staleness SLA, and what happens when a new provider version introduces resources the table does not know?

5. **What is the legal posture on the CFN zip?** No licence file in the archive; `sourceUrl` points at Apache-2.0 repos [D]. Build-time fetch with cache, runtime fetch with bundled fallback, or vendored extract? Needs an actual decision, not a preference.

6. **Can we avoid MPL-2.0 entirely?** The proposed path is to derive the TF-prefix → IAM-prefix table from `iam-dataset` (MIT) + `generated.lst` (Apache-2.0) and use `names_data.hcl` only to spot-check — but that path has not been measured against the 99.8% `names_data.hcl` achieves. If the MIT derivation is materially worse, is an isolated MPL file acceptable?

7. **Does Magic Modules actually deliver for GCP?** Flagged as the strongest untested lead [U]. Until measured, the only defensible GCP number is 40.6%.

8. **What does IAM derivation even mean for a non-upjet provider?** `provider-helm` and `provider-terraform` have no Terraform resource name to look up, and the design spec already names them as the real portability anchor (Open Question 1). Does the IAM panel render "not applicable", or does it have something to say?

9. **Should we emit companion `aggregate-to-view` / `aggregate-to-edit` ClusterRoles** so humans using `crossplane-view` / `crossplane-edit` can see the composed objects? Optional, and a distinct concern from making composition work — but it is nearly free once the GVK list exists.

10. **What happens if `--enable-operations` is turned on?** It is Alpha and defaults false, but if enabled, `Operations` need the same aggregated ClusterRole treatment [V]. Do we detect the flag, or always emit?

11. **How does the `blocked` state interact with `adopt`?** A foreign composition imported as an opaque node may compose a cluster-scoped object under a Namespaced XR, which is a hard fatal error — but the opaque node's GVKs are, by construction, not known to us.

---

## Negative results — each one is a finding, not a gap

- **[V]** No tool generates Kubernetes RBAC statically from workload manifests. Every generator derives from audit logs; `rbac-tool gen` is wildcard-expansion-with-denylist. The niche is empty. GitHub repository search for a Crossplane composition RBAC generator returns `total_count = 0` three different ways; code search for the label returns only crossplane and its forks.
- **[V]** The Terraform resource name is **not** recoverable from an installed CRD, a `ManagedResourceDefinition`, or a provider package's metadata. It exists only in the provider's Go source and its compiled binary.
- **[V]** No Terraform-resource → IAM-actions mapping exists to reuse. `iam-dataset`'s full tree (8,574 blobs) has no Terraform directory; HashiCorp publishes no per-resource IAM requirements, and its own issue asking for them (#32823) is open.
- **[V]** Neither Crossplane nor Upbound publishes the IAM a provider's credentials need, per resource or in aggregate. `crossplane-on-eks` ships 10 hand-written **workload** policies and zero control-plane policies. The de facto community answer is `AdministratorAccess`.
- **[V]** A Configuration package cannot carry a ClusterRole (`xpkg build` parse failure), and no `permissionRequests` field exists on any `Configuration` / `ConfigurationRevision` / `ProviderRevision` version in v2.4.0. There is no in-package delivery path.
- **[V]** There is **no** `crossplane core start` flag that auto-grants composed-resource RBAC — checked exhaustively against the flag struct. The live deployment runs with no feature flags at all.
- **[V]** `crossplane:allowed-provider-permissions`, the provider permission ceiling, currently resolves to **zero rules** — its selector matches nothing.
- **[D]** Hand-curation does not scale: `cfnlp` reached 12 of ~1,700 CFN types; SAM connectors cover ~78 pairs and grow by GitHub issue.

---

## Sources

**Kubernetes / Crossplane**
- [Compositions · Crossplane v2.4](https://docs.crossplane.io/latest/composition/compositions/) — "Grant access to composed resources"
- [Server-Side Apply | Kubernetes](https://kubernetes.io/docs/reference/using-api/server-side-apply/) · [Using RBAC Authorization | Kubernetes](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [crossplane/crossplane#7398](https://github.com/crossplane/crossplane/issues/7398) (OPEN) — RBAC denial surfaces as informer-sync timeout
- [discussion #4932](https://github.com/crossplane/crossplane/discussions/4932) · [#2084](https://github.com/crossplane/crossplane/issues/2084) · [#1637](https://github.com/crossplane/crossplane/issues/1637)
- [design-doc-rbac-manager.md](https://github.com/crossplane/crossplane/blob/main/design/design-doc-rbac-manager.md)
- Source read at tag `v2.4.0`: `internal/controller/apiextensions/composite/{composition_functions.go,reconciler.go,errors.go}`, `internal/engine/{engine.go,errors.go}`, `cmd/crossplane/core/core.go`
- [awslabs/crossplane-on-eks](https://github.com/awslabs/crossplane-on-eks) · [Enhancing Security Practices with Crossplane Providers](https://blog.crossplane.io/enhancing-security-practices-with-crossplane-providers/)

**Cloud IAM datasets**
- [AWS CloudFormation registry schemas (zip)](https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip)
- [AWS Service Reference Information](https://servicereference.us-east-1.amazonaws.com/) · [AWS Service Authorization Reference](https://docs.aws.amazon.com/service-authorization/latest/reference/reference.html)
- [iann0036/iam-dataset](https://github.com/iann0036/iam-dataset) · [MAP-README](https://github.com/iann0036/iam-dataset/blob/main/aws/MAP-README.md) · [iamlive](https://github.com/iann0036/iamlive) · [aws-leastprivilege/cfnlp](https://github.com/iann0036/aws-leastprivilege)
- [salesforce/policy_sentry](https://github.com/salesforce/policy_sentry) · [salesforce/cloudsplaining](https://github.com/salesforce/cloudsplaining) · [common-fate/iamzero](https://github.com/common-fate/iamzero)
- [crossplane-contrib/provider-upjet-aws](https://github.com/crossplane-contrib/provider-upjet-aws) · [config/generated.lst](https://raw.githubusercontent.com/crossplane-contrib/provider-upjet-aws/main/config/generated.lst) · [provider-upjet-gcp](https://github.com/crossplane-contrib/provider-upjet-gcp)
- [terraform-provider-aws names_data.hcl](https://raw.githubusercontent.com/hashicorp/terraform-provider-aws/main/names/data/names_data.hcl) · [terraform-provider-aws#32823](https://github.com/hashicorp/terraform-provider-aws/issues/32823) (open)
- [GoogleCloudPlatform/magic-modules](https://github.com/GoogleCloudPlatform/magic-modules) · [Upbound AWS IRSA authentication](https://docs.upbound.io/manuals/packages/providers/aws-auth/aws-irsa/)
- [Streamlining IAM permission discovery with CloudFormation resource schemas](https://dev.to/quixoticmonk/streamlining-iam-permission-discovery-with-cloudformation-resource-schemas-4lpg)

**Presentation prior art**
- [IAM Access Analyzer policy generation](https://docs.aws.amazon.com/IAM/latest/UserGuide/access-analyzer-policy-generation.html) · [SAM connector reference](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/reference-sam-connector.html) · [Infrastructure Composer FAQs](https://aws.amazon.com/infrastructure-composer/faqs/)
- [audit2rbac](https://github.com/liggitt/audit2rbac) · [rbac-tool](https://github.com/alcideio/rbac-tool) · [kubectl-who-can](https://github.com/aquasecurity/kubectl-who-can) · [krane](https://github.com/appvia/krane) · [rakkess](https://github.com/corneliusweig/rakkess) · [rbac.dev](https://rbac.dev/) · `controller-gen` v0.16.5
