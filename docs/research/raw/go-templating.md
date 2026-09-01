I have everything verified. Writing the brief.

**Provenance note:** all "VERIFIED" claims below come from running function-go-templating v0.12.0 (image `xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0`, already present in the local Docker store) under `crossplane render` v2.5.0, or from source at tag `v0.12.0` = commit `e249e8cb4e7ae5d58043358f6573c92975874605`. Source paths below are relative to that repo root.

---

# function-go-templating v0.12.0 — exact reference

## Decisions this enables

1. **Emit `options: ["missingkey=error"]` as a top-level input field (not under `inline`), and make it the generator default.** Without it, a missing optional XR spec field silently renders the literal string `<no value>` into the composed resource at *any* depth — `.spec.a.b.c` on a missing `a` does not error, it emits `<no value>`. This is the single highest-value correctness switch in the whole input schema. The upstream README documents `options` in the **wrong place** (nested under `inline`), which is a hard fatal error.
2. **Quote every scalar you interpolate, and never quote integers.** Verified: unquoted `1.10` → `1.1` (float, data loss); unquoted `on`/`yes` → `true`; unquoted `null`/empty → `null`. Annotations are worse — an unquoted integer in `metadata.annotations` is a *fatal* function error, not a silent coercion. A generator must emit `| quote` for string-typed schema fields and bare for integer/boolean-typed fields, driven off the provider CRD's `type:`.
3. **Target `.observed.composite.resource.spec.*` and `.desired.resources.<name>.resource`; there is no `.desired.composed`.** The README's documented path `(index .desired.composed "name")` does not exist — verified `NO`. Also note Crossplane v2 nests XR plumbing under `.observed.composite.resource.spec.crossplane.{compositionRef,resourceRefs}`, so a generated XRD must not define a `spec.crossplane` field of its own.
4. **For nested maps, the only safe idiom is `{{- with <path> }}` + `{{- toYaml . | nindent N }}`.** Verified: `{{ toYaml x | indent 6 }}` (no leading chomp, `indent` not `nindent`) produces broken YAML and a fatal decode error. `with` additionally solves nil-safety — an absent optional block emits nothing rather than `null` or `<no value>`. This is why `with` is the community idiom and it should be the generator's only emitter for object-typed spec fields.
5. **`CompositeConnectionDetails` is a dead end on this user's stack.** They run v2 `Namespaced` XRs; the function still *emits* `desired.composite.connectionDetails` (verified), but the SDK proto states plainly that for modern XRs "this will be ignored." The generator must instead compose an explicit `v1.Secret` resource. Same story for `ClaimConditions` — it works, but there are no claims in their XRD, so `target: CompositeAndClaim` degrades to `Composite`.

---

## 0. Version and provenance

| Fact | Value |
|---|---|
| Tag | `v0.12.0`, commit `e249e8cb4e7ae5d58043358f6573c92975874605`, released 2026-03-22 |
| Installed revision | `function-go-templating-677316af26e4`, `revision: 1`, `State: Active` |
| Image | `xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0` |
| `status.capabilities` | `[composition]` (from `package/crossplane.yaml` `spec.capabilities`) |
| gRPC endpoint | `dns:///function-go-templating.crossplane-system:9443` |
| Go module deps | `sprig/v3 v3.3.0`, `function-sdk-go v0.6.2`, `crossplane-runtime/v2 v2.2.0`, `gopkg.in/yaml.v3 v3.0.1`; `go 1.25.6` |

**v0.12.0 is not the latest.** Latest is **v0.12.4** (2026-08-25). Release notes for v0.12.1–v0.12.4 are **exclusively** Go-runtime and dependency CVE remediation — no functional, schema, or template-function changes. A generator targeting v0.12.0 semantics is safe against v0.12.4, and recommending the bump is free. (A parallel `release-0.11` line also exists, latest `v0.11.9`.)

Both registries serve the identical package — verified `docker manifest inspect` succeeds for both `xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.12.0` and `xpkg.upbound.io/...`. Upstream examples now use `xpkg.crossplane.io`; the user has `xpkg.upbound.io`. Either is fine.

**New in v0.12.0** (from the GitHub release body, i.e. read not run):
- `options` — template options (PR #488)
- YAML error context for better debugging (PR #487)
- Extra resources written into context under `apiextensions.crossplane.io/extra-resources` (PR #486)
- `inline.templates` — multiple inline templates (PR #519)
- Composite resource ready state exposed in function response (PR #496)
- Package API switched `pkg.crossplane.io/v1beta1` → `v1` (PR #521); `spec.capabilities` added (PR #449)
- README/examples updated for v2 connection-details behavior (PR #530); a nil-pointer deref fix (PR #534)

---

## 1. Input schema

Source of truth: `input/v1beta1/input.go`; generated CRD at `package/input/gotemplating.fn.crossplane.io_gotemplates.yaml`.

```go
type GoTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Delims      *Delims                    `json:"delims,omitempty"`
	Source      TemplateSource             `json:"source"`                // REQUIRED
	Inline      *TemplateSourceInline      `json:"inline,omitempty"`
	FileSystem  *TemplateSourceFileSystem  `json:"fileSystem,omitempty"`
	Environment *TemplateSourceEnvironment `json:"environment,omitempty"`
	Options     *[]string                  `json:"options,omitempty"`
}

type TemplateSourceInline     struct { Template string `json:"template,omitempty"`; Templates []string `json:"templates,omitempty"` }
type TemplateSourceFileSystem struct { DirPath string `json:"dirPath,omitempty"` }
type TemplateSourceEnvironment struct { Key string `json:"key,omitempty"` }
type Delims struct { Left *string `json:"left,omitempty"`; Right *string `json:"right,omitempty"` }
```

`TemplateSource` constants (`input/v1beta1/input.go`): `"Inline"`, `"FileSystem"`, `"Environment"`. There are no others.

### Canonical YAML — the form to generate

```yaml
apiVersion: apiextensions.crossplane.io/v1     # Composition API is still v1 in Crossplane 2.4.0
kind: Composition
spec:
  mode: Pipeline
  pipeline:
  - step: render-templates
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline                  # Inline | FileSystem | Environment
      options:                         # OPTIONAL, TOP-LEVEL (not under inline!)
      - missingkey=error
      delims:                          # OPTIONAL
        left: "[["
        right: "]]"
      inline:
        template: |
          <go template text, '---'-separated documents>
```

Other source forms:

```yaml
      source: Inline
      inline:
        templates:                     # slice; joined with "\n---\n"
        - |
          <doc 1>
        - |
          <doc 2>
---
      source: FileSystem
      fileSystem:
        dirPath: /templates            # path INSIDE the function container
---
      source: Environment
      environment:
        key: myTemplate                # key within context["apiextensions.crossplane.io/environment"]
```

### Verified schema behaviors

| Behavior | Evidence |
|---|---|
| Input is unmarshalled **strictly** — any unknown field is fatal | VERIFIED: `cannot get function input *v1beta1.GoTemplate from *v1.RunFunctionRequest: ... json: unable to unmarshal Go value of type v1beta1.GoTemplate: unknown name "bogusField"` |
| `options` under `inline` is fatal (README is wrong) | VERIFIED: `... unknown name "options"` against `v1beta1.TemplateSourceInline` |
| Bogus template option is caught, not a crash | VERIFIED: `cannot apply template options: panic occurred while applying template options: unrecognized option: not-a-real-option`. `safeApplyTemplateOptions` in `fn.go` `recover()`s the panic from `text/template.Option`. |
| `options: ["missingkey=error"]` works | VERIFIED: `cannot execute template: template: manifests:4:24: executing "manifests" at <.observed.composite.resource.spec.a.b.c>: map has no entry for key "a"` |
| `template` **and** `templates` both set → `template` silently wins | VERIFIED (rendered `a: fromTemplate`, `templates` ignored). The CEL rule `Exactly one of 'template' or 'templates' must be set` exists **only in the generated CRD, which is never installed** — `input.go` comment: "we never install its CRD." Runtime code (`template.go` `newInlineSource`) just prefers `Template`. **Do not rely on this validation.** |
| Neither set → `inline.template or inline.templates should be provided` | read (`template.go`) |
| Missing `source` → `source is required`; unknown → `invalid source: %s` | read (`template.go` `NewTemplateSourceGetter`) |
| `FileSystem` with a nonexistent dir | VERIFIED: `invalid function input: cannot read tmpl from the folder {/templates}: open /templates: no such file or directory` |
| `Environment` when the env context key is absent | VERIFIED: `invalid function input: cannot read tmpl from the environment: apiextensions.crossplane.io/environment key does not exist in context` |
| `Environment` source end-to-end | VERIFIED via `--context-values='apiextensions.crossplane.io/environment={"myTemplate":"..."}'` — template rendered, output `fromEnvTemplate: eu-north-1` |
| `delims` requires **both** left and right | read: `if delims.Left != nil && delims.Right != nil { tpl = tpl.Delims(...) }` (`function_maps.go`). Setting only one is silently ignored. CRD defaults are `{{` / `}}`. |

**`FileSystem` traversal** (`template.go` `readTemplates`): walks `dirPath` recursively, **skips hidden directories and hidden files** (leading `.`, `dotCharacter = 46`), concatenates every remaining file's contents and appends `"\n---\n"` after each. There is no extension filter — every non-hidden file is treated as a template.

**Undocumented CLI escape hatch** (`main.go`): `--default-source` / env `FUNCTION_GO_TEMPLATING_DEFAULT_SOURCE`. When set and the Composition input omits `source`, `fn.go` forces `source: FileSystem` with `fileSystem.dirPath` = that value. Relevant only if you ship a custom `DeploymentRuntimeConfig`. Other flags: `--debug/-d`, `--network` (default `tcp`), `--address` (default `:9443`), `--tls-certs-dir` (env `TLS_SERVER_CERTS_DIR`), `--insecure`, `--max-recv-message-size` (default 4 MB).

---

## 2. Custom template functions — the exact v0.12.0 list

From `function_maps.go`, `getFunctions()` plus `initInclude`. **Eleven** functions. Exact Go signatures:

| Template name | Go signature | Returns / notes |
|---|---|---|
| `randomChoice` | `randomChoice(choices ...string) string` | Uniform pick. Non-deterministic per reconcile — see footguns. |
| `toYaml` | `toYaml(val any) (string, error)` | `yaml.Marshal` (gopkg.in/yaml.v3). Trailing newline included. |
| `fromYaml` | `fromYaml(val string) (any, error)` | `yaml.Unmarshal` into `any`. |
| `getResourceCondition` | `getResourceCondition(ct string, res map[string]any) xpv1.Condition` | Returns a **Go struct** — access with capitalized fields. |
| `setResourceNameAnnotation` | `setResourceNameAnnotation(name string) string` | Returns the literal string `gotemplating.fn.crossplane.io/composition-resource-name: <name>` — a whole YAML *line*, not a value. |
| `getComposedResource` | `getComposedResource(req map[string]any, name string) map[string]any` | `nil` if absent. |
| `getCompositeResource` | `getCompositeResource(req map[string]any) map[string]any` | `nil` on error. |
| `getExtraResources` | `getExtraResources(req map[string]any, name string) []any` | `nil` if absent. |
| `getExtraResourcesFromContext` | `getExtraResourcesFromContext(req map[string]any, name string) []any` | Reads the **context** key, not the request. |
| `getCredentialData` | `getCredentialData(mReq map[string]any, credName string) map[string][]byte` | Base64-**decoded** bytes. **Omitted from the README table** but present in source and has an example dir. |
| `include` | `include(name string, data any) (string, error)` | Renders a `define`d template to a string. |

### Verified return values

All confirmed in one render against a mocked observed `Queue` named `the-queue`:

```
a_getComposedResource:         arn:aws:sqs:eu-north-1:123456789012:my-queue
b_getComposedResource_missing: NIL          # nil → falsy, safe for {{ if }}
c_getCompositeResource:        eu-north-1
d_cond_status:                 True         # (getResourceCondition "Ready" (getComposedResource . "the-queue")).Status
e_cond_via_observed:           Available    # .Reason, arg = (index .observed.resources "the-queue")
f_cond_missing:                Unknown      # missing condition type → Status "Unknown", never an error
g_cond_type:                   Ready        # .Type
h_randomChoice:                only
i_fromYaml:                    42
m_include:                     {"a":"eu-north-1","b":5}
n_setResourceNameAnnotation:   gotemplating.fn.crossplane.io/composition-resource-name: demo
```

**`getResourceCondition` returns `xpv1.Condition`, a Go struct — fields are PascalCase, not the JSON names.** From `crossplane-runtime/v2@v2.2.0/apis/common/condition.go`:

```go
type Condition struct {
	Type               ConditionType          // .Type
	Status             corev1.ConditionStatus // .Status  → "True"/"False"/"Unknown"
	LastTransitionTime metav1.Time            // .LastTransitionTime
	Reason             ConditionReason        // .Reason
	Message            string                 // .Message
	ObservedGeneration int64                  // .ObservedGeneration
}
```

`{{ .status }}` (lowercase) yields nothing. A generator emitting condition checks **must** use `.Status`.

`getResourceCondition` accepts **either** shape (`function_maps.go`): it first tries fieldpath `resource.status` (i.e. pass `(index .observed.resources "n")`), then falls back to `status` (i.e. pass `(getComposedResource . "n")`). Both verified working.

**Dotted resource names are safe.** `getComposedResource` builds fieldpath `observed.resources[%s]resource` (note: no `.` before `resource` — unusual but correct crossplane-runtime fieldpath syntax). VERIFIED with resource name `my.dotted-name`: both `getComposedResource . "my.dotted-name"` and `(index .observed.resources "my.dotted-name").resource...` returned the value.

**`include` recursion guard:** `recursionMaxNums = 1000`; exceeding it yields `rendering template has a nested reference name: %s`.

**Function registration order** (`GetNewTemplateWithFunctionMaps`, `function_maps.go`): custom map first → `include` → **Sprig last**. Sprig therefore *overrides* on a name collision. No collision exists in v3.3.0 (verified: `toYaml`, `fromYaml`, `include` are not Sprig names), but this ordering is load-bearing if either side ever adds a name.

---

## 3. Sprig availability

- Sprig **v3.3.0**, providing **211** functions (VERIFIED by compiling a program against `github.com/Masterminds/sprig/v3@v3.3.0` and printing `len(sprig.FuncMap())`).
- **Exactly two are deleted** (`function_maps.go`), with this source comment: *"Sprig's env and expandenv can lead to information leakage (injected tokens/passwords). Both Helm and ArgoCD remove these due to security implications."*
  - `env` — VERIFIED fatal: `invalid function input: cannot parse the provided templates: template: manifests:4: function "env" not defined`
  - `expandenv` — VERIFIED fatal: `... function "expandenv" not defined`
- **209 Sprig functions remain**, plus 11 custom, plus Go's built-ins (`and`, `or`, `not`, `len`, `index`, `slice`, `print`, `printf`, `println`, `call`, `html`, `js`, `urlquery`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`).

Sprig functions a generator will actually reach for, all verified working: `quote`, `squote`, `b64enc`, `b64dec`, `indent`, `nindent`, `default`, `dig`, `hasKey`, `keys`, `join`, `toJson`, `fromJson`, `mustToJson`, `toPrettyJson`, `toRawJson`, `toString`, `int`, `until`, `ternary`, `semver`, `trim`, `merge`, `dict`, `list`, `lower`, `upper`, `trunc`, `sha256sum`, `uuidv4`, `randAlphaNum`.

**`toYaml`/`fromYaml` are NOT Sprig** — verified absent from the 211. They exist only because this function adds them. `toJson`/`fromJson`/`mustToJson`/`toPrettyJson`/`toRawJson` **are** Sprig.

**Security note worth flagging:** the removals stop at `env`/`expandenv`. Still reachable and network- or entropy-active: **`getHostByName`** (performs a live DNS lookup — an exfiltration channel from a template), `genPrivateKey`, `genCA`, `genSelfSignedCert`, `bcrypt`, `derivePassword`, `encryptAES`/`decryptAES`, `randBytes`, `uuidv4`. If the generator ever renders untrusted template text, these are the ones to lint against.

---

## 4. Template context — exact dotted paths

The template's dot `.` is the **entire `RunFunctionRequest`**, protojson-marshalled to `map[string]any` (`fn.go` `convertToMap`). Keys are therefore **lowerCamelCase protojson names**, not proto snake_case.

**Top-level keys — VERIFIED by `{{ range $k, $v := . }}`:**

```
context          always present (may be {})
desired          always present (may be {})
input            always present — your own GoTemplate input, reflected back
meta             always present — {tag, capabilities}
observed         always present
credentials      ONLY when the pipeline step declares `credentials:`
extraResources   ONLY after requirements are satisfied (2nd invocation)
requiredResources ONLY after requirements are satisfied (2nd invocation)
requiredSchemas  never populated by this function (it never requests schemas) — INFERRED, not observed
```

### Verified paths

```
.observed.composite.resource                      # full XR: apiVersion, kind, metadata, spec, status
.observed.composite.resource.metadata.name
.observed.composite.resource.metadata.namespace   # populated for v2 Namespaced XRs
.observed.composite.resource.metadata.uid
.observed.composite.resource.metadata.labels."crossplane.io/composite"
.observed.composite.resource.spec.<your XRD fields>
.observed.composite.resource.spec.crossplane.compositionRef.name    # v2 nesting
.observed.composite.resource.spec.crossplane.resourceRefs[]         # v2 nesting
.observed.composite.resource.status.conditions[]
.observed.composite.connectionDetails              # map[string]<base64 string>, when the XR has any

.observed.resources                                # map keyed by composition-resource-name
.observed.resources.<name>.resource                # full observed MR incl. status.atProvider
.observed.resources.<name>.connectionDetails       # map[string]<base64 string>
# .observed.resources.<name>.ready is NEVER set on the request (proto: "Crossplane will never set this field in a RunFunctionRequest")

.desired.composite.resource                        # PARTIAL — only what prior steps set
.desired.composite.connectionDetails
.desired.resources.<name>.resource                 # PARTIAL
.desired.resources.<name>.ready                    # "READY_TRUE" | "READY_FALSE" | "READY_UNSPECIFIED"

.context.<key>                                     # arbitrary; use index for keys with dots/slashes
.credentials.<name>.credentialData.data.<key>      # base64 STRING (getCredentialData returns decoded bytes)

.extraResources.<key>.items[].resource
.requiredResources.<key>.items[].resource
.meta.tag                                          # opaque request hash
.meta.capabilities[]                               # e.g. CAPABILITY_REQUIRED_RESOURCES, CAPABILITY_CREDENTIALS, ...
```

**`.desired.composed` does not exist.** VERIFIED: `has_desired_composed_README: "NO"` vs `has_desired_resources: "YES"`, `desired_res_keys: the-queue`. The README's bullet `{{ (index .desired.composed "resource-name").resource.spec.widgets }}` is a documentation bug. Backed by the proto (`function-sdk-go@v0.6.2/proto/v1/run_function.proto`): `message State { Resource composite = 1; map<string, Resource> resources = 2; }`.

**Verified `.desired` dump** after a first pipeline step:
```yaml
composite:
    connectionDetails: {queueUrl: aHR0cHM6Ly9leGFtcGxlLmNvbS9x}
    resource: {status: {phase: Provisioning}}
resources:
    the-queue:
        ready: READY_TRUE
        resource:
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata: {annotations: {}}     # <-- meta annotations stripped, empty map remains
            spec: {forProvider: {...}}
```

**`.environment` does not exist as a top-level key.** In Crossplane v2.4.0 the composition environment lives only in the **context**, at `index .context "apiextensions.crossplane.io/environment"`, and only if an earlier step (`function-environment-configs`) put it there. VERIFIED on the live cluster:
- `kubectl explain composition.spec.environment` → `error: field "environment" does not exist` (Composition CRD serves only `v1`). The native `spec.environment` block is **gone** in v2.
- `EnvironmentConfig` CRD **does** still exist: `environmentconfigs.apiextensions.crossplane.io`.
- The context key string is unchanged — hardcoded in `template.go` (`newEnvironmentSource`) and `extraresources.go`.

### Multiple composed resources

The rendered output is decoded with `k8s.io/apimachinery/pkg/util/yaml.NewYAMLOrJSONDecoder` over the whole buffer (`fn.go`). Documents are separated by a line that is exactly `---` (after trimming). Each non-meta document becomes one desired composed resource, keyed by its `gotemplating.fn.crossplane.io/composition-resource-name` annotation.

**VERIFIED:** leading `---`, trailing `---`, consecutive `---`, and documents emptied by `{{- if false }}...{{- end }}` are all tolerated and skipped. A template may freely open and close with `---`. `inline.templates` entries are simply joined with `"\n---\n"` (`template.go`), so the two forms are exactly equivalent.

---

## 5. ExtraResources / RequiredResources

Declare requirements by emitting a meta document (`fn.go`, `extraresources.go`):

```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ExtraResources
requirements:
  <key>:
    apiVersion: v1          # required
    kind: ConfigMap         # required
    matchName: team-settings   # OR matchLabels (matchLabels ignored if matchName set)
    matchLabels:
      app: my-app
    namespace: team-a       # omit for cluster-scoped
```

Field names come from `ExtraResourcesRequirement` in `extraresources.go` (camelCase, deliberately, to avoid the proto's snake_case): `APIVersion`→`apiVersion`, `Kind`→`kind`, `MatchLabels`→`matchLabels`, `MatchName`→`matchName`, `Namespace`→`namespace`.

`fn.go` writes each requirement into **both** `requirements.Resources[k]` (v2) and — **only when `namespace == ""`** — the deprecated `requirements.ExtraResources[k]` (v1 compat). So a namespaced requirement is v2-only. Duplicate keys are fatal: `duplicate extra resource key %q`.

### THE critical footgun: extra resources arrive on the *second* invocation

**VERIFIED.** A template that declares a requirement *and* dereferences `.extraResources` unguarded fails on the first pass:

```
cannot execute template: template: manifests:18:20: executing "manifests" at
<index .extraResources "settings">: error calling index: index of untyped nil
```

Crossplane invokes the function, receives `requirements`, fetches the resources, then invokes it **again** with them populated. Every read must be guarded. Verified working, second pass:

```
len_getExtraResources:     1
has_extraResources_key:    YES
has_requiredResources_key: YES
tier_via_helper:  gold   # {{- with (getExtraResources . "settings") }}{{ (index . 0).resource.data.tier }}{{ end }}
tier_via_path:    gold   # {{- with .extraResources }}{{ (index (index . "settings").items 0).resource.data.tier }}{{ end }}
```

**Both `extraResources` and `requiredResources` are present** on the second pass. `convertToMap` (`fn.go`) aliases them:

```go
_, ok := mReq["extraResources"]
if !ok {
    if r, ok := mReq["requiredResources"]; ok { mReq["extraResources"] = r }
}
```

So `.extraResources` is *always* populated regardless of which proto field the server used. `getExtraResources` covers the same ground from the other direction, trying fieldpath `requiredResources[%s].items` first, then `extraResources[%s].items`. **Prefer `getExtraResources` in generated code** — it is version-agnostic by construction.

### `getExtraResourcesFromContext` is for a different job

**VERIFIED:** in the same step that declares the requirement, `len_getERFromContext: 0` and `has_ctx_er_key: NO`, even though `getExtraResources` returned 1. It reads `context["apiextensions.crossplane.io/extra-resources"][<name>].items`, which this function writes to the **response** context (new in v0.12.0). It is only useful in a **later pipeline step**, or to consume what `function-extra-resources` deposited. The final rendered context confirms the write:

```yaml
apiextensions.crossplane.io/extra-resources:
  settings:
    items:
    - resource: {apiVersion: v1, kind: ConfigMap, data: {tier: gold}, metadata: {...}}
```

Merging is shallow (`maps.Copy` in `mergeStructs`) — a later step's key wholly replaces an earlier one's.

### Using function-extra-resources alongside

Order the pipeline `function-extra-resources` → `function-go-templating`, and read via `getExtraResourcesFromContext . "<key>"` (or `index .context "apiextensions.crossplane.io/extra-resources"`). Both functions use the identical context key, so they compose. For a self-contained generator, the built-in `ExtraResources` meta kind removes the dependency entirely — recommended.

Local testing flag: `crossplane render ... --required-resources=<file|dir>` (short `-e`). The older `--extra-resources` spelling is gone in CLI v2.5.0.

---

## 6. Meta kinds

All use `apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1` (constant `metaAPIVersion` in `fn.go`). Four kinds are handled.

| Kind | Payload field | Effect |
|---|---|---|
| `CompositeConnectionDetails` | `data` (map, **base64 values**) | Sets `desiredComposite.ConnectionDetails` |
| `Context` | `data` (map) | Deep-merges into the response context (`mergo.WithOverride`) |
| `ExtraResources` | `requirements` (map) | Sets `rsp.Requirements` |
| `ClaimConditions` | `conditions` (list) | Appends to `rsp.Conditions` |

**The error message for an unknown kind omits `ClaimConditions`.** VERIFIED verbatim:

```
invalid kind "Bogus" for apiVersion "meta.gotemplating.fn.crossplane.io/v1alpha1"
 - must be one of CompositeConnectionDetails, Context or ExtraResources
```

`ClaimConditions` nonetheless works — the `switch` in `fn.go` handles it; only the `default:` branch's message string is stale. Do not treat this message as the authoritative kind list.

### CompositeConnectionDetails

```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: CompositeConnectionDetails
data:
  queueUrl: {{ (index $.observed.resources "the-queue").resource.status.atProvider.url | b64enc }}
```

`fn.go` base64-**decodes** each value before storing, so **values must be base64**. VERIFIED: emitting `{{ "https://example.com/q" | b64enc }}` produced `desired.composite.connectionDetails.queueUrl: aHR0cHM6Ly9leGFtcGxlLmNvbS9x`. Values already read from a composed resource's `connectionDetails` are base64 already; anything else needs `| b64enc`.

**Ignored for v2 XRs.** The function emits it regardless (verified against a v2 Namespaced XR), but `run_function.proto` on `Resource.connection_details` states: *"A function should set this field in a RunFunctionResponse to indicate the desired connection details of legacy XRs. For modern XRs, this will be ignored."* The README agrees. **For this user's stack, compose an explicit Secret:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    {{ setResourceNameAnnotation "connection-secret" }}
{{- with (index $.observed.resources "the-queue") }}
data:
  queueUrl: {{ .resource.status.atProvider.url | b64enc }}
{{- else }}
data: {}
{{- end }}
```

### Context

```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: Context
data:
  myKey: {value}
  "apiextensions.crossplane.io/environment":   # match the key to update the environment
    someField: newValue
```

`MergeContext` (`context.go`) does `mergo.Merge(&mergedContext, val, mergo.WithOverride)` against the **request** context, then `response.SetContextKey` per top-level key.

### ClaimConditions

```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ClaimConditions
conditions:
- type: QueueReady          # PascalCase
  status: "True"            # "True" | "False" | "Unknown" (string, quote it)
  reason: Provisioned       # machine-readable PascalCase
  message: all good         # optional
  target: CompositeAndClaim # "Composite" (default) | "CompositeAndClaim"
```

VERIFIED — the condition appeared on the rendered XR:
```yaml
- lastTransitionTime: "2024-01-01T00:00:00Z"
  message: all good
  reason: Provisioned
  status: "True"
  type: QueueReady
```

Reserved types are rejected. VERIFIED with `type: Ready`:
```
cannot set ClaimCondition type: Ready is a reserved Crossplane Condition
```
Reserved set is `Ready`, `Synced`, `Healthy` (`IsSystemConditionType`, `crossplane-runtime/v2@v2.2.0/apis/common/condition.go:131`). Any `status` value other than `"True"`/`"False"` silently becomes `STATUS_CONDITION_UNKNOWN` (`claimconditions.go` `transformCondition` — `default:` branch), and any `target` other than `CompositeAndClaim` silently becomes `TARGET_COMPOSITE`. **Neither typo is reported.** On this user's XRD (no claims) `CompositeAndClaim` is effectively `Composite`.

### Composite status vs. composed resource — the special rule

`fn.go`: if a rendered document's `apiVersion` **and** `kind` match the observed composite **and** it carries no `composition-resource-name` annotation, the function merges its `status` into the desired composite (via `mergo.WithOverride`) and **does not create a composed resource**. Adding the annotation flips it to creating a composed resource of the XR's own type (recursive XRs). VERIFIED: a bare `XQueue` document with `status: {phase: Provisioning}` produced `status.phase: Provisioning` on the XR and no extra resource.

Status is only written when either side is non-empty, so an empty `status: {}` is a no-op.

### Readiness

```yaml
metadata:
  annotations:
    {{ setResourceNameAnnotation "the-queue" }}
    gotemplating.fn.crossplane.io/ready: "True"
```

Values are **case-sensitive**: exactly `True`, `False`, `Unspecified`. VERIFIED with `"true"`:
```
invalid function input: invalid "gotemplating.fn.crossplane.io/ready" annotation value "true": must be True, False, or Unspecified
```
Verified effect: `ready: READY_TRUE` in `.desired.resources.<n>.ready`. The annotation also works on the composite document (new in v0.12.0, PR #496). Both meta annotations are stripped from the desired resource before it is emitted — but note `metadata.annotations: {}` is left behind as an empty map. The ready annotation is deliberately **not** honored on `metaAPIVersion` documents.

Missing name annotation is fatal. VERIFIED:
```
"Queue" template is missing required "gotemplating.fn.crossplane.io/composition-resource-name" annotation
```

---

## 7. Footguns — all verified by running them

### 7.1 `<no value>`: the silent one

Default Go template `missingkey` behavior on `map[string]any` is to substitute the literal text `<no value>`. **This happens at any depth and never errors.** VERIFIED:

| Expression | Result |
|---|---|
| `{{ .observed.composite.resource.spec.doesNotExist }}` | `<no value>` |
| `{{ .observed.composite.resource.spec.a.b.c }}` (no `a` at all) | `<no value>` — no nil-pointer error |
| `{{ if .observed...spec.missing }}Y{{else}}N{{end}}` | `N` — safe |
| `{{ .observed...spec.missing \| default "fallback" }}` | `fallback` — safe |
| `{{ dig "spec" "missing" "digfallback" .observed.composite.resource }}` | `digfallback` — safe |
| `{{- with .observed...spec.missing }}SET{{end}}` | `` (empty) — safe |

A generator that naively interpolates an optional XRD field will write `region: <no value>` into a real managed resource. **Mitigations, in order of preference:** (a) `options: ["missingkey=error"]` at the input top level; (b) wrap every optional field in `{{- with }}`; (c) `| default`.

Note that `missingkey=error` is strict — it will also fire on legitimately-absent optional fields, so it pairs with (b) rather than replacing it. `with`/`if`/`default`/`dig` all still work under it because they test rather than render the missing key... **flagged as unverified:** I confirmed `missingkey=error` fires on a bare deref, but did not separately confirm that `dig`/`default` remain safe *under* `missingkey=error`. Worth a 30-second test before the generator hard-codes the combination.

### 7.2 YAML type coercion on unquoted scalars

VERIFIED in one render — XR spec values on the left, what landed in the composed resource on the right:

| XR spec (all strings) | Unquoted interpolation | With `\| quote` |
|---|---|---|
| `versionish: "1.10"` | `1.1` ← **data loss** | `"1.10"` |
| `onish: "on"` | `true` | `"on"` |
| `yesish: "yes"` | `true` | `"yes"` |
| `nullish: "null"` | `null` | `"null"` |
| `emptyish: ""` | `null` | `""` |
| `delaySeconds: 5` (int) | `5` (int, correct) | `"5"` ← **wrong type** |

The decoder follows YAML 1.1 booleans (`on`/`yes` → `true`). **Rule for the generator: emit `| quote` iff the XRD/CRD schema type is `string`; emit bare for `integer`, `number`, `boolean`.** Getting this backwards fails provider CRD validation in one direction and silently corrupts values in the other.

### 7.3 Non-string annotation values are FATAL

VERIFIED:
```
invalid annotations in resource 'sqs.aws.m.upbound.io/v1beta1, Kind=Queue resource-name=q':
.metadata.annotations accessor error: contains non-string value in the map under key "my-count":
5 is of the type int64, expected string
```
`fn.go` explicitly guards this with `unstructured.NestedStringMap`. **Every templated annotation value needs `| quote`**, unconditionally — annotations are always strings. Same applies to labels.

### 7.4 Indentation: `nindent` and the leading chomp

VERIFIED failure with `{{ toYaml .spec.tags | indent 6 }}` under a `tags:` key:
```
cannot decode manifest: error converting YAML to JSON:
yaml: line 16 (document 1, line 15) near: 'Team: platform': did not find expected key
```

VERIFIED success with the correct idiom:
```yaml
    tags:
      {{- toYaml .observed.composite.resource.spec.tags | nindent 6 }}
```
→
```yaml
    tags:
      Env: prod
      Team: platform
```

Why: `indent N` prefixes *every* line including the first, so the first line gets the template's own literal indentation **plus** N. `nindent N` emits a leading newline then indents every line by exactly N, making the result independent of where the action sits. The `{{-` chomps the preceding whitespace/newline so `nindent`'s own newline is the only one. **Rule: always `{{- toYaml X | nindent N }}`, where N = the nesting depth of the parent key + 2.**

The `(document N, line M)` error context is new in v0.12.0 and is genuinely useful — it reports both the absolute line in the rendered buffer and the line within the offending document, plus the offending text (truncated at 80 chars).

### 7.5 Why `{{- with }}` is the idiom

It solves three problems at once, all verified:
1. **Nil-safety** — the block is skipped entirely for a missing/empty value, so no `<no value>`, no `null`, no empty parent key.
2. **Rebinding** — inside, `.` *is* the value, so `{{- toYaml . | nindent 6 }}` works without repeating a long path.
3. **Whitespace** — the `{{-` chomp prevents a stray blank line where the block would have been.

```yaml
    {{- with .observed.composite.resource.spec.redrive }}
    redrivePolicy:
      {{- toYaml . | nindent 6 }}
    {{- end }}
```
VERIFIED: present → correct nested map; absent → **nothing emitted at all** (no `neverEmitted:` key). Note the gotcha inherited from Go: `with` rebinds `.`, so reach the outer scope with `$` (e.g. `$.observed...`), which is why upstream examples are littered with `$`.

Caveat: `with` treats `0`, `false`, and `""` as absent. For a genuinely optional integer that may legitimately be `0`, use `{{ if hasKey .spec "field" }}` instead.

### 7.6 Other verified traps

- **`randomChoice` is not idempotent.** Seeded from `time.Now().UnixNano()` per call. Using it for a value that lands in a managed resource causes an infinite update loop. Only safe when the result is immediately captured into observed state and thereafter read back (as the upstream `example/inline` does via `index $.observed.resources ...`).
- **`inline.template` silently beats `inline.templates`** — the CEL guard is never installed.
- **`delims` needs both halves** or is silently ignored.
- **`ClaimConditions` typos in `status`/`target` are silently coerced**, not reported.
- **The generated CRD is never installed**, so *no* input validation happens server-side at Composition apply time. All input errors surface only at reconcile/render time. A generator should validate its own output.
- **`{{ setResourceNameAnnotation "x" }}` emits an entire `key: value` line.** It must sit at annotation-map indentation on its own line — not after a `key:`. It is not a value expression.
- **YAML block scalars inside templates are fragile.** Emitting `foo: |` followed by templated content requires the content be indented relative to `foo`; `nindent` is the only reliable tool. I hit this myself while writing the test harness.
- `crossplane beta render` in the README is stale; CLI v2.5.0 uses `crossplane render`.

---

## 8. Documentation defects found in v0.12.0 (source/behavior is authoritative)

| # | README says | Reality (verified) |
|---|---|---|
| 1 | `options` nested under `inline:` | Top-level on `GoTemplate`. Nested = fatal `unknown name "options"`. |
| 2 | `{{ (index .desired.composed "name").resource... }}` | No `.desired.composed`. It is `.desired.resources`. |
| 3 | Additional Functions table lists 10 | 11 exist; **`getCredentialData` is missing from the table** (it has an example dir). |
| 4 | `crossplane beta render` | `crossplane render` in CLI v2.5.0. |
| 5 | — | The `invalid kind` runtime error omits `ClaimConditions`, which is supported. |
| 6 | ExtraResources JSON sketch shows `"key": [ ... ]` | Actual shape is `"key": {"items": [ {"resource": {...}} ]}`. The prose examples elsewhere in the README use `.items` correctly. |

---

## 9. What I could not confirm

- **`requiredSchemas` in the template context.** `meta.capabilities` includes `CAPABILITY_REQUIRED_SCHEMAS`, and the proto defines `required_schemas` on the request, but v0.12.0 has **no code** that populates `requirements.schemas` — so the key should never appear. Inferred from source, not observed.
- **`.observed.resources.<n>.connectionDetails` populated.** The path is in the proto and used by upstream's `example/custom-delims`; `crossplane render -o` gave me no way to inject connection details on a mocked observed resource, so I saw only `NO`. The path is certainly correct; I did not see it non-empty.
- **Whether the live Crossplane v2.4.0 server sends `requiredResources` vs `extraResources`.** `crossplane render` v2.5.0 sent both (verified `has_requiredResources_key: YES`, `has_extraResources_key: YES`), but the alias in `convertToMap` makes this moot for template authors. I did not deploy to the cluster to observe the server directly (read-only constraint).
- **`dig`/`default` behavior *under* `options: ["missingkey=error"]`** — tested separately, not in combination. See §7.1.
- Sprig's own internal behavior changes between v3.2.x and v3.3.0 — out of scope; I pinned and enumerated v3.3.0 exactly, which is what v0.12.0 vendors.

---

## 10. Reference skeleton for the generator

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
  - step: render-templates
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      options:
      - missingkey=error
      inline:
        template: |
          ---
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              {{ setResourceNameAnnotation "queue" }}
          spec:
            forProvider:
              # required string
              region: {{ .observed.composite.resource.spec.region | quote }}
              # optional integer
              {{- with .observed.composite.resource.spec.delaySeconds }}
              delaySeconds: {{ . }}
              {{- end }}
              # optional object
              {{- with .observed.composite.resource.spec.tags }}
              tags:
                {{- toYaml . | nindent 6 }}
              {{- end }}
            providerConfigRef:
              kind: ClusterProviderConfig
              name: default
          ---
          # status write-back: same apiVersion+kind as the XR, NO name annotation
          apiVersion: platform.sparky.ee/v1alpha1
          kind: XQueue
          status:
            {{- with (getComposedResource . "queue") }}
            queueUrl: {{ dig "status" "atProvider" "url" "" . | quote }}
            ready: {{ (getResourceCondition "Ready" .).Status | quote }}
            {{- end }}
  - step: automatically-detect-ready-composed-resources
    functionRef:
      name: function-auto-ready
```

This renders cleanly on the user's exact stack (Crossplane 2.4.0 server, v2 Namespaced XRD, no claims, `sqs.aws.m.upbound.io/v1beta1`), which is the shape I used throughout the verification runs above.

**Scratch artifacts left on disk** (all read-only work; the cluster was not modified):
- `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/fgt012/` — verified v0.12.0 source checkout
- `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/rt/` — 23 render test cases (`c1.yaml`–`c23.yaml`, `xr.yaml`, `xr2.yaml`, `observed.yaml`, `obs2.yaml`, `obs3.yaml`, `extra.yaml`, `creds.yaml`, `functions.yaml`)
- `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/sprigls/` — Sprig v3.3.0 enumeration program