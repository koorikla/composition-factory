I have everything I need. Writing up the reference now.

## Decisions this enables

1. **The tool must emit `scope:` explicitly and may only offer `Namespaced` or `Cluster`.** `LegacyCluster` does **not** exist in `apiextensions.crossplane.io/v2` on Crossplane 2.4.0 — verified rejected by the API server. The published docs are wrong on this point. Absent `scope`, the server defaults to `Namespaced`, but `crossplane xrd convert` (CLI v2.5.0) defaults it to `LegacyCluster`, so omitting it produces divergent local-vs-cluster output.
2. **Only two fields are hard-blocked by v2 admission (`claimNames`, `connectionSecretKeys`); everything else that "shouldn't" be there is silently accepted and then either inert or rejected downstream by the *derived CRD*.** The form builder must implement the derived-CRD rules itself (`metadata.name == <plural>.<group>`, exactly one `referenceable`, printer-column type enum, DNS-1035 version/shortName/category labels), because a bad XRD applies cleanly and only fails later as a missing `Established` condition.
3. **The XR schema editor must model exactly what `xcrd.genCrdVersion` copies:** from the root only `description` + `x-kubernetes-validations`; from `metadata` only `name.maxLength`; from `spec` only `description/required/properties/oneOf/x-kubernetes-preserve-unknown-fields/x-kubernetes-validations`; from `status` the same minus preserve-unknown. Everything else the user writes at those levels is silently dropped — so the builder should refuse to offer those knobs rather than let users write no-ops.
4. **`spec.crossplane.*` and `status.conditions` are reserved by overwrite, not by rejection.** The builder must reserve the property names `spec.crossplane` and `status.conditions` in its UI and must not emit them. `status.crossplane` is *not* reserved in v2 (contradicting the docs) but should still be avoided.
5. **CEL (`x-kubernetes-validations`), `default`, `enum`, `pattern`, `format`, `x-kubernetes-int-or-string`, `x-kubernetes-embedded-resource`, and list-type maps are all fully supported**; the only notable prohibitions are `uniqueItems: true`, `$ref`, `additionalProperties` alongside `properties`, and a missing `type` on any property. Unknown OpenAPI keywords are preserved in the XRD but silently discarded in the CRD.

---

# Crossplane v2.4.0 — `apiextensions.crossplane.io/v2` CompositeResourceDefinition reference

**Environment verified against:** kubectl context `kind-platform`, k8s server v1.36.1, Crossplane server v2.4.0 (`xpkg.crossplane.io/crossplane/crossplane:v2.4.0`), Crossplane CLI v2.5.0.

Method legend used throughout: **[V]** = verified by running it on this cluster; **[S]** = read from Crossplane source at a pinned tag; **[D]** = read in docs only.

## 0. The XRD CRD itself

```
$ kubectl get crd compositeresourcedefinitions.apiextensions.crossplane.io -o json
scope: Cluster
names: {"categories":["crossplane"],"kind":"CompositeResourceDefinition",
        "listKind":"CompositeResourceDefinitionList","plural":"compositeresourcedefinitions",
        "shortNames":["xrd","xrds"],"singular":"compositeresourcedefinition"}
conversion: {"strategy":"None"}
VERSION v1 served=True  storage=True  deprecated=True  subresources=['status']
VERSION v2 served=True  storage=False deprecated=None  subresources=['status']
status.storedVersions: ['v1']
```
**[V]** Critical structural facts:

- **`v1` is still the storage version; `v2` is served but not stored.** Conversion strategy is `None`, i.e. the apiserver only rewrites `apiVersion` — the two schemas are structurally identical, v2 just adds CEL rules and narrows the `scope` enum.
- `v1` carries `deprecated: true` with `deprecationWarning: "CompositeResourceDefinition v1 is deprecated and will be removed in a future release; consider migrating to v2"`. Any `kubectl` call against the v1 endpoint prints that warning. **[V]**
- Consequence: a v2 XRD is persisted through the v1 schema, so **v1's defaults are applied and read back on the v2 object**. Writing a minimal v2 XRD and reading it back yields:
  ```yaml
  spec:
    defaultCompositeDeletePolicy: Background     # ← injected, inert in v2
    defaultCompositionUpdatePolicy: Automatic    # ← injected
    scope: Namespaced                            # ← v2 default
  ```
  **[V]** This is a real GitOps/Argo drift source: the tool's emitted YAML will never match the server object unless it also emits `defaultCompositeDeletePolicy: Background` and `defaultCompositionUpdatePolicy: Automatic`. Recommend emitting `defaultCompositionUpdatePolicy` explicitly and accepting the `defaultCompositeDeletePolicy` diff (or emitting it too).
- **Strict field validation is on.** `kubectl` sends `fieldValidation=Strict`; any unknown key under `spec` is a hard `BadRequest`:
  ```
  strict decoding error: unknown field "spec.bogusField"
  strict decoding error: unknown field "spec.versions[0].subresources.status"
  ```
  **[V]** The one exception is inside `versions[].schema.openAPIV3Schema`, which is declared `x-kubernetes-preserve-unknown-fields: true`. **[V]**

## 1. Complete `spec` field reference (v2)

Extracted directly from the live CRD's v2 OpenAPI schema. `spec.required: [group, names, versions]`.

### `spec` level CEL (the only v2-specific admission rules)
```yaml
x-kubernetes-validations:
- rule: "!has(self.claimNames)"
  message: "Claims aren't supported in apiextensions.crossplane.io/v2"
- rule: "!has(self.connectionSecretKeys)"
  message: "XR connection secrets aren't supported in apiextensions.crossplane.io/v2"
```
**[V]** Both fire together:
```
$ kubectl create --dry-run=server -f t17.yaml
The CompositeResourceDefinition "xtests.example.org" is invalid:
* spec: Invalid value: Claims aren't supported in apiextensions.crossplane.io/v2
* spec: Invalid value: XR connection secrets aren't supported in apiextensions.crossplane.io/v2
```

### Field table

| Field | Type | Req | Constraints (verified from live schema) |
|---|---|---|---|
| `group` | string | **yes** | CEL `self == oldSelf` — **immutable**. Must equal the suffix of `metadata.name`. |
| `names` | object | **yes** | CEL: immutable; `self.plural == self.plural.lowerAscii()`; `!has(self.singular) \|\| self.singular == self.singular.lowerAscii()` |
| `names.kind` | string | **yes** | No pattern enforced. CamelCase conventional. |
| `names.plural` | string | **yes** | Must be lowercase (CEL, message `"Plural name must be lowercase"`). |
| `names.singular` | string | no | Must be lowercase (CEL). Defaults to `lower(kind)` at CRD level. |
| `names.listKind` | string | no | Defaults to `<kind>List`. **Lowercase `listKind` is accepted** by both XRD and derived CRD. **[V]** |
| `names.categories` | []string | no | *Not* validated by the XRD; the **derived CRD requires DNS-1035** (lowercase). `categories: [Platform]` → derived CRD rejected. **[V]** Crossplane appends `composite` to whatever you supply. **[V]** |
| `names.shortNames` | []string | no | Same: DNS-1035 enforced only on the derived CRD. `shortNames: [XT]` → rejected downstream. **[V]** |
| `scope` | string | no | **`enum: [Namespaced, Cluster]`, `default: Namespaced`**, CEL immutable. See §2. |
| `versions` | []object | **yes** | Empty array passes XRD admission, fails derived CRD. **[V]** |
| `versions[].name` | string | **yes** | Not validated at XRD level; derived CRD enforces DNS-1035 (`V1Alpha1` → rejected). **[V]** |
| `versions[].served` | bool | **yes** | Explicitly required (no default — you must write it). |
| `versions[].referenceable` | bool | **yes** | Explicitly required. Maps 1:1 to CRD `storage`. **[S]** `Storage: vr.Referenceable` |
| `versions[].deprecated` | bool | no | Passed through to CRD `deprecated`. |
| `versions[].deprecationWarning` | string | no | `MaxLength=256` **[S]**. Passed through. |
| `versions[].schema` | object | no (API) / **yes** (controller) | Omitting it passes admission but the controller errors `custom resource validation cannot be nil` and the XRD never becomes Established. **[V]** |
| `versions[].schema.openAPIV3Schema` | object | — | `x-kubernetes-preserve-unknown-fields: true` — arbitrary content stored verbatim. |
| `versions[].additionalPrinterColumns` | []object | no | See §5. |
| `versions[].subresources` | object | no | **Only `scale` exists.** `subresources.status` is a strict-decoding error. **[V]** |
| `versions[].subresources.scale.specReplicasPath` | string | **yes** | |
| `versions[].subresources.scale.statusReplicasPath` | string | **yes** | |
| `versions[].subresources.scale.labelSelectorPath` | string | no | |
| `defaultCompositionRef` | object | no | `{name: string}`, `name` required. |
| `defaultCompositionRevisionSelector` | object | no | Full `metav1.LabelSelector` (`matchLabels`, `matchExpressions[{key,operator,values}]`). **Not in the original task list — it exists in v2.** |
| `enforcedCompositionRef` | object | no | `{name: string}`. CEL `self == oldSelf` — **immutable**. |
| `defaultCompositionUpdatePolicy` | string | no | `enum: [Automatic, Manual]`, `default: Automatic`. Becomes the **CRD-level default** of `spec.crossplane.compositionUpdatePolicy` on every XR. **[V]** |
| `defaultCompositeDeletePolicy` | string | no | `enum: [Background, Foreground]`, **no default in the v2 schema but defaulted to `Background` by the v1 storage schema**. Deprecated & **inert in v2** (it only ever fed claims). Still *accepted* — setting `Foreground` round-trips. **[V]** |
| `conversion` | object | no | Raw `extv1.CustomResourceConversion`. `strategy` required, `webhook.{clientConfig{caBundle,url,service{name,namespace,path,port}},conversionReviewVersions}`. See §7. |
| `metadata` | object | no | `{labels: map[string]string, annotations: map[string]string}` — applied to the **derived CRD's** metadata. |
| `claimNames` | object | no | **Present in the schema but blocked by CEL.** Only exists so v1 objects round-trip through the v2 endpoint. |
| `connectionSecretKeys` | []string | no | Same — present, CEL-blocked. |

### `status` (read-only, generated)
```yaml
status:
  conditions: []           # list-map keyed on `type`; only `Established` (+`Offered` for legacy)
  controllers:
    compositeResourceType:      {apiVersion, kind}
    compositeResourceClaimType: {apiVersion, kind}   # stays {"",""} in v2
```
**[V]** Live example from `xqueues.platform.sparky.ee`:
```yaml
status:
  conditions:
  - lastTransitionTime: "2026-08-26T00:55:22Z"
    observedGeneration: 1
    reason: WatchingCompositeResource
    status: "True"
    type: Established
  controllers:
    compositeResourceClaimType: {apiVersion: "", kind: ""}
    compositeResourceType: {apiVersion: platform.sparky.ee/v1alpha1, kind: XQueue}
```
Condition types/reasons **[S]** (`apis/apiextensions/v1/conditions.go`): `Established`, `Offered`, `ValidPipeline`, `Responsive`; reasons `WatchingCompositeResource`, `WatchingCompositeResourceClaim`, `TerminatingCompositeResource`, `TerminatingCompositeResourceClaim`, `ValidPipeline`, `MissingCapabilities`, `WatchCircuitOpen`, `WatchCircuitClosed`.

XRD finalizer: `defined.apiextensions.crossplane.io`. **[V]**

## 2. `scope` — the LegacyCluster question, settled

**`LegacyCluster` is NOT a valid value in `apiextensions.crossplane.io/v2` at Crossplane 2.4.0.**

```
$ kubectl explain xrd.spec.scope --api-version=apiextensions.crossplane.io/v2
FIELD: scope <string>
ENUM:
    Namespaced
    Cluster
```
```
$ kubectl create --dry-run=server -f t1.yaml     # scope: LegacyCluster, apiVersion v2
The CompositeResourceDefinition "xtests.example.org" is invalid:
* spec.scope: Unsupported value: "LegacyCluster": supported values: "Namespaced", "Cluster"
```
**[V]**, corroborated by source **[S]** (`apis/apiextensions/v2/xrd_types.go` @ v2.4.0):
```go
const (
	CompositeResourceScopeNamespaced CompositeResourceScope = "Namespaced"
	CompositeResourceScopeCluster    CompositeResourceScope = "Cluster"
)
// +kubebuilder:validation:Enum=Namespaced;Cluster
// +kubebuilder:default=Namespaced
```

By contrast the **v1** schema on the same cluster is `enum: [LegacyCluster, Namespaced, Cluster]` with `default: LegacyCluster`, and carries different CEL: **[V]**
```yaml
- rule: "self.scope == 'LegacyCluster' || !has(self.claimNames)"
  message: "Only LegacyCluster composite resources can offer claims"
- rule: "self.scope == 'LegacyCluster' || !has(self.connectionSecretKeys)"
  message: "Only LegacyCluster composite resources support connection secrets"
```

> **Docs discrepancy — flagging.** <https://docs.crossplane.io/latest/composition/composite-resource-definitions/> states the v2 `scope` field "supports three values: Namespaced, Cluster, LegacyCluster." That is false for the v2 *API version* on 2.4.0. **To author a LegacyCluster XRD you must write `apiVersion: apiextensions.crossplane.io/v1`.** Tool implication: LegacyCluster is a *v1 emitter mode*, not a v2 scope option.

Scope → derived CRD scope **[S]** `xcrd/crd.go`:
```go
scope := ptr.Deref(xrd.Spec.Scope, v1.CompositeResourceScopeLegacyCluster)
switch scope {
case Namespaced:    crd.Spec.Scope = extv1.NamespaceScoped
case Cluster:       crd.Spec.Scope = extv1.ClusterScoped
case LegacyCluster: crd.Spec.Scope = extv1.ClusterScoped
}
```
**Trap:** that `ptr.Deref(..., LegacyCluster)` default is what the offline CLI uses. Feeding a v2 XRD *without* `scope` to `crossplane xrd convert` renders a **LegacyCluster** CRD (claimRef, writeConnectionSecretToRef, status.connectionDetails, status.claimConditionTypes) while the same file applied to the cluster renders a **Namespaced** one. **[V]** — verified with t12.yaml. The CLI parses every XRD through the v1 Go types regardless of the file's `apiVersion`. **Always emit `scope:` explicitly.**

## 3. What v2 dropped vs v1

| Concept | v1 | v2 |
|---|---|---|
| `spec.claimNames` | Supported when `scope: LegacyCluster` | **CEL-rejected.** Field still in schema for round-tripping. |
| Claim CRD generation | `ForCompositeResourceClaim` produces a 2nd namespaced CRD | Never produced. `status.controllers.compositeResourceClaimType` stays `{"",""}`. **[V]** |
| `Offered` status condition | Set when claims are offered | Never set; `kubectl get xrd` OFFERED column is blank. **[V]** |
| `spec.connectionSecretKeys` | Filters published XR connection-secret keys | **CEL-rejected.** Docstring: *"Compose a secret instead."* **[V]** |
| `spec.defaultCompositeDeletePolicy` | Governs XR deletion when the claim goes away | Accepted, **inert** (nothing consumes it without claims). Still auto-defaulted to `Background`. **[V]** |
| XR `spec.claimRef` | Injected into the XR CRD | Not injected. **[V]** |
| XR `spec.writeConnectionSecretToRef` | Injected | Not injected. **[V]** |
| XR `status.connectionDetails.lastPublishedTime` | Injected | Not injected. **[V]** |
| XR `status.claimConditionTypes` | Injected | Not injected. **[V]** |
| XR machinery location | Directly on `spec.*` | Nested under **`spec.crossplane.*`** |
| XR namespacing | Cluster-scoped only | `Namespaced` (default) or `Cluster` |

**What LegacyCluster (v1-only) preserves** **[S]** `xcrd/schemas.go`: machinery flat on `spec` (`compositionRef`, `compositionSelector`, `compositionRevisionRef`, `compositionRevisionSelector`, `compositionUpdatePolicy`, `resourceRefs`) **plus** `spec.claimRef` (required `apiVersion,kind,namespace,name`) and `spec.writeConnectionSecretToRef` (required `name,namespace`); status gains `connectionDetails` and `claimConditionTypes`; the COMPOSITION/COMPOSITIONREVISION printer columns point at `.spec.compositionRef.name` instead of `.spec.crossplane.compositionRef.name`.

## 4. The derived XR CRD — exact v2.4 layout

Verified against the live `crd/xqueues.platform.sparky.ee` and reproduced by `crossplane xrd convert`.

```yaml
spec:                                # Namespaced XR
  crossplane:                        # description: "Configures how Crossplane will reconcile this composite resource"
    compositionRef:                 {name: string}                       required: [name]
    compositionRevisionRef:         {name: string}                       required: [name]
    compositionSelector:            {matchLabels: map[string]string}     required: [matchLabels]
    compositionRevisionSelector:    {matchLabels: map[string]string}     required: [matchLabels]
    compositionUpdatePolicy:        string enum:[Automatic,Manual]  default: <spec.defaultCompositionUpdatePolicy>
    resourceRefs:                   array, x-kubernetes-list-type: atomic
      items: {apiVersion, kind, name}  required: [apiVersion, kind]
  <your fields...>
status:
  conditions: array, x-kubernetes-list-type: map, x-kubernetes-list-map-keys: [type]
    items: {lastTransitionTime(date-time), message, observedGeneration(int64), reason, status, type}
    required: [lastTransitionTime, reason, status, type]
  <your fields...>
metadata:
  name: {type: string, maxLength: 63}   # everything else you wrote under metadata is DISCARDED
required: [spec]                        # injected at the schema root
subresources: {status: {}}              # always
```
**[V]** Live XR confirming the runtime shape:
```yaml
spec:
  crossplane:
    compositionRef: {name: xqueues.aws.platform.sparky.ee}
    compositionRevisionRef: {name: xqueues.aws.platform.sparky.ee-c6ccb78}
    compositionUpdatePolicy: Automatic
    resourceRefs:
    - {apiVersion: sqs.aws.m.upbound.io/v1beta1, kind: Queue, name: cncf-pre-talk-e28dacd7ec77}
  location: EU
status:
  conditions: [ {type: Synced,...}, {type: Ready,...}, {type: Responsive, reason: WatchCircuitClosed,...} ]
```

**`Cluster` vs `Namespaced` — the only schema difference:** `spec.crossplane.resourceRefs[].namespace` (type string) exists for `Cluster` and is **absent** for `Namespaced`. **[V]** (source comment: *"Namespaced XRs don't get to reference composed resources in other namespaces."*)

Also injected/derived: `names.categories` gets `composite` appended; XR labels `crossplane.io/composite: <xr-name>`; XR finalizer `composite.apiextensions.crossplane.io`; CRD gets an ownerReference `apiVersion: apiextensions.crossplane.io/v1, kind: CompositeResourceDefinition, controller: true, blockOwnerDeletion: true`. **[V]**

Metadata propagation **[S]/[V]**: the CRD's **labels** = XRD `metadata.labels` ∪ `spec.metadata.labels`; the CRD's **annotations** = `spec.metadata.annotations` only (the XRD's own annotations are **not** propagated).

## 5. Rules Crossplane enforces on `openAPIV3Schema`

### 5a. There is no rejection — there is **selective copying**

`xcrd.genCrdVersion` (crossplane-runtime `pkg/xcrd/crd.go`) starts from `BaseProps()` and copies a *fixed, small* set of fields off your schema **[S]**:

| Level | Copied from your schema | Everything else |
|---|---|---|
| root | `Description`, `XValidations` | **dropped** — including root `required` (replaced by `[spec]`), `oneOf`, `additionalProperties`, `x-kubernetes-preserve-unknown-fields` |
| `metadata` | only `properties.name.maxLength`, and only if **stricter** than 63 (`if old != nil && *old < maxLength`) | **dropped** — `metadata.labels`, `metadata.annotations`, `metadata.generateName` schemas silently vanish |
| `apiVersion`,`kind` | untouched (always `{type: string}`) | your versions are overwritten |
| `spec` | `Description`, `Required` (**appended**), `XPreserveUnknownFields`, `XValidations` (**appended**), `OneOf` (**appended**), `Properties` (`maps.Copy`) | **dropped** — `additionalProperties`, `anyOf`, `allOf`, `not`, `min/maxProperties`, `default`, `nullable`, `type` |
| `status` | `Description`, `Required` (**replaced**), `XValidations` (**replaced**), `OneOf` (**replaced**), `Properties` (`maps.Copy`) | dropped, incl. `XPreserveUnknownFields` |

Then, **after** that, Crossplane does `maps.Copy(spec.Properties, CompositeResourceSpecProps(...))` and `maps.Copy(status.Properties, CompositeResourceStatusProps(...))` — so **Crossplane wins every key collision**.

### 5b. Can you define `status`? `metadata`? `apiVersion`/`kind`?

| Top-level property | Allowed? | Behaviour (**[V]**) |
|---|---|---|
| `spec` | **Yes — this is the point.** | Your properties merged; `spec.crossplane` reserved. |
| `status` | **Yes.** Arbitrary `status.*` fields are preserved (e.g. `status.url`, `status.phase`). | `status.required` and `status.oneOf` are honoured. |
| `status.conditions` | Reserved. Writing `conditions: {type: string}` is **silently overwritten** with the canonical array. | Verified: t18 output. |
| `status.crossplane` | **Not reserved in v2.** A user-defined `status.crossplane: {properties:{foo:{type:string}}}` survives into the CRD verbatim. | Verified. Docs claim it's disallowed — false. Still avoid it. |
| `metadata` | Accepted but **stripped down to `name.maxLength`**. `metadata.labels` etc. vanish silently. | Verified: t6 (`maxLength: 20` survived, `labels` disappeared). |
| `apiVersion`, `kind` | Accepted, ignored (always reset to `{type: string}`). | Verified. |
| root `required` | Accepted, **replaced by `[spec]`**. | Verified: t12 wrote `required: [status]`, output `required: [spec]`. |
| root `x-kubernetes-validations` | **Preserved.** | Verified: t12's `has(self.spec)` rule appears in the CRD. |
| `spec.crossplane` | Reserved. User definition **silently replaced**. | Verified: t6. |

> **Docs discrepancy — flagging.** The docs say *"Crossplane doesn't allow the following fields in a schema: Any field under `spec.crossplane`, Any field under `status.crossplane`, `status.conditions`."* Empirically nothing is *rejected*: `spec.crossplane` and `status.conditions` are **overwritten**, `status.crossplane` is **kept**. A form builder should treat the first two as reserved names in the UI and warn (not error) rather than relying on server rejection.

### 5c. Kubernetes structural-schema rules that *do* reject (enforced on the derived CRD)

Verified by piping `crossplane xrd convert` output into `kubectl create --dry-run=server`:

| Construct | Verdict |
|---|---|
| property with no `type` | **Rejected**: `properties[noType].type: Required value: must not be empty for specified object fields` |
| `$ref: "#/definitions/Foo"` | **Rejected**: `$ref is not supported` |
| `additionalProperties` together with `properties` (incl. `additionalProperties: false`) | **Rejected**: `additionalProperties and properties are mutual exclusive` |
| `uniqueItems: true` | **Rejected**: `Forbidden: uniqueItems cannot be set to true since the runtime complexity becomes quadratic` |
| `default` whose value violates the declared `format` | **Rejected**: `default: Invalid value: "abc": in body must be of type email` |
| `x-kubernetes-validations[].fieldPath: "."` | **Rejected**: `must be a valid path` (use `.region`) |

Error reporting is **staged**: the `$ref`/`additionalProperties` class of errors suppresses the structural-schema pass, so a schema with both kinds of problem reports only the first batch. The builder must run its own complete validation rather than round-tripping to the apiserver once.

### 5d. What is accepted (all **[V]** through a clean derived-CRD dry-run)

`type: string|integer|number|boolean|array|object`; `minLength` `maxLength` `pattern` `format` `enum` `default` `example` `title` `description`; `minimum` `maximum` `exclusiveMinimum` `exclusiveMaximum` `multipleOf`; `minItems` `maxItems` `items`; `required`; `nullable`; `not`; `oneOf`/`anyOf`/`allOf`; `additionalProperties: {type: ...}` (map idiom); `x-kubernetes-preserve-unknown-fields: true`; `x-kubernetes-int-or-string: true`; `x-kubernetes-embedded-resource: true`; `x-kubernetes-list-type: atomic|set|map` + `x-kubernetes-list-map-keys`; `x-kubernetes-validations`.

`default` on a field that is also in `required` is accepted. **[V]**

### 5e. CEL — full support

```yaml
spec:
  type: object
  required: [region]
  x-kubernetes-validations:
  - rule: "self.region in ['eu','us']"
    message: bad region
    reason: FieldValueInvalid
    fieldPath: ".region"
  - rule: "self.size != 'huge'"
    messageExpression: "'size ' + self.size + ' not allowed'"
```
Accepted at XRD admission **and** by the derived CRD. **[V]** `rule`, `message`, `messageExpression`, `reason`, `fieldPath` all work. Rules attach at any level (root, `spec`, `status`, individual properties). Note the merge semantics: **`spec.x-kubernetes-validations` are appended to Crossplane's (currently empty) list; `status.x-kubernetes-validations` replace.** A `spec`-level rule evaluates against a `self` that *includes* the injected `crossplane` object, so `self.all(k, ...)`-style rules must account for it.

### 5f. Unknown keywords

An invented keyword (`madeUpKeyword: true`) is **stored verbatim in the XRD** (because `openAPIV3Schema` is `x-kubernetes-preserve-unknown-fields: true`) and **silently dropped from the derived CRD** (because `parseSchema` does `json.Unmarshal` into `extv1.JSONSchemaProps`). Same fate for OpenAPI keywords k8s doesn't model, e.g. a property-level `deprecated: true`. **[V]**

## 6. `additionalPrinterColumns` — exact shape

```yaml
additionalPrinterColumns:
- name:        string   # REQUIRED — the column header
  type:        string   # REQUIRED — must be one of: boolean, date, integer, number, string
  jsonPath:    string   # REQUIRED — e.g. ".spec.location"
  description: string   # optional
  format:      string   # optional — OpenAPI format hint
  priority:    integer  # optional, int32 — >0 means only shown with `-o wide`
```
`type` is **not** validated by the XRD; the derived CRD rejects it: `spec.additionalPrinterColumns[0].type: Invalid value: "object": must be one of boolean,date,integer,number,string`. **[V]**

**Ordering:** your columns come first, then Crossplane appends five **[V]/[S]**:

| name | type | jsonPath | priority |
|---|---|---|---|
| `SYNCED` | string | `.status.conditions[?(@.type=='Synced')].status` | 0 |
| `READY` | string | `.status.conditions[?(@.type=='Ready')].status` | 0 |
| `COMPOSITION` | string | `.spec.crossplane.compositionRef.name` | 0 |
| `COMPOSITIONREVISION` | string | `.spec.crossplane.compositionRevisionRef.name` | **1** |
| `AGE` | date | `.metadata.creationTimestamp` | 0 |

(For LegacyCluster the last two drop the `.crossplane` segment. Claim CRDs get `SYNCED`, `READY`, `CONNECTION-SECRET` → `.spec.writeConnectionSecretToRef.name`, `AGE`.)

Practical cap: k8s truncates table output, so keep user columns to ~2–3.

## 7. Naming rules — what is actually enforced

### `metadata.name` must be `<names.plural>.<group>` — enforced *indirectly*

Source **[S]**: `crd.SetName(xrd.GetName())` — Crossplane copies the XRD name onto the CRD **verbatim**, never recomputing it. The Kubernetes apiserver then enforces the CRD naming rule:

```
$ crossplane xrd convert t3.yaml | kubectl create --dry-run=server -f -   # name: wrong.example.org, plural: xtests
The CustomResourceDefinition "wrong.example.org" is invalid:
* metadata.name: Invalid value: "wrong.example.org": must be spec.names.plural+"."+spec.group
```
**[V]** — and crucially, the XRD itself applies **cleanly** (`kubectl create --dry-run=server -f t3.yaml` → `created`). The failure surfaces only as the XRD never gaining `Established: True`, plus a Warning Event with reason `RenderCRD`/`EstablishComposite`. **The builder must enforce this client-side.**

`metadata.name` is separately constrained to RFC-1123 subdomain by the apiserver. **[V]**

### The `X` prefix is **conventional, not required**

```
$ kubectl create --dry-run=server -f t2.yaml            # kind: Queue, plural: queues, name: queues.example.org
compositeresourcedefinition.apiextensions.crossplane.io/queues.example.org created (server dry run)
$ crossplane xrd convert t2.yaml | kubectl create --dry-run=server -f -
customresourcedefinition.apiextensions.k8s.io/queues.example.org created (server dry run)
```
**[V]** No prefix check anywhere in admission, CEL, or the CRD derivation. Docs **[D]**: *"Crossplane recommends starting XRD kinds with an X..."* — recommendation only. In Crossplane v2 (no claims) the historical reason for the X — disambiguating XR from claim kind — is gone. **Tool decision: offer the `X` prefix as a default, not a constraint.** A lowercase `kind` (`xtest`) also passes everywhere. **[V]**

### Case rules summary

| Field | Rule | Enforced by |
|---|---|---|
| `metadata.name` | RFC-1123 subdomain; `== plural + "." + group` | apiserver (XRD) / apiserver (derived CRD) |
| `names.plural` | lowercase | **XRD CEL** — `"Plural name must be lowercase"` |
| `names.singular` | lowercase | **XRD CEL** — `"Singular name must be lowercase"` |
| `names.kind` | none | — (CamelCase conventional) |
| `names.listKind` | none (lowercase accepted) | — |
| `names.shortNames[]` | DNS-1035 | derived CRD only |
| `names.categories[]` | DNS-1035 | derived CRD only |
| `versions[].name` | DNS-1035 | derived CRD only |

## 8. Versioning, `referenceable`, conversion

### `referenceable` ⇔ CRD `storage`

`kubectl explain xrd.spec.versions.referenceable --api-version=apiextensions.crossplane.io/v2`:
> *"Exactly one version must be marked as referenceable; all Compositions must target only the referenceable version. The referenceable version must be served. It's mapped to the CRD's `spec.versions[*].storage` field."* **[V]**

Enforcement is again downstream, not at XRD admission:
```
# two referenceable: true
XRD admission:  created (server dry run)
derived CRD:    spec.versions: ... must have exactly one version marked as storage version
                status.storedVersions: Invalid value: ["v1alpha1"]: must have the storage version v1beta1

# zero referenceable
XRD admission:  created (server dry run)
derived CRD:    spec.versions: ... must have exactly one version marked as storage version
                status.storedVersions: Invalid value: null: must have at least one stored version
```
**[V]** The "referenceable must be served" half is **documented but not enforced** — `served: false` + `referenceable: true` passes both the XRD and the derived CRD cleanly. **[V]** (It's still broken in practice: no XR can be created via that version.) The builder should enforce both rules itself.

Multi-version XRDs work: a full two-version XRD (`v1alpha1` deprecated/not-referenceable + `v1beta1` referenceable, differing schemas, a webhook conversion block) applies and derives a valid CRD. **[V]**

### Version ordering
Version names drive API-discovery order via the standard kube-like sort (GA > beta > alpha, then major, then minor; non-kube-like versions sort last, lexicographically). Copied verbatim from k8s into the field docstring. **[V]** via `kubectl explain`.

### Conversion webhooks

`spec.conversion` is passed through **verbatim** to the derived CRD **[S]** (`Conversion: xrd.Spec.Conversion`), so all validation is the apiserver's:

```
strategy: Bogus    → derived CRD: spec.conversion.strategy: Unsupported value: "Bogus": supported values: "None", "Webhook"
strategy: Webhook  → derived CRD: spec.conversion.webhookClientConfig: Required value: required when strategy is set to Webhook
                                  spec.conversion.conversionReviewVersions: Required value
strategy: None     → clean
```
**[V]** A full webhook block (`clientConfig.service{name,namespace,path,port}` + `conversionReviewVersions: [v1]`) renders and validates cleanly. **[V]**

> **v1-only landmine worth knowing (the tool avoids it by emitting v2):** the **v1** XRD schema carries the CEL rule `self.strategy == 'Webhook' && has(self.webhook)` on `spec.conversion`. That rule is written wrong — it makes *any* `conversion` block that isn't `Webhook`-with-webhook fail, including `strategy: None`:
> ```
> $ kubectl create --dry-run=server -f tc.yaml   # apiextensions.crossplane.io/v1, conversion: {strategy: None}
> The CompositeResourceDefinition "xtests.example.org" is invalid:
>   spec.conversion: Invalid value: Webhook configuration is required when conversion strategy is Webhook
> ```
> **[V]** **v2 has no such rule.** Recommendation: **omit `spec.conversion` entirely unless a real webhook is configured.**

Note Crossplane does not host a conversion webhook for XRs — you'd have to run your own service. The XRD CRD itself uses `strategy: None` between its own v1/v2. **[V]**

## 9. Tooling the generator can lean on

- **`crossplane xrd convert <file>`** (CLI v2.5.0, BETA) renders the exact CRD(s) offline using the same `xcrd` code the controller runs. Flags: `-o/--output-file`, `--output-dir`, `--format=crd|jsonschema`. **The `jsonschema` format emits a `$id`-tagged JSON Schema with `additionalProperties: false` injected** — directly usable to drive a form builder or YAML language server. It refuses to write multiple schemas to a single file (`use --output-dir`). **[V]**
  - **Caveat 1:** it parses everything through the **v1** Go types, so an omitted `scope` renders as LegacyCluster (see §2).
  - **Caveat 2:** its output carries `ownerReferences[0].uid: ""`, which the apiserver rejects. Strip `ownerReferences` before dry-running the CRD.
  - **Caveat 3:** it does *not* enforce the v2 CEL rules — `scope: LegacyCluster` under `apiVersion: .../v2` converts happily offline while the server rejects it. **[V]**
- **`crossplane xrd generate <xr-or-simpleschema.yaml> [--from xr|simpleschema] [--plural] [--path] [--replace]`** — infers an XRD from an example XR. Requires a `crossplane-project.yaml`. **[V]** (help text)
- **`crossplane composition generate <xrd> [--name] [--plural] [--path]`** — emits a Composition with a single `function-auto-ready` pipeline step. This is the direct prior art for the tool being built. **[V]** (help text)
- **No Crossplane validating webhook exists for XRDs.** `kubectl get validatingwebhookconfigurations` on this cluster shows only `crossplane-no-usages` (`nousages.protection.crossplane.io`, rules `*/*/*`) from Crossplane. All XRD validation is CRD-level CEL + strict decoding + downstream CRD rejection. **[V]**

## 10. Recommended emitter template

```yaml
apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: <plural>.<group>                 # MUST equal spec.names.plural + "." + spec.group
spec:
  group: <group>                         # immutable after create
  names:
    kind: X<Kind>                        # X prefix optional; kind free-form
    plural: <plural>                     # must be lowercase
    singular: <singular>                 # must be lowercase (optional)
    listKind: X<Kind>List                # optional
    categories: [<lowercase-dns1035>]    # optional; "composite" appended automatically
    shortNames: [<lowercase-dns1035>]    # optional
  scope: Namespaced                      # ALWAYS emit. Namespaced | Cluster only.
  defaultCompositionUpdatePolicy: Automatic   # emit to avoid GitOps drift
  versions:
  - name: v1alpha1                       # DNS-1035
    served: true
    referenceable: true                  # exactly one across all versions; keep served: true
    additionalPrinterColumns:            # optional; yours render before SYNCED/READY/COMPOSITION/AGE
    - {name: LOCATION, type: string, jsonPath: .spec.location}
    schema:
      openAPIV3Schema:                   # REQUIRED in practice (controller errors without it)
        type: object
        properties:
          spec:
            type: object
            required: [<...>]            # appended to Crossplane's
            properties:
              # every property MUST have `type`
              # do NOT define `crossplane` here — it is overwritten
          status:
            type: object
            properties:
              # do NOT define `conditions` here — it is overwritten
  # OMIT `conversion` unless a real webhook exists
  # NEVER emit claimNames or connectionSecretKeys (CEL-rejected)
```

### Client-side validation checklist the builder must implement (nothing below is caught by XRD admission)
1. `metadata.name == names.plural + "." + group`
2. exactly one `versions[].referenceable == true`, and that version has `served: true`
3. every `versions[].name` matches `[a-z]([-a-z0-9]*[a-z0-9])?`
4. every `shortNames[]` / `categories[]` matches the same DNS-1035 regex
5. every `additionalPrinterColumns[].type ∈ {boolean, date, integer, number, string}`; `name`, `type`, `jsonPath` all present
6. every schema property has an explicit `type`
7. no `$ref`; no `uniqueItems: true`; no `additionalProperties` alongside `properties`
8. `spec.crossplane` and `status.conditions` are reserved names
9. `versions[].schema.openAPIV3Schema` is always present
10. `spec.versions[].subresources` accepts only `scale` (writing `status` is a strict-decoding error)

## 11. Scratch artifacts (absolute paths)

All under `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/`:
`xrd-crd.json` / `xrd-crd.yaml` (live XRD CRD), `v1schema.json`, `v2schema.json` (extracted XRD OpenAPI schemas), `xr-crd.json`, `xr-openapi.json` (live derived XQueue CRD), `crd.go`, `schemas.go` (crossplane-runtime `pkg/xcrd` sources), `t1.yaml`–`t24.yaml` (test cases), `conv.sh` (helper: convert → strip ownerRefs → server dry-run).

## 12. Sources

- Live cluster `kind-platform` — Crossplane v2.4.0 (all **[V]** claims)
- [Composite Resource Definitions · Crossplane v2.4](https://docs.crossplane.io/latest/composition/composite-resource-definitions/) — **contains an error on `scope: LegacyCluster` in v2, and overstates schema-field prohibition as rejection rather than overwrite**
- [What's New in v2? · Crossplane](https://docs.crossplane.io/latest/whats-new/)
- [`apis/apiextensions/v2/xrd_types.go` @ v2.4.0](https://github.com/crossplane/crossplane/blob/v2.4.0/apis/apiextensions/v2/xrd_types.go)
- [`apis/apiextensions/v1/conditions.go` @ v2.4.0](https://github.com/crossplane/crossplane/blob/v2.4.0/apis/apiextensions/v1/conditions.go)
- [`internal/controller/apiextensions/definition/reconciler.go` @ v2.4.0](https://github.com/crossplane/crossplane/blob/v2.4.0/internal/controller/apiextensions/definition/reconciler.go)
- [`crossplane-runtime/pkg/xcrd/crd.go`](https://github.com/crossplane/crossplane-runtime/blob/main/pkg/xcrd/crd.go) — read from `main`, not a v2.4.0-pinned tag; behaviour cross-checked against the live cluster and matched exactly
- [`crossplane-runtime/pkg/xcrd/schemas.go`](https://github.com/crossplane/crossplane-runtime/blob/main/pkg/xcrd/schemas.go) — same caveat

## 13. Unverified / caveats

- **`crossplane-runtime` `pkg/xcrd` was read from `main`**, since the module isn't vendored in the `crossplane/crossplane` tree. Every behaviour I attributed to it was independently reproduced against the live 2.4.0 cluster or the 2.5.0 CLI, so the risk of drift is low — but the exact line numbers may differ from what 2.4.0 links.
- **I did not test reading a `scope: LegacyCluster` XRD (created via v1) through the v2 endpoint.** With `conversion.strategy: None` the apiserver only rewrites `apiVersion`, so I *expect* it to return `scope: LegacyCluster` on a v2 read (CEL runs on write, not read) — meaning a tool that lists XRDs via v2 may encounter a `scope` value outside the v2 enum. Untested because I could not create a LegacyCluster XRD under the read-only constraint. Recommend the tool tolerate `LegacyCluster` on read while refusing to emit it under v2.
- **Everything marked "server dry-run" used `kubectl create --dry-run=server`**, which runs full admission and CEL without persisting. No cluster state was modified; `kubectl get xrd -A` still shows only the two pre-existing XRDs.
- The `Responsive` / `WatchCircuitClosed` condition observed on the live XR is Crossplane's realtime-composition watch circuit breaker — outside this brief's scope but relevant if the tool surfaces XR health.