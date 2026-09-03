# Composition test run — `CachedService`, namespaced, cluster round-trip

**Date:** 2026-09-04
**Tester:** composition-tester agent (outsider persona; CLI only, no source read before the run)
**Worktree:** `.claude/worktrees/agent-a287eb9767f100a9d`, branch `worktree-agent-a287eb9767f100a9d`
**Binary:** `bin/cf` at `v0.9.0-40-g0d3e914`
**Cluster:** pre-existing shared `kind-cf-test`, Crossplane server v2.4.0, `crossplane` CLI v2.5.0
**Workspace isolation:** namespace `cf-agent-a287eb9767f100a9d-089857`, group suffix `w089857.cf-test`
**Artifacts:** [`2026-09-04-cachedservice-namespaced-roundtrip/`](2026-09-04-cachedservice-namespaced-roundtrip/)

---

## Mission and outcome

**Outcome: completed with workaround.**

Every step of the mission ran to completion: a namespaced `CachedService` XR composing a
ServiceAccount, a Deployment, a Service, a HorizontalPodAutoscaler, two `forEach` ConfigMaps
and a `when`-guarded ElastiCache `Cluster` was authored from `cf init`, generated, rendered
with the real `crossplane` CLI, applied to a live kind cluster where all seven composed
objects were created, read back with `kubectl get -o yaml`, re-adopted with `cf adopt`, and
regenerated. The round trip did **not** reproduce the original bytes.

**Wall clock to first correct `crossplane composition render`: 5 minutes 07 seconds**
(21:24:34Z `make build` → 21:29:41Z green render). The workaround inside that window was
forced by finding **CF-048**: a numeric `Service.spec.ports[].targetPort` cannot be expressed
in this DSL at all, so the blueprint had to be rewritten to use a *named* container port.

---

## Narrative

I read `AGENTS.md`, the authoring skill, the backlog-authoring skill, `docs/cli.md` and
`docs/dsl.md`, built the binary, and started from `cf init cachedservice.cf.yaml`. The
scaffold is honest and small — a Namespaced XRD with `providerName` already declared — and
the "next:" line it prints (`cf kinds, cf provider add, or cf gen`) is exactly the right
next question. No hesitation here.

I wanted a real OSS managed resource, so I ran `cf catalogue redis` and `cf catalogue cache`.
Both returned in under a second with a short, readable table;
`ghcr.io/crossplane-contrib/provider-aws-elasticache:v2.7.0` serving a `Cluster` kind was the
obvious fit for a thing called `CachedService`. `cf provider add` on it took 1.7 seconds and
wrote the digest into `.cf.lock`. That is a genuinely good first five minutes.

`cf fields elasticache.aws.m.upbound.io/v1beta1/Cluster --required` told me only `region` is
required, and `--status` gave me the wire targets. I picked `atProvider.clusterAddress`
because it is a scalar leaf and reads like a cache endpoint. Here is my first guess: I could
not tell from `docs/dsl.md` whether a status wire is legal in a `fields:` leaf that sits
inside a list element (`spec.template.spec.containers[0].env[0].value`). The docs show status
wires in flat `fields` and in `annotations`, never inside an indexed path. I tried it anyway
and it worked — but that guess is where finding **CF-046** came from, so it was a productive
one.

I then hand-wrote the whole blueprint rather than using the canvas, because that is what a
user with an editor open actually does. Nine parameters (two of them exercising the traps the
authoring skill names: `cacheNodeType` optional-with-no-default, and `image`/`replicas`/
`maxReplicas`/`shardCount`/`enableCache` carrying XRD defaults), `when: params.enableCache` on
the managed cache, `forEach: params.shardCount` on the ConfigMaps, and a
HorizontalPodAutoscaler specifically because the skill says HPAs are *not* in Crossplane's
accidental RBAC allowlist and I wanted to see whether `rbac.yaml` would notice.

First `cf gen` was clean. I read all four outputs against the four counter-intuitive facts:

- `options: ["missingkey=error"]` is a sibling of `inline`. Correct.
- The one optional parameter with no default is guarded: `{{- if hasKey $spec "cacheNodeType" }}`.
  Correct, and `with` is nowhere in the output.
- The XRD is `apiextensions.crossplane.io/v2`, `scope: Namespaced`, no `claimNames`, defaults
  in the schema. Correct.
- `rbac.yaml` contains **exactly one** rule — `autoscaling/horizontalpodautoscalers`, all
  seven verbs, no `/status`, label `rbac.crossplane.io/aggregate-to-crossplane: "true"`
  quoted. It did not emit noise rules for the four pre-granted kinds. That is the trap
  handled properly, and I later proved it end-to-end: the HPA was created in the cluster.

Then `cf gen --validate` refused, with two complaints. One was my own fault
(`data[shard]: {raw: '{{ $i }}'}` renders an integer into a `map[string]string`) and the
validator caught it before Kubernetes did — good. The other was
`targetPort: {value: 8080}`, which the validator called `invalid type: expected string, got
integer 8080`. I did not believe it, so I checked the live API server: `targetPort: 8080` is
accepted, `targetPort: "8080"` is rejected outright. I then tried `{raw: "8080"}` as an
escape hatch and got the same refusal. Three attempts, three refusals — the three-strike
rule fired, so I wrote it down (**CF-048**) and worked around it the way a determined user
would: name the container port `http` and target it by name. That is a legal and arguably
better manifest, but it is not the one I asked for, and it is not a workaround a user who has
not read the OpenAPI spec would find quickly.

With the named-port workaround `cf gen --validate` said `render validation ok`, and I moved to
the real thing. My first `crossplane render` used a *minimal* XR carrying only the three
required parameters — which is exactly what the XRD invites, since everything else has a
default. It died:

```
executing "manifests" at <$spec.enableCache>: map has no entry for key "enableCache"
```

That surprised me for about thirty seconds until I remembered that `crossplane render` reads
an XR from a file and therefore never applies XRD defaulting. In the cluster the key is always
present, so this is correct behaviour and not a defect — but it is worth noting that the
emitter guards `replicas`, `image` and `maxReplicas` (all defaulted) with `hasKey` and does
*not* guard `enableCache` and `shardCount` (also defaulted). Same class of parameter, two
different treatments. I did not file it: nothing is harmed in the cluster, and I could not
show a case where it produces wrong output.

Spelling out the defaults gave a green render at 21:29:41Z. I read the rendered YAML rather
than trusting the exit code, and the Deployment's container came out as
`- name: CACHE_ENDPOINT` with no `value` — the guarded status wire omitting cleanly on the
first pass, which is right. So I re-ran the render with `--observed-resources`, giving the
cache a `clusterAddress` of `"true"` (a deliberately awkward but entirely legal string). The
annotation came back as `platform.example.org/cache-endpoint: "true"`; the container env value
came back as `value: true`. Same source, same tool, two different YAML types. `crossplane
render` exited 0, `cf gen --validate` exits 0, and the API server rejects the Deployment
outright. That is **CF-046**, and I reduced it to a thirty-line blueprint before filing.

Then the cluster. `make cluster` was unnecessary — a shared kind cluster was already up from
another workspace — so I created my namespace, generated with
`--group-suffix=w089857.cf-test`, and applied. The XRD established, a CompositionRevision was
created (so the 63-character label cap that `scripts/cluster/workspace.sh` warns about was
respected), and I installed `provider-aws-elasticache` plus a dummy `ClusterProviderConfig`.
Worth recording: `cf gen` had *guessed* the ProviderConfig shape as
`aws.m.upbound.io/v1beta1` / `ClusterProviderConfig`, printed a paragraph saying in plain
words that it was a guess and how to replace it, and the guess turned out to be exactly right.

Creating the XR with only the three required parameters worked: the API server injected all
five defaults, the Composition rendered, and all seven objects appeared, HPA included, with
the Deployment running 2/2.

Finally the Round-Trip Rule. `kubectl get composition ... -o yaml` and `kubectl get xrd ...
-o yaml`, both into a directory, then `cf adopt roundtrip -o adopted.cf.yaml`. It exited 0 and
wrote an eleven-line loss report naming `creationTimestamp`, `uid`, `resourceVersion`,
`generation`, `last-applied-configuration` and `status`. The XRD regenerated **byte-identical**
apart from the `# Source:` comment line — which is a real achievement, because the adopted
blueprint stringifies `default: "true"` for a boolean and `default: "5"` for an integer and
the emitter still puts the right types back into the XRD schema.

The Composition did not. The ServiceAccount lost its `labels` block, silently, with no line
in the loss report. I reduced it to a 25-line blueprint that reproduces on `cf adopt` of cf's
*own* output with no server involved at all, then went back to the cluster and applied the
regenerated Composition: twenty seconds later the live `shop-sa` had lost its `app: shop`
label. Every command in that chain exited 0. That is **CF-045**.

Two smaller things fell out on the way. Adopting a Composition *without* its XRD derives the
plural as `ToLower(Kind) + "s"` — `MetaLoss` becomes `metalosss` — even though the
Composition's own name (`metalosses.platform.example.org`) carries the correct plural and cf
had already read it (**CF-047**). And `cf function add`, following the worked example in
`docs/cli.md` verbatim, writes the digest pin into `.cf.lock` and *then* exits 1
(**CF-049**).

---

## Findings

### CF-045 — P0 — `cf adopt` silently drops every `metadata.*` field a native resource declares under `fields:`, breaking the Round-Trip Rule and mutating live objects [V]

**User goal.** Label a composed ServiceAccount so a NetworkPolicy and a Service selector can
find it — `metadata.labels[app]` in the blueprint's `fields:` block, which the emitter has
supported since 2026-09-03 (archived item: "make `metadata.name`, `metadata.labels`,
`metadata.annotations` settable"). Then round-trip the Composition through the cluster, as
`AGENTS.md` §1 requires.

**Repro** (`docs/comp-runs/2026-09-04-.../repros/cf-001-adopt-metadata-loss/meta.cf.yaml`;
no cluster needed — it reproduces on cf's own output):

```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: meta-loss}
spec:
  xrd:
    group: platform.example.org
    version: v1alpha1
    kind: MetaLoss
    plural: metalosses
    scope: Namespaced
    parameters:
      appName: {type: string, required: true}
  resources:
    - name: sa
      kind: ServiceAccount
      provider: k8s
      fields:
        metadata.labels[app]: {from: params.appName}
        metadata.labels[tier]: {value: backend}
    - name: cm
      kind: ConfigMap
      provider: k8s
      fields:
        metadata.labels[app]: {from: params.appName}
        data[app]: {from: params.appName}
```

```sh
cf gen meta.cf.yaml -o out1
cf adopt out1/compositions/metalosses.platform.example.org.yaml -o adopted.cf.yaml
cf gen adopted.cf.yaml -o out2
diff -u out1/compositions/*.yaml out2/compositions/*.yaml
```

**Observed.** `cf adopt` exits 0 and prints `Adopted blueprint written to adopted.cf.yaml`
with **no loss-report line for labels**. The adopted blueprint contains:

```yaml
  resources:
  - fields: {}
    kind: ServiceAccount
    name: sa
    provider: k8s
  - fields:
      data[app]:
        from: params.appName
    kind: ConfigMap
    name: cm
    provider: k8s
```

and the regenerated Composition differs by:

```diff
             name: {{ $xr }}-sa
             annotations:
               {{ setResourceNameAnnotation "sa" }}
-            labels:
-              app: {{ $spec.appName | quote }}
-              tier: 'backend'
           ---
           apiVersion: v1
           kind: ConfigMap
@@
             annotations:
               {{ setResourceNameAnnotation "cm" }}
-            labels:
-              app: {{ $spec.appName | quote }}
```

The same loss is what the mission's canonical round trip produced —
`kubectl get composition -o yaml` + `kubectl get xrd -o yaml` → `cf adopt` → `cf gen` — see
[`roundtrip/composition.diff`](2026-09-04-cachedservice-namespaced-roundtrip/roundtrip/composition.diff).
It is the *only* difference besides the `# Source:` comment; the XRD round-trips clean.

**Live consequence, verified on the cluster.** Before applying the regenerated Composition:

```
$ kubectl get sa shop-sa -n cf-agent-... -o jsonpath='{.metadata.labels}'
{"app":"shop","crossplane.io/composite":"shop"}
```

after `kubectl apply -f out-adopted/compositions/` and 20 seconds:

```
{"app":"shop","crossplane.io/composite":"shop"}   →   {"crossplane.io/composite":"shop"}
```

**Mechanism** (located after the run). `internal/adopt/adopt.go:1361` reads
`m["metadata"]` and extracts **only** `metadata.annotations`; then the loop at
`internal/adopt/adopt.go:1429-1431` walks the composed document's top-level keys and
`continue`s unconditionally on `"metadata"`. Nothing under `metadata` other than annotations
is ever read, and `report.Record` is never called for what is skipped — which is why the loss
report is silent.

**Cost.** A round trip through the cluster strips labels from every composed native object.
GitOps pipelines that re-adopt (the documented recovery path, and what `cf package`'s
"lossless recovery" annotation implies) will delete selector labels from live ServiceAccounts,
ConfigMaps and Deployments with no diff, no warning, and exit 0 at every step.

**What a fix must achieve.** Every `metadata.*` path the emitter can write from `fields:` must
survive `cf gen` → `cf adopt` → `cf gen` byte-identically; anything the importer cannot
represent must appear as a named line in the loss report rather than vanishing.

---

### CF-046 — P0 — a status wire into `fields:` is interpolated unquoted, so a string status value that looks like a bool or number changes type and the composed resource is rejected [V]

**User goal.** Publish an upstream resource's status value into a composed object — the
`docs/dsl.md` §"Cross-Resource Status reference" feature — in both an annotation and a field.

**Repro** (`repros/cf-002-status-wire-quoting/`):

```yaml
    - name: cache
      kind: Cluster
      provider: ghcr.io/crossplane-contrib/provider-aws-elasticache:v2.7.0
      fields:
        region: {from: params.region}
        engine: {value: redis}
    - name: cm
      kind: ConfigMap
      provider: k8s
      annotations:
        platform.example.org/endpoint: {from: resources.cache.status.atProvider.clusterAddress}
      fields:
        data[endpoint]: {from: resources.cache.status.atProvider.clusterAddress}
```

with `observed/cache.yaml` carrying `status.atProvider.clusterAddress: "true"` (a string, as
the CRD declares):

```sh
cf gen wire.cf.yaml -o out --validate          # -> "render validation ok", exit 0
crossplane render xr.yaml out/compositions/statuswires.platform.example.org.yaml \
  out/functions.yaml --observed-resources=observed   # -> exit 0
```

**Observed.** One value, two types:

```yaml
apiVersion: v1
data:
  endpoint: true
kind: ConfigMap
metadata:
  annotations:
    crossplane.io/composition-resource-name: cm
    platform.example.org/endpoint: "true"
```

The API server refuses the field form:

```
$ kubectl apply --dry-run=server -f cm-rendered.yaml
Error from server (BadRequest): ConfigMap in version "v1" cannot be handled as a
ConfigMap: json: cannot unmarshal bool into Go struct field ConfigMap.data of type string
```

The same happens in the mission blueprint's Deployment
(`rendered/render-obs.yaml`): `value: true` under `env[0]`, rejected with
`cannot unmarshal bool into Go struct field EnvVar.spec.template.spec.containers.env.value
of type string`.

`cf gen --validate` cannot catch this, because it renders without observed resources — the
guard omits the value entirely and the type error never appears in the document it type-checks.

**This is not hypothetical.** The very CRD in this run declares
`status.atProvider.autoMinorVersionUpgrade` as `type: string`, and ElastiCache populates it
with the literal strings `"true"` / `"false"` (`cf fields Cluster --status` shows the type).
Version strings (`"1.10"`), zero-padded ids and `"yes"`/`"no"` values behave the same way.

**Mechanism** (located after the run). `internal/emit/composition.go:989-999` documents the
decision explicitly: *"planFields interpolates it bare (the field's type is schema-checked
here, and the observed value arrives from the provider's own controller conforming to that
same CRD) … while planAnnotations pipes it through quote"*. `statusGuard`
(`internal/emit/composition.go:1172`) returns a bare expression and `statusWire` checks only
that the **source** status leaf is *some* scalar
(`scalarStatusTypes[leaf.Type]`, `composition.go:1056`); neither the source leaf's declared
type nor the **target** field's declared type is consulted when deciding whether to quote. A
`string`→`string` wire is precisely the case the premise gets wrong: the value conforms to the
CRD, but a bare YAML interpolation re-parses it as a different type.

**Cost.** A composed resource that renders green under every cf gate and is then refused by
the API server — the "composed resource never appears" failure the authoring skill calls badly
under-reported. The Deployment case is worse than the ConfigMap case, because the value only
appears once the upstream resource has converged, i.e. minutes after the operator has walked
away from a green render.

**What a fix must achieve.** A status wire landing in a field whose CRD schema declares
`type: string` must reach the API server as a string for every legal scalar value the source
leaf can hold, and the emitted quoting must be decided from schema types rather than from an
assumption about the observed value.

---

### CF-047 — P1 — `cf adopt` of a Composition without its XRD invents the XRD plural by appending `s`, ignoring the plural in the Composition's own name [V]

**User goal.** The primary documented invocation, `cf adopt <composition.yaml>` — you have a
Composition and want a blueprint.

**Repro** (same `meta.cf.yaml` as CF-045; kind `MetaLoss`, plural `metalosses`):

```sh
cf gen meta.cf.yaml -o out1          # writes out1/compositions/metalosses.platform.example.org.yaml
cf adopt out1/compositions/metalosses.platform.example.org.yaml -o adopted.cf.yaml
cf gen adopted.cf.yaml -o out2
```

**Observed.**

```
wrote out2/compositions/metalosss.platform.example.org.yaml
wrote out2/xrds/metalosss.platform.example.org.yaml
```

```diff
 metadata:
-  name: metalosses.platform.example.org
+  name: metalosss.platform.example.org
```

Exit 0, no warning, no loss-report line.

**Mechanism** (located after the run). `internal/adopt/adopt.go:310-312`:
`bp.Spec.XRD.Plural = strings.ToLower(bp.Spec.XRD.Kind) + "s"`. The correct plural was already
in hand: `internal/adopt/adopt.go:258-260` copies the Composition's `metadata.name` into
`bp.Metadata.Name`, and by Crossplane convention that name is `<plural>.<group>` — the very
string `metalosses.platform.example.org`. It is read and not used.

**Cost.** Regeneration emits a differently-named XRD and Composition. Applying them creates a
*second* CRD alongside the original rather than updating it, and the original XRs stop being
governed by the regenerated Composition. Bites every kind whose plural is not `kind + "s"`:
anything ending in s, x, ch, sh, or y.

**What a fix must achieve.** When the XRD is absent, the plural must be derived from the
information actually present (the Composition name's `<plural>.<group>` form, cross-checked
against the group), and a plural that still has to be guessed must be reported, not silently
assumed.

---

### CF-048 — P2 — every `x-kubernetes-int-or-string` field is typed `string`, so `cf gen --validate` rejects the integer form the API server requires and there is no escape hatch [V]

**User goal.** `targetPort: 8080` on a composed Service — the single most common line in a
Kubernetes Service — and `maxUnavailable: 1` on a Deployment rolling update.

**Repro** (`repros/cf-004-intorstring/svc.cf.yaml`, `intstr.cf.yaml`):

```yaml
      fields:
        spec.ports[0].port: {value: 80}
        spec.ports[0].targetPort: {value: 8080}
```

```sh
cf gen svc.cf.yaml -o out1 --validate
```

**Observed** (reproduced on two consecutive runs):

```
cf: error: render validation failed:
           line 48: resource "service" (Service): field "spec.ports[0].targetPort": invalid type: expected string, got integer 8080
```

The `raw:` escape hatch does not escape:

```sh
$ sed 's/targetPort: {value: 8080}/targetPort: {raw: "8080"}/' svc.cf.yaml > svc-raw.cf.yaml
$ cf gen svc-raw.cf.yaml -o out3 --validate
cf: error: render validation failed:
           line 48: resource "service" (Service): field "spec.ports[0].targetPort": invalid type: expected string, got integer 8080
```

And the form cf demands is the one Kubernetes refuses:

```
$ kubectl apply --dry-run=server -f targetport-int.yaml     # targetPort: 8080
service/tp-int created (server dry run)

$ kubectl apply --dry-run=server -f targetport-str.yaml     # targetPort: "8080"
The Service "tp-str" is invalid: spec.ports[0].targetPort: Invalid value: "8080":
must contain at least one letter (a-z)
```

Not specific to Services:

```
$ cf gen intstr.cf.yaml -o out4 --validate
cf: error: render validation failed:
           line 51: resource "deployment" (Deployment): field "spec.strategy.rollingUpdate.maxUnavailable": invalid type: expected string, got integer 1
```

**Mechanism** (located after the run). `internal/schema/k8s/k8s.go:210-215` collapses
IntOrString and Quantity to `"type": "string"`, and the comment states the justification:
*"the one spelling that is always legal for both (`"8080"` and `"500m"` round-trip; the API
server coerces where it must). This is the single lossy step in the whole pipeline, and it is
deliberate and documented rather than an accident of decoding."* The premise is false for
`targetPort`: a numeric string there is validated as a **port name**, and the API server
rejects it. `internal/emit/render_validate.go:509` (`isIntOrString`) already knows how to
recognise these fields via `x-kubernetes-int-or-string` and `oneOf`, but by the time it runs
the `oneOf` has been flattened away, so the branch never fires for native kinds.

**Cost.** `cf gen --validate` — the documented CI gate — cannot pass a Service with a numeric
target port. The workaround (name the container port and target it by name) is legal but has
to be discovered from the OpenAPI spec, and it is not available at all where the target is not
a named port.

**What a fix must achieve.** A field the Kubernetes schema declares as int-or-string must
accept both an integer and a string from `value:` and `raw:`, and `--validate` must not refuse
a document the API server accepts. Note that `docs/dsl.md`'s error table does not mention this
class at all.

---

### CF-049 — P2 — `cf function add` writes the lockfile pin and *then* exits 1 for any function with no typed Input CRD, including the one cf's own default pipeline emits [V]

**User goal.** Pin the functions the generated `functions.yaml` references. `docs/cli.md:269`
gives the worked example verbatim.

**Repro:**

```sh
$ cf function add xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
cf: error: package "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1" contains 0 function input schemas
$ echo $?
1
```

Independently re-verified against the ref cf actually emits, from a `.cf.lock` with its
`functions` key removed:

```sh
$ cf function add xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0
cf: error: package "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0" contains 0 function input schemas
$ echo $?
1
$ cat .cf.lock
{
 "providers": [ ... ],
 "functions": [
  {
   "ref": "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
   "digest": "sha256:762d6ac75774fcfb4899e76e45ca05dd5c121aecb4e93682de83f2bb62916171"
  }
 ]
}
```

Control — a function that *does* carry an Input CRD succeeds:

```sh
$ cf function add xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0
added xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0
  digest sha256:ad8a2ad4433c96768d81bda45b69386733c4403cc44222bcf3299619aa326f11
  1 function input schema of 1 CRDs
```

**Mechanism** (located after the run). `cmd/cf/function.go:30` calls
`store.FetchAndSave(ctx, c.Lock, c.Ref, c.fetch)`, which writes the lockfile entry; the
`inputs == 0` check that returns the error is at `cmd/cf/function.go:45-50`, after the write.
`function-auto-ready` legitimately has no Input CRD — and `cf gen` emits it into every default
pipeline.

**Cost.** The single function cf always emits cannot be pinned without a command that exits
non-zero; a `set -e` CI step that pins the pipeline dies on a step that in fact succeeded, and
a user who trusts the exit code will believe nothing was written and retry or hand-edit
`.cf.lock`. The documented example in `docs/cli.md` fails on copy-paste.

**What a fix must achieve.** A run that successfully resolves and pins a digest must exit 0.
Whether a function carries a typed Input is information, not failure; if the distinction
matters it belongs in the success message.

---

### CF-050 — P3 — the auto-ready package documented in `docs/dsl.md` and `docs/cli.md` is neither the registry nor the version `cf gen` emits [V]

**Repro:**

```sh
$ grep -n "function-auto-ready:v0" docs/dsl.md docs/cli.md
docs/cli.md:269:cf function add xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
docs/dsl.md:49:      package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
docs/dsl.md:352:3. `function-auto-ready` (`xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1`).
docs/dsl.md:361:      package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
docs/dsl.md:378:- **`package`**: OCI package reference (required, e.g. `xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1`).

$ grep -A2 "name: function-auto-ready" out/functions.yaml
  name: function-auto-ready
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0
```

`docs/dsl.md:352` is the load-bearing one: it documents what the generator *auto-injects*.
Both the registry host (`xpkg.crossplane.io` vs `xpkg.upbound.io`) and the version
(`v0.5.1` vs `v0.5.0`) are wrong. A user reconciling their cluster's installed Functions
against the docs installs a package `cf gen` will never reference, and the resulting
`functionRef` resolves to nothing.

**What a fix must achieve.** The documented default pipeline reference must match what
`internal/emit` emits, and stay matched — ideally checked, since the two have already drifted.

---

### CF-051 — P3 — `docs/cli.md` says `rbac.yaml` is emitted "when native Kubernetes kinds are composed"; it is emitted only for kinds outside Crossplane's pre-granted set [V]

**Repro:**

```sh
$ cf gen meta.cf.yaml -o out1        # composes ServiceAccount + ConfigMap, both pre-granted
wrote out1/compositions/metalosses.platform.example.org.yaml
wrote out1/functions.yaml
wrote out1/xrds/metalosses.platform.example.org.yaml
$ ls out1
compositions  functions.yaml  xrds
```

vs. the mission blueprint, which adds a HorizontalPodAutoscaler:

```sh
$ ls out
compositions  functions.yaml  providerconfigs  rbac.yaml  xrds
```

`docs/cli.md` states, twice, `rbac.yaml (emitted when native Kubernetes kinds are composed)`
and *"When native Kubernetes kinds like Deployment or Service are composed, `rbac.yaml`
contains the aggregated ClusterRole required by Crossplane to manage them"*. Deployment and
Service are exactly the two kinds for which it is **not** emitted, because they are already
granted. The generator's behaviour is right; the doc names the wrong trigger and picks the two
worst possible examples. The `cf gen` warning line has the same imprecision
(`composed native Kubernetes kinds require cluster RBAC permissions`).

**What a fix must achieve.** The doc must say `rbac.yaml` covers composed native kinds that
Crossplane does not already hold rights on, and not use Deployment or Service as the example.

---

## What worked

Specifically, and these are not throwaway compliments — each is something I checked against
the authoring skill or the cluster and found correct:

- **`rbac.yaml` gets the hardest trap in the skill exactly right.** One rule, for the one
  non-pre-granted kind (`autoscaling/horizontalpodautoscalers`), all seven verbs in the order
  the preflight authorizer loops over, no `/status` rule, label
  `rbac.crossplane.io/aggregate-to-crossplane: "true"` correctly quoted, and no noise rules for
  the pre-granted kinds. Proven end-to-end: `horizontalpodautoscaler.autoscaling/shop-hpa`
  was created by Crossplane in the live cluster.
- **`options: ["missingkey=error"]` is a sibling of `inline`**, not nested — the fatal form
  the upstream function README shows.
- **The optional-parameter guard is `hasKey`, never `with`.** `cacheNodeType` (optional, no
  default) came out as `{{- if hasKey $spec "cacheNodeType" }}`.
- **`when:` on an unguardable parameter is refused, loudly, with the best error message in the
  product.** `when: params.enableExtra` on an optional boolean with no default:
  `when parameter "enableExtra" must be required or carry a default -- the condition
  dereferences it unguarded, and under options: ["missingkey=error"] an absent key hard-fails
  the whole render; only the XRD's required gate or its schema default makes the key's
  presence unconditional`. That sentence teaches the trap rather than just reporting it.
- **The XRD round-trips byte-identically through the API server.** `apiextensions.crossplane.io/v2`,
  explicit `scope: Namespaced`, no `claimNames`, defaults preserved — and it survives
  `kubectl get -o yaml` → `cf adopt` → `cf gen` with zero diff outside the `# Source:` comment,
  even though the adopted blueprint carries `default: "true"` and `default: "5"` as strings.
  The `# required lists only the parameters the blueprint marks Required` footer explains the
  design decision to the reader at the exact point they would question it.
- **The `--validate` type check found my genuine bug** (`data[shard]: {raw: '{{ $i }}'}`
  rendering an integer into a `map[string]string`) with a line number and the value, before
  Kubernetes did.
- **`providerconfigs/aws.yaml` labels its own guess honestly** — an `# ASSUMPTION:` paragraph
  saying it did not find a real CRD, what shape it assumed and why, and the exact command to
  replace the guess — and the guess (`aws.m.upbound.io/v1beta1`, `ClusterProviderConfig`) was
  verified correct against the installed provider's CRDs.
- **`cf catalogue`, `cf kinds`, `cf fields` are fast and answer the question asked.**
  `cf catalogue redis` → two rows in under a second; `cf provider add` on a 16-CRD package →
  1.7 s with a digest pinned to `.cf.lock`. Nothing in the discovery loop cost me a detour.
- **`cf gen --check` is honest**: `in sync` on the tree it generated, exit 0.
- **Workspace isolation worked as documented.** `--group-suffix=w089857.cf-test` kept the
  cluster-scoped XRD clear of the other workspace's, the CompositionRevision was created
  (so the 63-character label cap `scripts/cluster/workspace.sh` warns about was respected),
  and the whole run coexisted with another session's cluster without a collision.

---

## Could not reproduce / deliberately not filed

- **`crossplane render` on a hand-written minimal XR fails on defaulted parameters used in
  `when:`/`forEach:`** (`map has no entry for key "enableCache"`). Correct behaviour — the
  render CLI does not apply XRD defaulting, and in-cluster the keys are always present. The
  emitter is internally inconsistent here (it *does* `hasKey`-guard defaulted parameters used
  in `fields:`) but I could not produce a case where that inconsistency yields wrong output,
  so it is not a finding.
- **`cf gen --check` reporting drift on `<name>.w089857.cf-test.w089857.cf-test.yaml`** was my
  own error — passing `--group-suffix` again to an already-suffixed adopted blueprint. Commit
  `0d3e914` shows this is known and handled in the Lane C script. Not a finding.
- **Guarded status wires rendering an env entry with a `name` and no `value`** on the first
  reconcile pass. Legal, applies fine, and the value appears once the upstream converges.
  Not a defect.
- **`cf adopt` of a bare Composition marking a required parameter `required: false`** and
  injecting a `providerName` parameter the original blueprint did not have. Without the XRD
  the importer has no source for required-ness, so this is unavoidable rather than wrong; it
  did not occur in the canonical round trip, where the XRD was present.
