---
name: crossplane-composition-authoring
description: Use when writing or reviewing a Crossplane v2 Composition that uses function-go-templating, or an apiextensions.crossplane.io/v2 CompositeResourceDefinition
---

# Crossplane Composition Authoring

Four facts that make the difference between a Composition that works and one that
renders a silently-wrong resource. Each was verified against Crossplane v2.4 with
function-go-templating v0.12.0, and each contradicts either the upstream docs or
what a competent author would reasonably assume.

Scope: only the things that are counter-intuitive. Ordinary v2 practice — the `v2`
XRD apiVersion, explicit `scope:`, no `claimNames`, the `.m.` namespaced provider
group, `providerConfigRef` needing `kind`, indexing a resource name inside a loop —
is not repeated here.

## 1. `missingkey=error` is mandatory, and goes at the top level

Without it, a template that dereferences a missing XR field renders the **literal
string `<no value>`** into a live managed resource. That string is legal YAML, so
schema validation, `crossplane composition render`, and every downstream gate all
exit 0. It reaches production.

`options` is a sibling of `inline`, **not** nested inside it. The function's own
README shows it nested; that form is a fatal runtime error. Either YAML sequence
form is fine — what matters is the indentation of the `options` key itself.

```yaml
input:
  apiVersion: gotemplating.fn.crossplane.io/v1beta1
  kind: GoTemplate
  source: Inline
  options: ["missingkey=error"]     # sibling of inline, never inside it
  inline:
    template: |
      ...
```

## 2. `{{- with }}` cannot guard an optional field under `missingkey=error`

The two cancel each other. `with` still *evaluates* `$spec.x`, so an absent key
fails the whole render:

```
executing "manifests" at <$spec.maxMessageSize>: map has no entry for key
```

Use `hasKey`. It short-circuits before the lookup, and direct access inside the
taken branch is safe because the key provably exists:

```gotemplate
{{- if hasKey $spec "maxMessageSize" }}
maxMessageSize: {{ $spec.maxMessageSize }}
{{- end }}
```

Do not "fix" this by marking every referenced field `required` in the XRD. That
works, but it silently abolishes optional parameters — any field a template reads
becomes mandatory for the user.

## 3. An XRD `default:` makes a field safe to read directly

Kubernetes defaulting injects the value into the stored XR, so the key is always
present and `missingkey=error` never fires. One declaration covers the schema, the
default, and the template binding — write the default once, in the XRD.

## 4. Composing a native Kubernetes object needs RBAC, and the failure is silent

Crossplane v2 composes any Kubernetes object directly. The control plane must hold
rights on each composed GVK, granted by a ClusterRole labelled:

```yaml
rbac.crossplane.io/aggregate-to-crossplane: "true"     # exact key, quoted "true"
```

`"True"` or an unquoted `true` will not aggregate. Emit all seven verbs
(`get,list,watch,create,update,patch,delete`) — Crossplane's preflight authorizer
loops over exactly that list. Do **not** add `/status` rules for composed
resources; Crossplane never writes their status.

**The trap:** Deployment, Service, Secret, ConfigMap, ServiceAccount and CRDs
already work with no extra RBAC, because core Crossplane needs them for package
management. Everything else — StatefulSet, Job, CronJob, Ingress, PVC, HPA, and
every third-party CRD — is denied and simply hangs. The two most common demo
objects are in the accidental allowlist, so this is badly under-reported.

## Quick reference

| Symptom | Cause |
|---|---|
| `<no value>` in a live resource | `missingkey=error` absent |
| `map has no entry for key` on render | `{{- with }}` on an optional field — use `hasKey` |
| Fatal error parsing the function input | `options` nested under `inline` |
| Composed resource never appears, no error | Missing RBAC for that GVK |
| Fields silently dropped by the API server | Cluster-scoped variant used for a Namespaced XR |
| XRD rejected, or scope not what you expected | `scope:` omitted, or `LegacyCluster` (not valid in v2) |

## Common mistakes

- Trusting the function README's nested `options` example.
- Reaching for `{{- with }}` because it is the idiomatic Go template guard.
- Assuming a composed Deployment that never becomes Ready is a readiness problem —
  check RBAC first, then readiness (a Deployment reports `Available`, not `Ready`,
  so `function-auto-ready` v0.5.0 cannot ready it unaided).
