# Crossplane v2 patterns: composing native K8s objects, and what v2 changed

Survey for **compositionfactory** DSL design. Method: 68 cloned repos, 680 composition YAML
files, 508 XRD files, read at scale with grep/python; plus primary sources (Crossplane v2.4
source + docs at commit `0e4f8c1d`, function-go-templating `5d48403b`, function-auto-ready
`5383ea04`). Every count below is from that corpus unless stated.

---

## What this means for the DSL — 5 bullets

1. **`readiness` must be a per-resource first-class node property with three modes, not one.**
   Of 109 real `gotemplating.fn.crossplane.io/ready` occurrences: **93 (85%) are a bare literal
   `"True"`** (force-ready), **14 (13%) are wrapped in an `{{ if }}` guard** derived from an
   observed condition/field, **2 (2%) interpolate a variable computed earlier in the template**.
   The escape hatch is only needed for the third. Model modes: `auto` (emit nothing, let
   function-auto-ready decide) / `always` / `whenCondition{type,status}` /
   `whenFieldPresent{path}` / `expr` (raw). function-kro already models this structurally as
   `readyWhen`, which is precedent that a graph GUI can own it.

2. **`namespace` on a composed object is a derived field, never a user field — and its
   derivation is a pure function of XRD scope.** The corpus proves it: the *only* diff between
   back-stack's namespaced and cluster-scoped versions of the same app composition is one added
   line per resource. `scope: Namespaced` → emit no namespace (Crossplane force-overwrites it);
   `scope: Cluster` → emit `namespace: <expr>` (required, and it is the only way to place the
   resource). The generator should compute this and the GUI should not show a namespace field
   for namespaced XRDs at all.

3. **Two hard structural constraints must be enforced at graph-edit time, not at apply time.**
   (a) A namespaced XR **cannot** compose a cluster-scoped kind — Crossplane fails the whole
   composition with `cannot apply cluster scoped composed resource ...`. (b) v2 XRDs **reject**
   `claimNames` and `connectionSecretKeys` via CEL on the CRD. Both are cheap to check in a node
   palette (filter kinds by scope; never emit those XRD fields) and expensive to debug later.

4. **XR connection details are GONE in v2 — `CompositeConnectionDetails` is a silent no-op.**
   The v2 XR schema simply has no `spec.writeConnectionSecretToRef` (only `LegacyCluster` gets
   it), and the publisher early-returns when that ref is nil. 29 corpus files still emit
   `CompositeConnectionDetails`. The DSL must replace it with a **first-class "aggregate secret"
   node**: a composed `v1/Secret` whose `data` keys are wired from other nodes'
   `.connectionDetails`, with an empty-`data` fallback for first reconcile. Composed *MRs* keep
   `writeConnectionSecretToRef` — but in v2 it takes **`name` only**, no `namespace`.

5. **The user's `function-auto-ready v0.5.0` cannot make a Deployment ready — upgrade to
   ≥ v0.6.0 changes the DSL's default.** v0.5.0 only checks `Ready=True` conditions; a
   Deployment reports `Available`, never `Ready`, so it hangs forever. Native GVK health checks
   (Deployment/Service/Job/StatefulSet/Ingress/PVC/HPA/…) landed in **v0.6.0** (2025-12-05).
   Generate for the version: on ≤ v0.5.x, native K8s nodes must default to an explicit
   readiness derivation; on ≥ v0.6.0 they can default to `auto`. Make this a blueprint-level
   `functions.autoReady.version` input.

---

## Corpus shape (for calibration)

| slice | count |
|---|---|
| composition YAML files scanned | 680 |
| XRD documents scanned | 508 |
| XRD `apiextensions.crossplane.io/v2` vs `/v1` | **223 v2** / 160 v1 |
| XRD scopes: `Namespaced` / `Cluster` / `LegacyCluster` | **191 / 33 / 159** |
| compositions resolvable to an XRD in the same repo | 619 |
| … targeting a **v2** XRD | **319** (275 Namespaced, 44 Cluster) |
| … targeting a v1/LegacyCluster XRD | 300 |
| v2 compositions that compose a **native K8s apiVersion** | **114 / 319 (36%)** |
| compositions using `function-go-templating` | 236 |
| compositions using `function-auto-ready` | 274 |
| compositions using `function-environment-configs` | 68 |
| compositions referencing `ops.crossplane.io` | **0** |

v2 is now roughly half of all real compositions and slightly ahead of v1 in this corpus.

---

## 1. Composing native Kubernetes objects directly

### 1.1 Pattern: `NativeK8sDirect` — the v2 replacement for provider-kubernetes `Object`

Direct native composition has overtaken the v1 `Object`-wrapper for every workload kind:

| kind | composed **directly** | wrapped in provider-kubernetes `Object` |
|---|---|---|
| Service | **88** | 11 |
| ConfigMap | **75** | 9 |
| Secret | **65** | 33 |
| Deployment | **56** | 12 |
| PersistentVolumeClaim | **9** | 6 |
| Ingress | **10** | 4 |
| ServiceAccount | 8 | **14** |
| Job | 1 | **8** |
| Namespace | 4 | **7** |
| StatefulSet | 1 | 2 |
| CronJob | 2 | 1 |

(66 corpus compositions still reference `kubernetes.crossplane.io` at all; 614 do not.)
Note the tail: `Namespace` and `ServiceAccount` are still mostly wrapped, because a namespaced
XR **cannot** compose a cluster-scoped `Namespace` — see §1.3.

**Canonical example** — Crossplane's own get-started composition, v2.4 docs:

```yaml
# https://github.com/crossplane/docs/blob/main/content/v2.4/manifests/get-started/composition/composition-templated-yaml.yaml
apiVersion: apiextensions.crossplane.io/v1        # <- Composition is STILL v1 in Crossplane v2
kind: Composition
spec:
  compositeTypeRef:
    apiVersion: example.crossplane.io/v1
    kind: App
  mode: Pipeline
  pipeline:
  - step: create-deployment-and-service
    functionRef:
      name: crossplane-contrib-function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          ---
          apiVersion: apps/v1
          kind: Deployment
          metadata:
            annotations:
              gotemplating.fn.crossplane.io/composition-resource-name: deployment
              {{ if eq (.observed.resources.deployment | getResourceCondition "Available").Status "True" }}
              gotemplating.fn.crossplane.io/ready: "True"
              {{ end }}
            labels:
              example.crossplane.io/app: {{ .observed.composite.resource.metadata.name }}
          spec:
            replicas: 2
            selector:
              matchLabels:
                example.crossplane.io/app: {{ .observed.composite.resource.metadata.name }}
            template:
              metadata:
                labels:
                  example.crossplane.io/app: {{ .observed.composite.resource.metadata.name }}
              spec:
                containers:
                - name: app
                  image: {{ .observed.composite.resource.spec.image }}
                  ports:
                  - containerPort: 80
```

Note: **no `metadata.name`, no `metadata.namespace`** on the Deployment. Both are supplied by
Crossplane.

> **GUI:** fully structural. This is the single most important node type — "compose a native K8s
> object". Everything in it (labels, selector, container list, ports) is plain field mapping from
> XRD fields. No escape hatch needed.

### 1.2 Naming and ownership — exactly what Crossplane injects

Source of truth: `RenderComposedResourceMetadata` in
`internal/controller/apiextensions/composite/composition_render.go` (Crossplane v2.4):

```go
// We recommend composed resources let us generate a name for them. They're
// allowed to explicitly specify a name if they want though.
if cd.GetName() == "" && cd.GetGenerateName() == "" {
    cd.SetGenerateName(xr.GetLabels()[xcrd.LabelKeyNamePrefixForComposed] + "-")
}

// If the XR is namespaced it can only create composed resources in its own
// namespace. Cluster scoped XRs can compose cluster scoped resources, or
// resources in any namespace.
if xr.GetNamespace() != "" {
    cd.SetNamespace(xr.GetNamespace())
}

if n != "" {
    xcrd.SetCompositionResourceName(cd, string(n))
}

metaLabels := map[string]string{
    xcrd.LabelKeyNamePrefixForComposed: xr.GetLabels()[xcrd.LabelKeyNamePrefixForComposed],
}
// ... claim labels only if present ...
meta.AddLabels(cd, metaLabels)

or := meta.AsController(meta.TypedReferenceTo(xr, xr.GetObjectKind().GroupVersionKind()))
return errors.Wrap(meta.AddControllerReference(cd, or), errSetControllerRef)
```

Exact keys (`crossplane-runtime/pkg/xcrd/schemas.go` + `composite.go`):

```go
LabelKeyNamePrefixForComposed = "crossplane.io/composite"
LabelKeyClaimName             = "crossplane.io/claim-name"        // never set on v2 XRs
LabelKeyClaimNamespace        = "crossplane.io/claim-namespace"   // never set on v2 XRs
AnnotationKeyCompositionResourceName = "crossplane.io/composition-resource-name"
```

So, injected for free on every composed object, **including native K8s ones**:
`generateName: <xr-name>-` (if no name given), `namespace` (= XR's, if XR namespaced),
label `crossplane.io/composite: <xr-name>`, annotation
`crossplane.io/composition-resource-name: <step-resource-name>`, and an owner reference with
`controller: true` pointing at the XR. Deletion is by Kubernetes GC via that owner ref; that's
also what `matchControllerRef: true` selectors key off.

**Practical consequence the DSL must expose:** `generateName` produces `my-app-9bj8j`-style
names, so **a composed object that another composed object must reference by name needs an
explicit `metadata.name`**. 57 of the 114 v2+native compositions (50%) set a templated
`metadata.name`. back-stack's web-app needs it three times over — Ingress→Service, HPA→Deployment:

```yaml
# https://github.com/back-stack/kubecon-na-2025/blob/main/crossplane/05-compositions/web-app/go-templating.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .observed.composite.resource.metadata.name }}
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: ingress
    gotemplating.fn.crossplane.io/ready: "True"
spec:
  rules:
  - host: {{ .observed.composite.resource.spec.fqdn }}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: {{ .observed.composite.resource.metadata.name }}   # <- must match the Service's explicit name
            port:
              number: {{ .observed.composite.resource.spec.port }}
```

> **GUI:** structural, and this is a *graph edge the canvas should draw*. Node property
> `naming: generated | explicit(<expr>)`. When node B references node A's name, the generator
> must force A to `explicit` and emit the same expression on both sides — the GUI can validate
> this and refuse to leave A on `generated`. This is a real class of production bug the tool can
> eliminate.

### 1.3 Namespace: inherited automatically, and it is a hard boundary

Docs (v2.4 upgrade guide, "Namespace handling in compositions"):

> - **Namespaced XRs**: Don't specify `metadata.namespace` in templates. Crossplane ignores
>   template namespaces and uses the XR's namespace.
> - **Modern cluster-scoped XRs** (`scope: Cluster`): Can compose resources in any namespace.
>   Include `metadata.namespace` in templates to specify the target namespace.
> - **Legacy cluster-scoped XRs** (`scope: LegacyCluster`): Can't compose namespaced resources.

Enforcement, `composition_functions.go` (v2.4):

```go
// If the XR is namespaced then the composed resource must be too.
if xr.GetNamespace() != "" && cd.GetNamespace() == "" {
    isNs, err := c.client.IsObjectNamespaced(cd)
    ...
    if !isNs {
        return CompositionResult{}, errors.Errorf(errFmtNamespacedXRClusterResource, name, cd.GetKind(), cd.GetName())
    }
}

// Emit a warning if the XR is namespaced and the composed resource has
// a different namespace. The namespace will be overwritten to match the
// XR's namespace.
if xr.GetNamespace() != "" && cd.GetNamespace() != "" && cd.GetNamespace() != xr.GetNamespace() {
    events = append(events, TargetedEvent{
        Event:  event.Warning(reasonNamespaceOverridden, errors.Errorf(errFmtNamespaceOverridden, name, cd.GetNamespace(), xr.GetNamespace())),
        ...
```

Exact strings:
```go
errFmtNamespacedXRClusterResource = "cannot apply cluster scoped composed resource %q (a %s named %s) for a namespaced composite resource."
errFmtNamespaceOverridden         = "cannot create composed resource %q in namespace %q, using XR namespace %q instead"
```
Covered by e2e test `test/e2e/manifests/apiextensions/composition/namespaced-xr-no-cluster-scoped-resource/`
("Tests that namespaced XRs cannot compose cluster-scoped resources").

**Pattern: `ScopeConditionalNamespace`.** The clean proof this is derivable — back-stack ship the
same app twice; `diff namespaced/go-templating.yaml cluster-scoped/go-templating.yaml` is
*entirely*:

```
23a24
>             namespace: {{ .observed.composite.resource.spec.namespace }}
49a51
>             namespace: {{ .observed.composite.resource.spec.namespace }}
```
(https://github.com/back-stack/kubecon-na-2025/tree/main/crossplane/05-compositions/basic-app)

78 of 114 v2+native compositions do write a `namespace:` somewhere; harmless-but-redundant on
namespaced XRs (homelab writes `namespace: {{ $ns }}` from
`.observed.composite.resource.metadata.namespace` on every object).

> **GUI:** structural and *derived*. Hide the namespace field entirely when `scope: Namespaced`.
> When `scope: Cluster`, surface it as a required per-node field (or one blueprint-level default).
> Filter the node palette by target scope so a namespaced XR can never be given a `Namespace`,
> `ClusterRole`, or `StorageClass` node.

### 1.4 RBAC gate — the generator must emit it

Crossplane's SA can create MRs, XRs, and *some* K8s kinds out of the box; anything else needs an
aggregated ClusterRole (v2.4 `composition/compositions.md`, "Grant access to composed resources"):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cnpg:aggregate-to-crossplane
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"   # <- critical
rules:
- apiGroups: ["postgresql.cnpg.io"]
  resources: ["clusters"]
  verbs: ["*"]
```

> **GUI:** structural and fully automatable. compositionfactory knows every composed GVK in the
> graph, so it can emit exactly one aggregated ClusterRole per blueprint as a side artifact next
> to the XRD + Composition. Free correctness win — this is the #1 "why is nothing happening"
> failure for v2 native composition.

---

## 2. Readiness, in depth — the canvas's most load-bearing concept

### 2.1 function-auto-ready: exact rules, per version

**v0.5.0 (what the user is running)** — one rule only, from `fn.go`:

```go
// If this desired resource doesn't exist in the observed resources, it can't be ready.
or, ok := observed[name]; if !ok { continue }
// A previous Function ... said this resource was explicitly ready, or explicitly not ready.
if dr.Ready != resource.ReadyUnspecified { continue }
// If this observed resource has a status condition with type: Ready, status: True,
// we set its readiness to true.
c := or.Resource.GetCondition(xpv1.TypeReady)
if c.Status == corev1.ConditionTrue { dr.Ready = resource.ReadyTrue }
```

A `Deployment` has no `Ready` condition (it has `Available` and `Progressing`). A `Service`,
`ConfigMap` and `Secret` have no conditions at all. **On v0.5.0 every native K8s composed
resource is permanently not-ready, and therefore so is the XR.** That is exactly why 85% of
real ready annotations are a hardcoded `"True"`.

**v0.6.0+ (2025-12-05)** adds a GVK-keyed health-check registry (`healthchecks/registry.go`),
run *before* the Ready-condition fallback. Registered by `init()`:
ConfigMap, CronJob, DaemonSet, Deployment, HorizontalPodAutoscaler, Ingress, Job, Namespace,
PersistentVolumeClaim, Pod, ReplicaSet, Secret, Service, ServiceAccount, StatefulSet.
Verified present at tags v0.6.0, v0.6.8, v0.7.0; **absent** at v0.5.0/v0.5.1/v0.5.2.

Exact semantics that a generator must reason about:

```go
// checkDeploymentHealth: ArgoCD-style.
// 1. spec.replicas (default 1) == status.updatedReplicas
// 2. spec.replicas == status.availableReplicas
// 3. status.conditions contains "Available" with status "True"

// checkServiceHealth:
// - ClusterIP/NodePort/ExternalName -> always ready
// - LoadBalancer -> requires len(status.loadBalancer.ingress) > 0

// checkJobHealth: Complete=True -> ready; Failed=True or Suspended=True -> not ready; else progressing
// checkStatefulSetHealth: currentRevision == updateRevision && spec.replicas == readyReplicas == currentReplicas
// alwaysReady (exists => ready): ConfigMap, Secret, ServiceAccount, Namespace
```

Two traps worth encoding in the generator's defaults:
- **Ingress** requires an assigned load-balancer ingress. On a kind cluster with no LB
  controller, `status.loadBalancer.ingress` never populates → the XR never goes ready. The
  corpus agrees: every real Ingress node uses forced `ready: "True"`.
- **Secret/ConfigMap `alwaysReady` is existence-only.** For a connection-details aggregate Secret
  it marks ready before the keys are populated. Derive explicitly there (see §5).

**CEL health checks** are alpha and off by default (`features/features.go`:
`CELHealthcheckCustomizations: {Default: false, PreRelease: featuregate.Alpha}`), enabled with
`--feature-gates`. Useful for kinds with no Ready condition (Crossplane `Provider`/
`Configuration` packages, Cluster API `Cluster`). Do not generate this by default.

Source: https://github.com/crossplane-contrib/function-auto-ready

### 2.2 The `gotemplating.fn.crossplane.io/ready` annotation — exact accepted values

From function-go-templating `fn.go` (HEAD, and v0.12.x):

```go
annotationKeyReady = "gotemplating.fn.crossplane.io/ready"
readyStatusUnknown = "Unknown"
...
if v, found := cd.Resource.GetAnnotations()[annotationKeyReady]; found {
    // Kubernetes condition status uses "Unknown" where this function uses
    // "Unspecified". Accept "Unknown" as an alias so users can pass a resource's
    // condition status straight through, e.g. via getResourceCondition.
    if v == readyStatusUnknown { v = string(resource.ReadyUnspecified) }

    if v != string(resource.ReadyTrue) && v != string(resource.ReadyUnspecified) && v != string(resource.ReadyFalse) {
        response.Fatal(rsp, errors.Errorf("invalid function input: invalid %q annotation value %q: must be True, False, or Unspecified", annotationKeyReady, v))
        return rsp, nil
    }
    r := resource.Ready(v); ready = &r
    meta.RemoveAnnotations(cd.Resource, annotationKeyReady)   // stripped before apply
}
```

**Accepted: `"True"`, `"False"`, `"Unspecified"`, plus `"Unknown"` as an alias for
`"Unspecified"`. Anything else is a FATAL pipeline error.** Case-sensitive. Quote it — bare
`True` becomes a YAML boolean and will not match. The annotation is removed before the object is
applied. It works on the **XR** too (set readiness of the composite itself) as well as on
composed resources.

Interaction rule: auto-ready skips any resource whose readiness is already `!= Unspecified`,
so an explicit `True` **or** `False` from go-templating is authoritative and survives a later
auto-ready step. `Unspecified` hands the decision back to auto-ready.

### 2.3 The three real readiness patterns, with counts

20 corpus files, **109 occurrences**.

**(a) `ForceReady` — 93/109 (85%).** A bare literal, no guard.

```yaml
# https://github.com/cujarrett/homelab/blob/main/platform/api/composition.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ $name }}
  namespace: {{ $ns }}
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: deployment
    gotemplating.fn.crossplane.io/ready: "True"
```

> **GUI:** structural — a checkbox / enum value `readiness: always`. Note this is a *lie* the
> user is choosing (the XR reports ready before the app is up); worth a GUI warning, but it is
> genuinely the dominant real-world choice, largely because auto-ready < v0.6.0 leaves no
> alternative.

**(b) `DeriveReadyFromCondition` / `DeriveReadyFromField` — 14/109 (13%).** An `{{ if }}` guard
on the line before, emitting the annotation only when true (so it stays `Unspecified` otherwise).
Both flavours appear in the same canonical file:

```yaml
# Deployment: bridge Available -> Ready
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: deployment
    {{ if eq (.observed.resources.deployment | getResourceCondition "Available").Status "True" }}
    gotemplating.fn.crossplane.io/ready: "True"
    {{ end }}

# Service: readiness from a field's presence
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: service
    {{ if (get (getComposedResource . "service").spec "clusterIP") }}
    gotemplating.fn.crossplane.io/ready: "True"
    {{ end }}
```
(crossplane/docs `content/v2.4/manifests/get-started/composition/composition-templated-yaml.yaml`;
 back-stack/kubecon-na-2025 `05-compositions/basic-app/namespaced/go-templating.yaml` and
 `05-compositions/web-app/go-templating.yaml`)

Helper semantics (`function_maps.go`) the generator must respect:
```go
func getResourceCondition(ct string, res map[string]any) xpv2.Condition {
    // tries "resource.status" then "status"; returns an EMPTY condition
    // (Status == "Unknown") when absent  -> safe to compare without a nil guard
}
func getComposedResource(req map[string]any, name string) map[string]any {
    // "observed.resources[<name>]resource"; returns nil when absent -> MUST be guarded
}
```
So the condition form needs no nil guard; the field form does (`{{ with }}` / `get`), and
indexing `status.loadBalancer.ingress` element 0 needs a length check.

> **GUI:** structural. This is the "readiness derivation" node property the user already asked
> for: `readyWhen: { source: <this node|other node>, kind: condition, type: Available, status: True }`
> or `{ kind: fieldPresent, path: spec.clusterIP }`. The generator emits the guard, picks
> `getResourceCondition` vs `getComposedResource`+`get`, and gets the nil-safety right.
> **Precedent that this belongs in a schema, not a template:** function-kro exposes it as a
> first-class list field in the *same* docs page —
> `readyWhen: - ${deployment.status.?conditions.orValue([]).exists(c, c.type == "Available" && c.status == "True")}`
> (`composition-yaml-cel.yaml`).

**(c) `ComputedReadyValue` — 2/109 (2%).** Readiness computed into a variable earlier in the
template and interpolated, which also lets it emit an explicit `"False"`:

```yaml
# https://github.com/cujarrett/homelab/blob/main/platform/sql/composition.yaml
{{- $rdsReady := "False" }}
{{- with index (.observed.resources | default dict) "rds-instance" }}
  {{- range (.resource.status.conditions | default list) }}
    {{- if and (eq .type "Ready") (eq .status "True") }}
      {{- $rdsReady = "True" }}
    {{- end }}
  {{- end }}
{{- end }}
...
apiVersion: rds.aws.m.upbound.io/v1beta1
kind: Instance
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: rds-instance
    gotemplating.fn.crossplane.io/ready: {{ $rdsReady | quote }}
```

> **GUI:** this is the **rawTemplate** case — one node's readiness depends on a multi-resource
> boolean computed across the graph. Model the common single-source case structurally (b) and let
> (c) fall through to a per-field raw escape on the readiness property. 2/109 justifies not
> building a boolean-expression editor.

Explicit `"False"` appears only 3 times in the corpus (all in function-go-templating's own
examples) — but note it *is* semantically important: it blocks a later auto-ready from marking
the resource ready. Support it as an enum value, don't build UI around it.

### 2.4 XR-level readiness

Crossplane considers the XR ready when **all** desired composed resources are ready. The
annotation also works on the XR object itself (go-templating routes it to
`desiredComposite.Ready` when the rendered object matches the XR's GVK and carries no
`composition-resource-name`). Rare in the corpus; support it as a blueprint-level property.

---

## 3. What v2 changed for composition authors

Official sources: https://blog.crossplane.io/announcing-crossplane-2-0/ ·
https://docs.crossplane.io/v2.4/whats-new/ · https://docs.crossplane.io/v2.4/guides/upgrade-to-crossplane-v2/

**3.1 `Composition` is still `apiextensions.crossplane.io/v1`.** Only the *XRD* moved to `/v2`.
Every v2 example in the docs and every one of the 319 v2 compositions in the corpus uses
`apiVersion: apiextensions.crossplane.io/v1` for the Composition. The generator must not "upgrade"
it. `spec.mode: Pipeline` is the only mode (native patch-and-transform was removed).

**3.2 Claims removed; `scope` added.** v2 XRD spec carries CEL rules that reject the v1 fields
(`apis/apiextensions/v2/xrd_types.go`):

```go
// +kubebuilder:validation:XValidation:rule="!has(self.claimNames)",message="Claims aren't supported in apiextensions.crossplane.io/v2"
// +kubebuilder:validation:XValidation:rule="!has(self.connectionSecretKeys)",message="XR connection secrets aren't supported in apiextensions.crossplane.io/v2"
type CompositeResourceDefinitionSpec struct {
    ...
    // +kubebuilder:validation:Enum=Namespaced;Cluster
    // +kubebuilder:default=Namespaced
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
    Scope CompositeResourceScope `json:"scope,omitempty"`
```

`scope` defaults to `Namespaced` and is **immutable**, as are `group` and `names`. Only
`Namespaced` and `Cluster` are valid in v2 (`LegacyCluster` exists only under the v1 XRD API).

> **GUI:** structural. Scope is a blueprint-level radio that must be locked after first
> generation, and it drives the whole node palette (§1.3).

**3.3 `spec.crossplane` plumbing.** Generated by `xcrd.CompositeResourceSpecProps`
(crossplane-runtime `pkg/xcrd/schemas.go`):

```go
// Modern XRs nest their Crossplane machinery fields under spec.crossplane.
return map[string]extv1.JSONSchemaProps{
    "crossplane": {
        Type:        "object",
        Description: "Configures how Crossplane will reconcile this composite resource",
        Properties:  props,   // compositionRef, compositionSelector, compositionRevisionRef,
                              // compositionRevisionSelector, compositionUpdatePolicy, resourceRefs
    },
}
```

…and, critically for the DSL:

```go
// Namespaced XRs don't get to reference composed resources in other namespaces.
if s == v1.CompositeResourceScopeNamespaced {
    props["resourceRefs"] = ... // {apiVersion, kind, name}  -- NO namespace field
}
```

Only `LegacyCluster` gets `claimRef` and `writeConnectionSecretToRef` directly under `spec`.
The generator **must not** author any of these — Crossplane injects the whole `spec.crossplane`
subtree into the CRD schema. A generated XRD should contain only the user's own `spec`/`status`
properties. A v2 XR then looks like:

```yaml
apiVersion: example.crossplane.io/v1
kind: App
metadata:
  namespace: default
  name: my-app
spec:
  image: nginx
  crossplane:
    compositionRef: { name: app-kcl }
    compositionRevisionRef: { name: app-kcl-41b6efe }
    resourceRefs:
    - { apiVersion: apps/v1, kind: Deployment, name: my-app-9bj8j }
    - { apiVersion: v1,      kind: Service,    name: my-app-bflc4 }
```

**3.4 `.m.` groups.** Every Upjet provider ships each MR twice. Docs rule: `<domain>` →
`m.<domain>`, and *the namespaced variant usually resets to `v1beta1`*:

```
apiVersion: s3.aws.upbound.io/v1beta2      # legacy, cluster scoped
apiVersion: s3.aws.m.upbound.io/v1beta1    # namespaced  <- v2 XRDs target this
```

Corpus: 72 compositions reference `.m.upbound.io`, 136 reference `.m.crossplane.io`.
**The version reset is a real generator hazard** — you cannot mechanically derive the namespaced
apiVersion by string-substituting `.m.` into the cluster-scoped one; you must read the actual
namespaced CRD/MRD schema. `kubectl get mrds` lists them.

**3.5 `providerConfigRef` gained a `kind`.** In v2 the ref is `{name, kind}` and **defaults to
`{name: default, kind: ClusterProviderConfig}`** if omitted (v2.4 `managed-resources.md`):

```yaml
spec:
  providerConfigRef:
    name: default
    kind: ClusterProviderConfig    # or: ProviderConfig (namespaced, same namespace as the MR)
```

Corpus: of 164 compositions that emit `.m.` MRs, 106 set `providerConfigRef` and **84 explicitly
set `kind: ClusterProviderConfig`**. Real ClusterProviderConfig, with the reason stated:

```yaml
# https://github.com/cujarrett/homelab/blob/main/cluster/crossplane/aws-providerconfig.yaml
# ClusterProviderConfig rather than ProviderConfig so one credential serves every
# namespace. Namespaced managed resources default to this exact kind and name.
apiVersion: aws.m.upbound.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
```

> **GUI:** structural. A blueprint-level default `{name, kind}` applied to every MR node, with a
> per-node override. Emitting `kind` explicitly is the safe default given 84/106 do.

**3.6 MRD / MRAP gating which MRs exist.** `ManagedResourceDefinition` (alpha,
`apiextensions.crossplane.io/v1alpha1`) is a CRD wrapper with `state: Active|Inactive`;
`ManagedResourceActivationPolicy` matches MRD names by pattern and flips them Active. v2 ships a
catch-all `*` MRAP by default. A targeted one:

```yaml
apiVersion: apiextensions.crossplane.io/v1alpha1
kind: ManagedResourceActivationPolicy
metadata:
  name: my-resources
spec:
  activate:
  - buckets.s3.aws.upbound.io        # legacy cluster-scoped
  - instances.ec2.aws.upbound.io
  - buckets.s3.aws.m.upbound.io      # modern namespaced
  - instances.ec2.aws.m.upbound.io
```
(v2.4 `guides/upgrade-to-crossplane-v2.md` §3; also
`content/v2.4/manifests/guides/disabling-unused-managed-resources/activation-policy.yaml`)

> **DSL impact, and it's a real one:** if the user has replaced the default MRAP, a composition
> referencing a non-activated MR will fail at apply. compositionfactory already knows every MR
> GVK in the blueprint, so it should **emit a matching MRAP alongside the Composition** (same
> pattern as the RBAC ClusterRole in §1.4), and the MCP server should be able to check activation
> state against a live cluster. Corpus usage of MRAP is currently near zero — it's new — so make
> it an opt-in output, not a default.

**3.7 Operations (`ops.crossplane.io/v1alpha1`).** Alpha. `Operation` (run once),
`CronOperation` (schedule), `WatchOperation` (on resource change). Same `mode: Pipeline` +
`pipeline` + `functionRef` shape as a Composition, plus per-step
`requirements.requiredResources`:

```yaml
apiVersion: ops.crossplane.io/v1alpha1
kind: Operation
metadata:
  name: ingress-cert-monitor
spec:
  mode: Pipeline
  pipeline:
  - step: check-ingress-certificate
    functionRef:
      name: crossplane-contrib-function-python
    requirements:
      requiredResources:
      - requirementName: ingress
        apiVersion: networking.k8s.io/v1
        kind: Ingress
        name: example-app
        namespace: default
```
(`content/v2.4/manifests/get-started/operations/*.yaml`)

**Corpus count: 0.** Nobody in 680 real compositions uses Operations yet. Do **not** build canvas
support. The shape is close enough to a Composition pipeline that it's a cheap future output
target if it takes off.

**3.8 Removed in v2 (breaking, from `whats-new/_index.md`):** native patch-and-transform,
`ControllerConfig`, external secret stores, **composite resource connection details**, and the
default package registry (`spec.package` must now be fully qualified,
`xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.12.0`).

> **GUI:** the fully-qualified-package rule affects the `functions.yaml` the generator emits.
> Always write the registry host.

---

## 4. Nested composition — an XR composing another XR

**Count: 30 / 680 compositions (4.4%)**, of which 11 target a v2 XRD. Present across
independent real platforms: `homelab` (Api → Cache), `netclab-xp` (Router →
LoopbackInterface/RoutedInterface/BgpGlobal), `livewyer-ops` (Event → Workflow), `pavedplane`
(XEnvironment → XStorage), `stuttgart-things` (HarvesterVM → CloudInit/VolumeClaim),
`crossplane-on-eks` (XServerlessApp → IAMPolicy, 7 files), `component-appcat` (XCodeyInstance →
XVSHNForgejo, 5 files).

**Structurally it is not a special case.** A child XR is emitted exactly like any other composed
resource — GVK, `composition-resource-name`, no namespace:

```yaml
# https://github.com/cujarrett/homelab/blob/main/platform/api/composition.yaml
{{- if $cacheEnabled }}
---
apiVersion: platform.local.lab/v1alpha1
kind: Cache
metadata:
  name: {{ $name }}-cache
  namespace: {{ $ns }}
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: cache
    gotemplating.fn.crossplane.io/ready: "True"
spec:
  parameters:
    backend: {{ $cacheBackend }}
    consumerServiceAccount: {{ $name }}
{{- end }}
```

```yaml
# https://github.com/netclab/netclab-xp/blob/main/apis/mid/compositions/routereos-composition.yaml
{{- $endpoint := .observed.composite.resource.spec.endpoint }}
---
apiVersion: eos.netclab.dev/v1alpha1
kind: LoopbackInterface
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: {{ .observed.composite.resource.metadata.name }}-lo0
spec:
  endpoint: {{ $endpoint }}
  ifName: Loopback0
```

Three properties worth knowing:
- **Readiness is free.** XRs *do* publish a `Ready` condition, so even function-auto-ready v0.5.0
  handles a child XR correctly with no annotation. (homelab forcing `"True"` above is
  unnecessary.)
- **RBAC is free.** The RBAC manager grants Crossplane access to all XRs automatically — unlike
  arbitrary native K8s kinds (§1.4).
- **Namespace inheritance applies transitively.** A namespaced parent XR produces a namespaced
  child XR in the same namespace, which produces its grandchildren there too.

> **GUI:** structural, and the canvas representation is the interesting part. A child-XR node is
> just a node whose kind resolves to *another blueprint in the workspace*, so the GUI can (a)
> populate its `spec` fields from that blueprint's XRD schema instead of a provider CRD, (b) draw
> a drill-down edge, and (c) validate scope compatibility (a namespaced parent needs a namespaced
> child XRD). Given 4.4% real-world usage this is a **v2 feature, not v1** — but the node type
> costs almost nothing because it reuses the same emitter.

---

## 5. Connection details in v2 — a genuine redesign

**What changed.** From `whats-new/_index.md`: *"It removes composite resource connection details
support."* The v2.4 guide is explicit:

> Crossplane v1 included a feature that automatically created connection details for XRs.
> Crossplane v2 removes this feature **for XRs only**. Managed Resources (MRs) aren't affected by
> this change and still support connection details via their `writeConnectionSecretToRef` field.

**Why `CompositeConnectionDetails` silently does nothing.** go-templating still parses it and
fills `desiredComposite.ConnectionDetails`. Crossplane still carries it through
`CompositionResult.ConnectionDetails` and calls `PublishConnection`. But the publisher
early-returns:

```go
// internal/controller/apiextensions/composite/api.go
func (a *APIFilteredSecretPublisher) PublishConnection(ctx context.Context, o ConnectionSecretOwner, c managed.ConnectionDetails) (bool, error) {
	// This resource does not want to expose a connection secret.
	if o.GetWriteConnectionSecretToReference() == nil {
		return false, nil
	}
```

…and `spec.writeConnectionSecretToRef` is only added to the generated CRD schema for
`LegacyCluster` XRs (§3.3). So on a v2 XR the ref is always nil → nothing is published, with no
error. **29 corpus compositions emit `CompositeConnectionDetails`** — a live footgun the
generator should refuse to produce for a v2 XRD.

### 5.1 The v2 replacement: `AggregateSecret` — exact working YAML

Official pattern (v2.4 `guides/connection-details-composition.md` + its manifests). The XRD
declares an ordinary user field for the secret name (**not** `connectionSecretKeys`, which is
CEL-rejected):

```yaml
# content/v2.4/manifests/guides/connection-details-composition/xrd.yaml
apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: useraccesskeys.example.org
spec:
  group: example.org
  names: { kind: UserAccessKey, plural: useraccesskeys }
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              writeConnectionSecretToRef:      # <- a PLAIN user field, not Crossplane machinery
                type: object
                properties:
                  name:
                    type: string
```

```yaml
# content/v2.4/manifests/guides/connection-details-composition/composition-go-templating.yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: useraccesskeys-go-templating
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: UserAccessKey
  mode: Pipeline
  pipeline:
  - step: render-templates
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          ---
          apiVersion: iam.aws.m.upbound.io/v1beta1
          kind: User
          metadata:
            annotations:
              {{ setResourceNameAnnotation "user" }}
          spec:
            forProvider: {}
          ---
          apiVersion: iam.aws.m.upbound.io/v1beta1
          kind: AccessKey
          metadata:
            annotations:
              {{ setResourceNameAnnotation "accesskey-0" }}
          spec:
            forProvider:
              userSelector:
                matchControllerRef: true
            writeConnectionSecretToRef:
              name: {{ $.observed.composite.resource.metadata.name }}-accesskey-secret-0   # NAME ONLY, no namespace
          ---
          apiVersion: iam.aws.m.upbound.io/v1beta1
          kind: AccessKey
          metadata:
            annotations:
              {{ setResourceNameAnnotation "accesskey-1" }}
          spec:
            forProvider:
              userSelector:
                matchControllerRef: true
            writeConnectionSecretToRef:
              name: {{ $.observed.composite.resource.metadata.name }}-accesskey-secret-1
          ---
          apiVersion: v1
          kind: Secret
          metadata:
            name: {{ dig "spec" "writeConnectionSecretToRef" "name" "" $.observed.composite.resource}}
            annotations:
              {{ setResourceNameAnnotation "connection-secret" }}
          {{ if eq $.observed.resources nil }}
          data: {}
          {{ else }}
          data:
            user-0: {{ ( index $.observed.resources "accesskey-0" ).connectionDetails.username }}
            user-1: {{ ( index $.observed.resources "accesskey-1" ).connectionDetails.username }}
            password-0: {{ ( index $.observed.resources "accesskey-0" ).connectionDetails.password }}
            password-1: {{ ( index $.observed.resources "accesskey-1" ).connectionDetails.password }}
          {{ end }}
  - step: ready
    functionRef:
      name: function-auto-ready
```

Five mechanics the generator must get exactly right:

1. **`writeConnectionSecretToRef` on a namespaced MR takes `name` only** — no `namespace`; the
   secret lands in the MR's (= the XR's) namespace. 121 corpus files use it.
2. **`.connectionDetails` values are already base64** — the observed connection details map is
   raw base64 strings, so they go into `data:` (not `stringData:`) verbatim. 97 corpus files read
   `.connectionDetails`.
3. **The first-reconcile guard is mandatory.** `{{ if eq $.observed.resources nil }} data: {} {{ else }}`
   — without it the template panics on the first pass when nothing is observed.
4. **Cross-resource wiring uses the composition-resource-name**, not the K8s name:
   `index $.observed.resources "accesskey-0"`. That name is the stable graph identity.
5. **`matchControllerRef: true`** is how the child MR finds its sibling — it works because
   Crossplane set the same controller owner reference on both (§1.2).

> **GUI:** almost entirely structural, and it is the single biggest ergonomic win available.
> Model an `AggregateSecret` node: name expression, plus a list of `{key, fromNode,
> fromConnectionDetail}` rows. The canvas draws an edge per row. The generator emits the
> `index`/`.connectionDetails` lookups, the `data:`-vs-`stringData:` choice, and the nil guard —
> all three are things people get wrong by hand. Only pathological cases (a key assembled from a
> printf across several sources) need rawTemplate.
>
> **Also:** pair it with an explicit readiness derivation, not `auto`. auto-ready ≥ v0.6.0 marks a
> Secret ready on *existence* (`alwaysReady`), so a v2 XR with an aggregate-secret node would
> report ready while `data` is still `{}`. Derive from the source MRs' `Ready` conditions and the
> presence of the required keys.

---

## 6. EnvironmentConfig / function-environment-configs in v2

**Still supported, still beta, still widely used.** v2.4 docs
(`composition/environment-configs.md`, `state: beta`, `betaVersion: "1.18"`) keep the type at
`apiextensions.crossplane.io/v1beta1`, cluster-scoped, with a free-form `data` object. Crossplane
`<= v1.17` selected them natively; since v1.18 selection is done by
**function-environment-configs**, which merges the selected configs and puts them on the
**Context** at the well-known key `apiextensions.crossplane.io/environment`.

**Corpus: 68 compositions use function-environment-configs; 66 reference
`environmentconfigs.fn.crossplane.io`; 47 read the `apiextensions.crossplane.io/environment`
context key.** That is ~10% of all compositions — clearly first-class, and notably more common
than nested XRs.

```yaml
# content/v2.4/composition/environment-configs.md
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: example-composition
spec:
  mode: Pipeline
  pipeline:
  - step: environmentConfigs
    functionRef:
      name: function-environment-configs
    input:
      apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
      kind: Input
      spec:
        environmentConfigs:
        - type: Reference
          ref:
            name: example-environment
        - type: Selector
          selector:
            matchLabels:
            # Removed for brevity
    # the environment will be passed to the next function in the pipeline
    # as part of the context
```

Two selector types: `Reference` (exact name) and `Selector` (labels, where each label value is
either `type: Value` with a literal or `type: FromCompositeFieldPath` with
`valueFromFieldPath` — i.e. a label value taken from the XR). Multiple configs are merged **in
list order**.

Downstream a go-templating step reads it off the context, e.g.
`{{ index .context "apiextensions.crossplane.io/environment" }}`.

**One v2 caveat:** EnvironmentConfigs are **cluster-scoped**. A namespaced XR selecting one is
reading cluster-scoped data — that's fine (it's a *read*, via the required-resources path, which
is not subject to the namespaced-composed-resource boundary), but it means an EnvironmentConfig
is shared platform-wide and cannot be tenant-scoped by namespace.

> **GUI:** structural. A blueprint-level "environment" section — a list of `Reference`/`Selector`
> entries — that emits the pipeline step, plus a source type on any field mapping:
> `from: environment` with a path. The generator inserts the step at position 0 and rewrites the
> lookups. `FromCompositeFieldPath` label matching is the one fiddly bit, and it's still a plain
> two-field form.

---

## Appendix: assorted counts, for prioritisation

| pattern | files (of 680) |
|---|---|
| `setResourceNameAnnotation` helper | 74 |
| literal `gotemplating.fn.crossplane.io/composition-resource-name` | 156 |
| `getResourceCondition` | 9 |
| `gotemplating.fn.crossplane.io/ready` | 20 (109 occurrences) |
| `writeConnectionSecretToRef` | 121 |
| `.connectionDetails` read in a template | 97 |
| `CompositeConnectionDetails` (no-op on v2 XRs) | 29 |
| `writeConnectionSecretsToNamespace` (v1 Composition field) | 184 |
| `function-environment-configs` | 68 |
| `.m.crossplane.io` / `.m.upbound.io` | 136 / 72 |
| `kind: ClusterProviderConfig` | 84 (of 106 with a providerConfigRef) |
| nested XR composition | 30 |
| `ops.crossplane.io` | 0 |

Corpus repos include: crossplane/crossplane (e2e manifests), crossplane/docs (v2.2–v2.4),
upbound platform-ref-{aws,azure,gcp} and configuration-*, cujarrett/homelab,
back-stack/kubecon-na-2025, netclab/netclab-xp, tomernos/pavedplane, livewyer-ops/*,
stuttgart-things/crossplane, modelplaneai/modelplane, vshn/component-appcat, awslabs/crossplane-on-eks,
giantswarm/crossplane-gs-apis, TeraSky-OSS/declarative-conversion-operator,
platformplane/catalog-crossplane, 0xayf/homelab-idp, shlapolosa/health-service-idp, and the
crossplane-contrib function repos' own examples.
