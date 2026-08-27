# Kubernetes RBAC that Crossplane v2 needs to compose native objects

Research brief for the compositionfactory "permissions panel" feature.
Environment: Crossplane **v2.4.0** core + rbac-manager, kind cluster `kind-platform`, k8s **v1.36.1**.
Every VERIFIED claim was run against that live cluster (read-only: `get`, `auth can-i`, `--dry-run=server`) or read in `crossplane/crossplane` @ tag `v2.4.0`.

---

## Decisions this enables

1. **Emit exactly one aggregated `ClusterRole` per XRD, not per Composition.** The label is `rbac.crossplane.io/aggregate-to-crossplane: "true"` (exact key, exact string value `"true"` — VERIFIED). It must be a `ClusterRole`; a namespaced `Role` cannot work, for two independent reasons (aggregation only selects ClusterRoles, and Crossplane's informers are cluster-scoped by default). This is a real generator feature with no existing tool — a GitHub code search for the label returns **only crossplane itself and its forks** (VERIFIED).

2. **Emit all seven verbs `get,list,watch,create,update,patch,delete` — never a minimal subset.** Crossplane's own preflight authorizer literally loops over that exact seven-verb list and raises a warning if *any* is missing (VERIFIED in `internal/engine/engine.go`). A "minimal" role that omits `update` still works but permanently emits `RoleBasedAccessControl` warning events. `verbs: ["*"]` also satisfies it (the authorizer tries `*` first) and is what the Crossplane docs example uses.

3. **Do not emit `/status` subresource rules for composed objects.** Crossplane never writes a composed resource's status — every `client.Status().Patch/Update` call in the composite controller targets the XR itself, not `cd.Resource` (VERIFIED). Emitting `resources: ["deployments/status"]` is cargo-culted noise. (`/status` *is* needed for XRs and MRs, but rbac-manager already generates those.)

4. **The GUI should show a per-node traffic light, because ~5 native kinds already work and everything else silently hangs.** There is an *accidental allowlist* — Deployment, Service, Secret, ConfigMap, ServiceAccount and CRDs are composable with zero extra RBAC because core Crossplane needs them for package management. StatefulSet, DaemonSet, Job, CronJob, Ingress, NetworkPolicy, PVC, Role/RoleBinding, HPA, PDB and *every* third-party CRD are denied (VERIFIED by `auth can-i`, table below). This is exactly why the failure is under-reported: the two most common demo objects happen to be in the allowlist.

5. **Kind → resource-plural is fully solvable offline with zero heuristics.** Read the plural straight out of the OpenAPI v3 **path** (`/apis/apps/v1/namespaces/{namespace}/deployments`), filtering to paths that *end* at the plural. That rule yielded a correct plural for 177/177 real resource types and leaked zero subresource pseudo-kinds (VERIFIED). If you must guess from a bare Kind, Kubernetes' own `UnsafeGuessKindToResource` scored **148/148 on live discovery and 81/81 on installed CRDs** (VERIFIED) — the "irregular cases" in the task brief (Endpoints, NetworkPolicies, Ingresses) are all already handled by that 12-line algorithm.

---

## 1. The mechanism

### The label (VERIFIED)

The core `ClusterRole` named `crossplane` is bound by `ClusterRoleBinding/crossplane` to `ServiceAccount crossplane-system/crossplane`, and carries:

```json
"aggregationRule": {
  "clusterRoleSelectors": [
    { "matchLabels": { "rbac.crossplane.io/aggregate-to-crossplane": "true" } }
  ]
}
```

`matchLabels` is an exact string match: the value must be lowercase `"true"`, quoted so YAML does not coerce it to a boolean. `"True"`, `"yes"`, `""` will silently not aggregate.

Full inventory of `rbac.crossplane.io/*` label keys observed on the cluster (VERIFIED), all with value `true`:

| Label key | Aggregates into | Audience |
|---|---|---|
| `rbac.crossplane.io/aggregate-to-crossplane` | `ClusterRole/crossplane` | **the control plane itself — this is the one that matters** |
| `rbac.crossplane.io/aggregate-to-admin` / `-edit` / `-view` / `-browse` | `crossplane-admin` / `-edit` / `-view` / `-browse` | cluster-wide humans |
| `rbac.crossplane.io/aggregate-to-ns-admin` / `-ns-edit` / `-ns-view` | namespaced user roles | per-namespace humans |
| `rbac.crossplane.io/aggregate-to-allowed-provider-permissions` | `crossplane:allowed-provider-permissions` | provider permission boundary |
| `rbac.crossplane.io/system` | (marker, value = provider revision name) | provider system roles |

### What actually carries the label right now (VERIFIED)

```
crossplane:system:aggregate-to-crossplane                                  <- static, from the Helm chart
crossplane:composite:xmicroservices.sparky.ee:aggregate-to-crossplane      <- rbac-manager, per XRD
crossplane:composite:xqueues.platform.hooli.tech:aggregate-to-crossplane   <- rbac-manager, per XRD
crossplane:provider:provider-aws-sqs-<hash>:aggregate-to-edit              <- rbac-manager, per provider
crossplane:provider:upbound-provider-family-aws-<hash>:aggregate-to-edit   <- rbac-manager, per provider
```

Note the provider roles are named `aggregate-to-edit` but **also** carry `aggregate-to-crossplane: "true"` — that is how the MR API groups reach the control plane's own role.

### The critical gap (VERIFIED)

The per-XRD role that rbac-manager generates grants rights on **the XR and nothing else**:

```json
// crossplane:composite:xqueues.platform.hooli.tech:aggregate-to-crossplane
[
 {"apiGroups":["platform.hooli.tech"],"resources":["xqueues","xqueues/status"],"verbs":["*"]},
 {"apiGroups":["platform.hooli.tech"],"resources":["xqueues/finalizers"],"verbs":["update"]}
]
```

It is owned (`ownerReferences`) by the `CompositeResourceDefinition`. **rbac-manager never reads the Composition**, so it has no idea which composed GVKs the pipeline will produce. That is the entire hole this feature fills.

### rbac-manager runtime (VERIFIED)

```
image: xpkg.crossplane.io/crossplane/crossplane:v2.4.0
args:  ["rbac","start","--provider-clusterrole=crossplane:allowed-provider-permissions"]
sa:    crossplane-system/rbac-manager
```
Its own ClusterRole holds `clusterroles/roles: [get,list,watch,create,update,patch,escalate]` and `clusterroles: [bind]` — the `escalate` verb is how it grants Crossplane rights it does not itself hold.

### The accidental allowlist (VERIFIED — `kubectl auth can-i --as=system:serviceaccount:crossplane-system:crossplane`)

`ClusterRole/crossplane` resolves to 16 aggregated rules. Rules 8–15 come from the static `crossplane:system:aggregate-to-crossplane` role and exist for *package management*, but they incidentally authorize composition:

| Composed GVK | create | get | list | watch | patch | Net result |
|---|---|---|---|---|---|---|
| `apps/v1 Deployment` | yes | yes | yes | yes | yes | **works out of the box** |
| `v1 Service` | yes | yes | yes | yes | yes | **works** |
| `v1 Secret` | yes | yes | yes | yes | yes | **works** |
| `v1 ConfigMap` | yes | yes | yes | yes | yes | **works** |
| `v1 ServiceAccount` | yes | yes | yes | yes | yes | **works** |
| `apiextensions.k8s.io CustomResourceDefinition` | yes | yes | yes | yes | yes | works |
| `v1 Event` | yes | **no** | **no** | **no** | yes | **partial — informer fails** |
| `apps/v1 StatefulSet` | no | no | no | no | no | denied |
| `apps/v1 DaemonSet` / `ReplicaSet` | no | no | no | no | no | denied |
| `v1 Pod` / `PersistentVolumeClaim` / `Namespace` / `Endpoints` | no | no | no | no | no | denied |
| `batch/v1 Job` / `CronJob` | no | no | no | no | no | denied |
| `networking.k8s.io/v1 Ingress` / `NetworkPolicy` | no | no | no | no | no | denied |
| `rbac.authorization.k8s.io/v1 Role` / `RoleBinding` / `ClusterRole` | no | no | no | no | no | denied |
| `autoscaling/v1 HorizontalPodAutoscaler` | no | no | no | no | no | denied |
| `policy/v1 PodDisruptionBudget` | no | no | no | no | no | denied |
| `cert-manager.io/v1 Certificate` / `Issuer` | no | no | no | no | no | denied |
| `argoproj.io/v1alpha1 Application` / `Rollout` | no | no | no | no | no | denied |
| `sqs.aws.(m.)upbound.io Queue` | yes | yes | yes | yes | yes | works (rbac-manager) |

`Event` is the instructive case: a **partial** grant. It passes the `create`, fails the informer. A generator must reason at verb granularity, not "is this apiGroup mentioned anywhere".

**Live corroboration (VERIFIED):** the cluster's working `xmicroservices.kubernetes.sparky.ee` Composition composes exactly `apps/v1 Deployment` + `v1 Service` — both in the accidental allowlist — and all 10 of its XRs report `SYNCED=True READY=True`. It has never needed an extra ClusterRole, which is precisely why the problem stays invisible until someone drags an Ingress or a Job onto the canvas.

---

## 2. What breaks without it

### The apply path (VERIFIED, `composition_functions.go:689`)

```go
if err := c.client.Patch(ctx, cd.Resource, client.Apply, client.ForceOwnership,
        client.FieldOwner(ComposedFieldOwnerName(xr))); err != nil {
```

Composed resources are written with **server-side apply**, field owner `apiextensions.crossplane.io/composed/<32-hex-of-xr>` (`FieldOwnerComposedPrefix` = `apiextensions.crossplane.io/composed`).

Consequence for the generator: SSA is a `PATCH`, so the authorizer checks **`patch`**, and creating a not-yet-existing object through apply *additionally* requires **`create`** (DOCS — [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/): apply needs `patch` to edit and `create` to create). Confirmed empirically that the denied verb reported is `patch`, not `create`.

### Exact error text (VERIFIED — captured live via `kubectl apply --server-side --dry-run=server --as=system:serviceaccount:crossplane-system:crossplane`, no persistent change)

```
Error from server (Forbidden): ingresses.networking.k8s.io "rbac-probe" is forbidden:
User "system:serviceaccount:crossplane-system:crossplane" cannot patch resource "ingresses"
in API group "networking.k8s.io" in the namespace "team-a"

Error from server (Forbidden): jobs.batch "rbac-probe" is forbidden:
User "system:serviceaccount:crossplane-system:crossplane" cannot patch resource "jobs"
in API group "batch" in the namespace "team-a"
```
Control: the same dry-run for `apps/v1 Deployment` returned `serverside-applied (server dry run)`.

### How it surfaces on the XR

`IsInvalid` is handled specially; **`Forbidden` is not**, so it falls through to a fatal `ComposedResourceError` (VERIFIED, `composition_functions.go:711`) and the reconciler marks `Synced=False` with reason `ReconcileError` (`reconciler.go:789`). Message strings (VERIFIED, `reconciler.go:76-78`, `composition_functions.go:70`):

```go
errCompose          = "cannot compose resources"
errFmtApplyCD       = "cannot apply composed resource %q"
errSyncResources    = "cannot sync composed resources"
errInvalidResources = "some resources were invalid, check events"
```

### v2.4 has a dedicated RBAC diagnostic (VERIFIED)

`reconciler.go:780-786` — when composition fails, an authorizer re-checks the offending GVK and emits a second, explicit event:

```go
reasonRBAC event.Reason = "RoleBasedAccessControl"
```
`internal/engine/errors.go:42` formats it as:
```
{user} is not allowed to [{denied verbs}] resource {plural}.{group}/{version} in namespace {ns}
```

### It is NOT silent — but it IS misleading after the first reconcile

This is the important nuance, and it is an **open bug in v2.4**: [crossplane/crossplane#7398](https://github.com/crossplane/crossplane/issues/7398) (DOCS — read the issue; state OPEN). The first reconcile reports the truth; every subsequent one reports an informer timeout, because the informer for the unauthorized GVK never started and the cached `Get` times out. Observed event timeline from that issue:

```
Warning ComposeResources        25m   cannot apply composed resource "team-idp-argocd-project":
                                      appprojects.argoproj.io "idp" is forbidden: User
                                      "system:serviceaccount:crossplane-system:crossplane" cannot
                                      patch resource "appprojects" in API group "argoproj.io" in
                                      the namespace "argocd"
Warning RoleBasedAccessControl  25m   system:serviceaccount:crossplane-system:crossplane is not
                                      allowed to [get,list,watch,create,update,patch,delete]
                                      resource appprojects.argoproj.io/v1alpha1 in namespace argocd
Warning ComposeResources        85s   (x11 over 23m) cannot compose resources: cannot get existing
                                      composed resources: cannot get composed resource: Timeout:
                                      failed waiting for *unstructured.Unstructured Informer to sync
```

Steady-state `Synced` condition an operator actually sees:

```yaml
- type: Synced
  status: "False"
  reason: ReconcileError
  message: |
    cannot compose resources: cannot get existing composed resources:
    cannot get composed resource: Timeout: failed waiting for
    *unstructured.Unstructured Informer to sync
```

**This is the "why nothing happens" signature.** It points at networking / API-server health, not RBAC. Worth surfacing verbatim in the GUI as "if you see this message, it means a missing ClusterRole" — that mapping is non-obvious enough that a maintainer-adjacent issue thread was needed to establish it.

---

## 3. Does v2 change this?

**No automatic escalation. The operator must grant it.** (VERIFIED + DOCS)

- v1 mostly composed MRs, whose CRDs arrived via a `Provider` package; rbac-manager watches `ProviderRevision.status.objectRefs` and generates `crossplane:provider:<rev>:system` + `:aggregate-to-edit` roles. That covered ~everything.
- v2 composes arbitrary native objects that **no package owns**, so rbac-manager has no trigger to generate anything.
- The v2.4 docs are explicit (DOCS — [Compositions · Crossplane v2.4](https://docs.crossplane.io/latest/composition/compositions/), section "Grant access to composed resources"): *"You must grant Crossplane access to compose any other kind of resource. You do this by creating an RBAC ClusterRole."* And: *"If you disable the RBAC manager, you must manually grant Crossplane access to any kind of resource you wish to compose - including XRs and MRs."*

The canonical docs example (DOCS, quoted verbatim) — note it uses `verbs: ["*"]`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cnpg:aggregate-to-crossplane
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
- apiGroups:
  - postgresql.cnpg.io
  resources:
  - clusters
  verbs:
  - "*"
```

What v2 *did* add is diagnosis, not automation: the `SelfSubjectAccessReview` authorizer + `RoleBasedAccessControl` event (§2).

### `--enable-*` flags on `crossplane core start` (VERIFIED, `cmd/crossplane/core/core.go`)

The live deployment runs with **no feature flags at all** (`args: ["core","start"]`). Defaults that matter:

| Flag | Default | Why the generator cares |
|---|---|---|
| `--enable-realtime-compositions` | **true** (Beta) | Starts an informer per composed GVK. **This is what makes `list` and `watch` mandatory, not optional.** |
| `--watch-cache-namespaced` | **false** | Informers are **cluster-wide**. A namespaced `Role` is insufficient even for a namespaced composed object. Forces `ClusterRole`. |
| `--enable-ssa-claims` | true (Beta) | — |
| `--enable-usages` | true (Beta) | — |
| `--enable-operations` | false (Alpha) | If enabled, `Operations` need the same aggregated ClusterRole treatment. |
| `--restrict-namespaced-events` | false | — |

There is **no** flag that auto-grants composed-resource RBAC. Negative result, checked exhaustively against the flag struct.

---

## 4. The exact ClusterRole to emit

Given composed GVKs `apps/v1 Deployment`, `v1 Service`, `v1 Secret`, `networking.k8s.io/v1 Ingress`:

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

VERIFIED schema-valid: `kubectl apply --server-side --dry-run=server` returned `serverside-applied (server dry run)`.

**Rules for the generator:**

- **`apiGroups`** — core group is the empty string `""`, never `"core"` or `"v1"`. Derive by splitting `apiVersion` on `/`: one segment → `""`, two → first segment.
- **`resources`** — lowercase plural, no group suffix (`deployments`, not `deployments.apps`). See §5.
- **Group the rules by apiGroup**, sorting resources within each. Deterministic output matters for GitOps diffs.
- **`verbs`** — emit all seven: `get, list, watch, create, update, patch, delete`.
  - *Functionally* required: `create` + `patch` (SSA), `get` + `list` + `watch` (realtime-composition informer), `delete` (composed-resource GC, `composition_functions.go:1005`).
  - `update` is **not** used by the composite controller (VERIFIED — composed objects are only ever `Patch`ed, `Get`, `Delete`d). But Crossplane's authorizer checks it, so omitting it produces permanent spurious `RoleBasedAccessControl` warnings. Include it.
  - `verbs: ["*"]` is equally acceptable and is what the docs example uses — the authorizer short-circuits on `*`. Offer it as a "broad" toggle; default to the explicit seven for least-privilege review.
- **Subresources: NO.** Do not emit `deployments/status`. VERIFIED: every `client.Status().Patch/Update` in the composite controller targets the XR, never `cd.Resource`. (`/status` and `/finalizers` *are* needed for XRs and MRs — but rbac-manager already generates those, so the generator must not duplicate them.)
- **Skip GVKs that rbac-manager already covers** — anything in a provider-owned API group (`*.upbound.io`, `*.crossplane.io`) and the XR's own group. Emitting them is harmless but noisy; better to show them in the GUI as "already granted by rbac-manager".
- **Consider also emitting `aggregate-to-view` / `aggregate-to-edit` companion roles** so humans using `crossplane-view`/`crossplane-edit` can actually see the composed objects. Optional, and a distinct concern from making composition work.

---

## 5. Kind → resource-plural without a cluster

### Best answer: read it from the OpenAPI v3 **paths** — no guessing at all (VERIFIED)

The plural is *not* in the schema `components` (the `Deployment` schema carries only `x-kubernetes-group-version-kind`, no plural). It **is** in the path structure. Algorithm:

1. Fetch/vendor `/openapi/v3` → per-group-version documents.
2. For each path, match the **collection** form — the path must *end* at the plural:
   ```
   ^/apis?/(?:([^/]+)/)?([^/]+)/(?:namespaces/\{namespace\}/)?([a-z0-9]+)$
   ```
3. Take the `post` operation's `x-kubernetes-group-version-kind` → that's the Kind; capture group 3 → that's the plural.

VERIFIED results on the live cluster's 74 group-version documents:
- 186 GVKs had a plural derivable from some path.
- Requiring the path to **end** at the plural removed all 9 subresource pseudo-kinds (`Eviction`→`pods`, `Scale`→`deployments`, `TokenRequest`→`serviceaccounts`, `PodExecOptions`/`PodAttachOptions`/`PodProxyOptions`/`PodPortForwardOptions`/`NodeProxyOptions`/`ServiceProxyOptions`→`pods`/`nodes`/`services`) with **zero** false negatives.
- On `core/v1` it yields exactly 16 composable kinds, `Endpoints → endpoints` included, with **no special-casing whatsoever**.

The `post` filter is a bonus: a Kind with no `post` on its collection path is not creatable, therefore not composable — the GUI can grey it out.

### Fallback: guess from a bare Kind

If all you have is a Kind string, use Kubernetes' own `UnsafeGuessKindToResource` (`k8s.io/apimachinery/pkg/api/meta`):

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

VERIFIED accuracy on this cluster:
- **148/148 (100%)** against live `kubectl api-resources` — core k8s, cert-manager, Argo CD, Argo Rollouts, Kargo, Crossplane, Upbound AWS MRs.
- **81/81 (100%)** against `CustomResourceDefinition.spec.names.plural` as *declared* by installed CRDs.

The task brief's suspected irregular cases are all handled by the three rules:

| Kind | Rule | Plural |
|---|---|---|
| `Endpoints` | `unpluralizedSuffixes` | `endpoints` (not `endpointses`) |
| `Ingress` | `s` → `es` | `ingresses` |
| `NetworkPolicy` | `y` → `ies` | `networkpolicies` |
| `PodSecurityPolicy` | `y` → `ies` | `podsecuritypolicies` (removed in k8s 1.25; not on this cluster) |
| `ComponentStatus` | `s` → `es` | `componentstatuses` |
| `IngressClass`, `StorageClass`, `PriorityClass` | `s` → `es` | `…classes` |
| `APIService` | default | `apiservices` |

**Known exceptions that did NOT appear on this cluster** (DOCS, flag as residual risk):
- `metrics.k8s.io/v1beta1`: Kind `NodeMetrics` → plural `nodes`, `PodMetrics` → `pods`. Genuine violation. Low risk: aggregated read-only API, not composable.
- CRDs *may* declare an arbitrary `spec.names.plural` unrelated to the Kind. Zero of 81 installed CRDs did so, but it is legal. **Mitigation: prefer the OpenAPI-path method, which reads the declared plural rather than guessing it.** Only fall back to `guess()` for a Kind you have no schema for, and mark such rows "unverified plural" in the GUI.

---

## 6. Namespaced vs cluster-scoped composed objects

VERIFIED (`composition_functions.go:551-586`, `865-880`, `1017-1027`) and confirmed live.

- **A Namespaced XR can compose ONLY namespaced objects.** Cluster-scoped composed resources are a hard, fatal error:
  ```go
  errFmtNamespacedXRClusterResource =
    "cannot apply cluster scoped composed resource %q (a %s named %s) for a namespaced composite resource."
  ```
  The check is `c.client.IsObjectNamespaced(cd)` — a RESTMapper lookup, so **the GUI can and should block this at drag time.** The `namespaced` boolean is in the same discovery/OpenAPI data as the plural.
- **The composed object inherits the XR's namespace, always.** If the template sets a *different* namespace it is silently overwritten, with a warning event:
  ```go
  errFmtNamespaceOverridden =
    "cannot create composed resource %q in namespace %q, using XR namespace %q instead"
  ```
  Also: a Namespaced XR's `resourceRefs` have `ref.Namespace` forcibly blanked — its OpenAPI schema does not permit a namespace there.
- **A Cluster-scoped XR may compose cluster-scoped objects *and* namespaced objects in arbitrary namespaces** (source comment at `:873`).
- Live confirmation: XR `dev1-0/podinfo` (`XMicroservice`, XRD `scope: Namespaced`) → `Deployment dev1-0/podinfo`, same namespace, `ownerReferences` → the XR, label `crossplane.io/composite: podinfo`, annotation `crossplane.io/composition-resource-name: deployment`.

### Does this change Role vs ClusterRole? **No — always ClusterRole.**

Two independent reasons, either sufficient:
1. **Aggregation only works on ClusterRoles.** `ClusterRole/crossplane`'s `aggregationRule` uses `clusterRoleSelectors`; there is no `roleSelectors` in the Kubernetes API. A `Role` can never aggregate in, and rbac-manager creates no `RoleBinding` for the Crossplane SA.
2. **Informers are cluster-wide.** `--watch-cache-namespaced` defaults `false` and is unset on this deployment, so the realtime-composition informer does a cluster-scoped `list`/`watch`. A namespace-scoped `Role` cannot satisfy that even for an object that only ever lives in one namespace.

So: **one aggregated `ClusterRole` per XRD, regardless of XR scope.** Namespace scope affects *validation* (block cluster-scoped kinds under a Namespaced XR) but never the *shape of the emitted RBAC*.

---

## 7. Prior art: does any tool generate this?

**No. Negative result, checked three ways.**

1. GitHub **repository** search — `crossplane rbac generate composition clusterrole`, `aggregate-to-crossplane generator`, `crossplane composition rbac generator`: **total_count = 0** for all three (VERIFIED).
2. GitHub **code** search for `aggregate-to-crossplane language:go`: **11 hits, all of them crossplane itself or forks** — `crossplane/crossplane`, `IBM/ibm-crossplane`, `turkenh/upbound-crossplane-experiment`, `gonzalezjp/crossplane`, `muvaf/upbound-crossplane` (all `internal/controller/rbac/...`), plus one provider (`rossigee/provider-discord`). No generator, no linter, no CLI (VERIFIED).
3. Crossplane's own issue tracker has adjacent-but-different work: #1637 "Generate RBAC roles for composite resources and claims" (that is the XR-side role rbac-manager already ships), #2084 "How to grant RBAC permissions for composition managed resources", discussion #4932. Community answers on #4932 recommend **binding `cluster-admin` to the Crossplane SA** — which is both the state of the art and an argument for building this properly.

**This is a genuinely unoccupied niche**, and it is the single highest-signal thing compositionfactory can emit alongside a Composition: it is derivable entirely statically from the composed GVKs the user dragged onto the canvas, it has a precise correct answer, and getting it wrong produces a failure mode that misdirects operators toward network debugging.

---

## Implementation notes for the generator

**Input** (already available from the canvas): the set of composed `(apiVersion, kind)` pairs, plus the XRD name and scope.

**Per GVK, decide one of four states:**

| State | Condition | GUI |
|---|---|---|
| `already-granted` | GVK in a provider-owned group, or the XR's own group | green, "granted by rbac-manager" |
| `accidentally-granted` | Deployment / Service / Secret / ConfigMap / ServiceAccount / CRD | green, "granted by core Crossplane"; still emit the rule for portability |
| `needs-rule` | anything else, all 7 verbs missing | amber, contributes a rule |
| `blocked` | cluster-scoped kind under a Namespaced XR | red, RBAC cannot help |

Verify `already-/accidentally-granted` at runtime with a `SelfSubjectAccessReview` when a cluster is reachable — that is exactly what Crossplane's own authorizer does, and it is read-only and safe.

**Offline data needed:** vendored OpenAPI v3 per group-version, from which you get `(group, version, kind) → (plural, namespaced)`. That is the same vendored schema data the tool already needs for the provider MR schemas, so this adds no new dependency.

**Determinism:** sort apiGroups, then resources within each rule. GitOps diffs.

---

## Sources

- [Compositions · Crossplane v2.4](https://docs.crossplane.io/latest/composition/compositions/) — "Grant access to composed resources"
- [Server-Side Apply | Kubernetes](https://kubernetes.io/docs/reference/using-api/server-side-apply/) — apply needs `patch` + `create`
- [Using RBAC Authorization | Kubernetes](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) — ClusterRole aggregation
- [crossplane/crossplane#7398](https://github.com/crossplane/crossplane/issues/7398) — RBAC denial surfaces as informer-sync timeout (OPEN)
- [crossplane/crossplane discussion #4932](https://github.com/crossplane/crossplane/discussions/4932) — "RBAC doesn't allow creation of child resources of a Composition"
- [crossplane/crossplane#2084](https://github.com/crossplane/crossplane/issues/2084) — granting RBAC for composition managed resources
- [crossplane/crossplane#1637](https://github.com/crossplane/crossplane/issues/1637) — generate RBAC roles for XRs and claims
- [design-doc-rbac-manager.md](https://github.com/crossplane/crossplane/blob/main/design/design-doc-rbac-manager.md)
- Source read at tag `v2.4.0`: `internal/controller/apiextensions/composite/{composition_functions.go,reconciler.go,errors.go}`, `internal/engine/{engine.go,errors.go}`, `cmd/crossplane/core/core.go`
