Cluster untouched (2 XQueues, 2 Queues, unchanged). Here is the brief.

---

# Offline testing loop for generated Crossplane artifacts — Crossplane CLI v2.5.0

**Everything below marked ✅ was executed on this machine** against CLI v2.5.0 / server v2.4.0 / kind-platform / Docker 29.7.2. Items marked 📖 are read-only from docs/help text. No cluster mutation was performed (only `kubectl get` and `--dry-run`).

## Decisions this enables

1. **The test loop is `xpkg get-crds` → `resource validate` → `composition render` → `resource validate` → golden-diff.** ✅ Ran end-to-end on the user's real XQueue in **1.38s warm**. Not `crossplane validate` — that command does not exist in v2.5.0 (exit 80).
2. **`crossplane render` is byte-for-byte deterministic** — including generated names (`demo-queue-2d702055d0fb`) and owner-reference UUIDs (`ac342a8b-…`). ✅ 3 runs, identical MD5. **Golden-file testing is a first-class strategy**, not a hope.
3. **Split the CI job in two: `render` needs Docker, `validate` does not.** ✅ With `DOCKER_HOST=tcp://127.0.0.1:1`, validate exits 0; render dies creating a Docker *network*. Ship a `validate`-only lane for Docker-less/air-gapped CI.
4. **`--xrd` on render does *defaulting*, not validation.** ✅ An XR with `location: ASIA` (enum violation), missing `required` field, and an unknown field rendered happily with exit 0 under `--xrd`. **The generator must emit a separate `resource validate` gate** — do not let users believe `--xrd` checks anything.
5. **Two silent-failure traps must be designed around**: missing schemas pass with **exit 0** unless `--error-on-missing-schemas` is passed ✅, and Go-template misses emit the literal string `<no value>` which is **schema-valid and passes the whole pipeline with exit 0** ✅. Ship `--error-on-missing-schemas` always, plus a `grep '<no value>'` guard.

---

## 1. The real v2.5.0 command tree

`crossplane --help` ✅. Note the banner: *"Beta features are enabled"* — beta is **on by default**, user config is just `version: 1`.

```
cluster       [BETA]   cluster top
composition            composition convert | generate | render
config                 config set | view
dependency    [BETA]   dependency add | update-cache | clean-cache
function      [BETA]   function generate
project       [BETA]   project init | build | push | run | stop
resource      [BETA]   resource trace | validate
version
xpkg                   xpkg batch|build|get-crds|init|install|push|update|extract
xrd           [BETA]   xrd convert | generate
completions
```

### Migration table — what your muscle memory maps to

| Pre-2.x | v2.5.0 | Verified |
|---|---|---|
| `crossplane beta render` | `crossplane composition render` | ✅ |
| `crossplane render` | **still works — hidden top-level alias** | ✅ exit 0 with real args |
| `crossplane beta validate` | `crossplane resource validate` | ✅ |
| `crossplane validate` | **does not exist** → `error: unexpected argument validate`, **exit 80** | ✅ |
| `crossplane beta trace` | `crossplane resource trace` | ✅ |
| `crossplane beta <anything>` | **gone** → `error: unexpected argument beta`, exit 80 | ✅ |

`crossplane render` and `crossplane composition render` produce identical help except the usage/summary lines ✅. The alias is real and the CLI's own help text uses it (`crossplane render <(crossplane xr generate claim.yaml) composition.yaml functions.yaml`). **Recommendation: generate `crossplane composition render` in shipped Makefiles** — it is the documented form; treat the short alias as convenience only.

### Alpha-gated commands (hidden by default)

With a throwaway `--config` containing `features.enableAlpha: true` ✅ (user's own config left untouched):

```
operation  [ALPHA]  operation render
xr         [ALPHA]  xr generate | xr patch
```

`crossplane xr generate` converts a **Claim → XR** — irrelevant for v2 namespaced/no-claim XRDs like the user's, but relevant if the tool ever emits v1 claim-based APIs.

---

## 2. `crossplane composition render`

```
Usage: crossplane composition render <composite-resource> <composition> [<functions>] [flags]
```

Positional args ✅:
- `<composite-resource>` — YAML file, the XR. **Required.**
- `<composition>` — YAML file. **Must be `mode: Pipeline`.**
- `[<functions>]` — YAML file *or directory*. Optional **only** inside a Crossplane Project (dir with `crossplane-project.yaml`).

### Complete flag list (verbatim from `--help`)

| Flag | Meaning |
|---|---|
| `--crossplane-version=VERSION` | Version of the Crossplane image used for rendering. Defaults to latest stable. |
| `--crossplane-image=IMAGE` | Override the full Crossplane Docker image reference. |
| `--crossplane-binary=PATH` | Local crossplane binary instead of Docker. **See trap below.** |
| `--crossplane-docker-network=STRING` | Docker network to start the crossplane container in. |
| `--context-files=KEY=VALUE;...` | Context pairs; values are *files* containing JSON/YAML. |
| `--context-values=KEY=VALUE;...` | Context pairs; values are literal JSON/YAML. Takes precedence over `--context-files`. |
| `-r, --include-function-results` | Emit function messages as `kind: Result`. |
| `-x, --include-full-xr` | Include the input XR's full spec+metadata in output. |
| `-o, --observed-resources=PATH` | File/dir of mocked observed composed resources (simulate update). |
| `--extra-resources=PATH` | **Deprecated** — use `--required-resources`. |
| `-e, --required-resources=PATH` | File/dir of required resources. Repeatable. |
| `-s, --required-schemas=DIR` | Dir of JSON OpenAPI v3 schemas from `kubectl get --raw /openapi/v3/<group-version>`. |
| `-c, --include-context` | Emit pipeline context as `kind: Context`. |
| `--function-credentials=PATH` | File/dir of credentials for functions. |
| `-a, --function-annotations=KEY=VALUE,...` | Override function annotations for **all** functions. Repeatable. |
| `--cache-dir=STRING` | Cached xpkg contents (`$CROSSPLANE_XPKG_CACHE`). |
| `--max-concurrency=8` | Concurrency for building embedded functions. |
| `-f, --project-file="crossplane-project.yaml"` | Path to project file. |
| `--timeout=1m` | **Default 1 minute** — too short for a cold multi-function pull on slow links. |
| `--xrd=PATH` | XRD defining the XR's schema. **Applies defaults; does NOT validate.** |

### How functions actually run — Docker is mandatory ✅

This is **the biggest change from v1.x**: it is no longer just the functions that run in Docker — **the render engine itself runs as a container**. Sampling `docker ps` during a render ✅:

```
distracted_nightingale | xpkg.crossplane.io/crossplane/crossplane:stable          | 8080/tcp
angry_lehmann          | .../function-go-templating:v0.12.0                       | 9443/tcp
lucid_haslett          | .../function-auto-ready:v0.5.0                           | 9443/tcp
jovial_villani         | .../function-go-templating:v0.12.0                       | 9443/tcp
```

One engine container + one container per function (go-templating appeared twice — engine-side and pipeline-side). Containers are torn down after the run. A Docker **network** is created per render:

```
crossplane: error: cannot create Docker network for rendering: cannot create Docker
network "crossplane-render-z49h7jhl": Cannot connect to the Docker daemon at
tcp://127.0.0.1:1. Is the docker daemon running?
```
✅ (exit 1, with `DOCKER_HOST` deliberately broken)

**Implication for CI:** `crossplane composition render` needs a real Docker daemon *and* network-create privileges. That rules it out of many restricted runners and out of rootless/DinD setups without `--privileged`. Plan the fallback lane accordingly (see §7).

#### `--crossplane-binary` trap ✅

The help says *"Use a local crossplane binary instead of Docker: `--crossplane-binary=/usr/local/bin/crossplane`"*. Passing the **CLI** binary fails:

```
crossplane: error: cannot render composite resource: crossplane internal render returned
error with output: crossplane: error: unexpected argument internal : exit status 80
```

The flag wants the **Crossplane core (server) binary**, which has a hidden `internal render` subcommand — a different artifact from the CLI. Inside `xpkg.crossplane.io/crossplane/crossplane:stable` it lives at `/bin/crossplane` → `/nix/store/…-crossplane-linux-arm64-v2.4.0/bin/crossplane` ✅ (a **linux** binary; unusable on this darwin host). To use this path on macOS you'd need a darwin build of `github.com/crossplane/crossplane/cmd/crossplane` — **not confirmed to work; I could not test it.** ⚠️ Even then, *functions* still need Docker unless annotated `render.crossplane.io/runtime: Development`.

#### Function runtime annotations 📖 (from help; the pull-policy one ✅ tested)

| Annotation | Purpose |
|---|---|
| `render.crossplane.io/runtime: "Development"` | Connect to a function on `localhost:9443` running with `--insecure` instead of Docker. |
| `render.crossplane.io/runtime-development-target: "dns:///example.org:7443"` | Non-default gRPC target. |
| `render.crossplane.io/runtime-docker-cleanup: "Orphan"` | Don't stop the container after rendering. |
| `render.crossplane.io/runtime-docker-name: "<name>"` | Create/reuse a named container. |
| `render.crossplane.io/runtime-docker-pull-policy: "Always"` | Also `Never`, `IfNotPresent`. ✅ |
| `render.crossplane.io/runtime-docker-publish-address: "0.0.0.0"` | Default `127.0.0.1`. |
| `render.crossplane.io/runtime-docker-target: "docker-host"` | Address the CLI dials. |

Override globally with `-a`, e.g. ✅ `-a render.crossplane.io/runtime-docker-pull-policy=Always`.

### Measured timings ✅

| Scenario | Wall time |
|---|---|
| Warm (all images cached) | **1.05 – 1.46 s** |
| Forced manifest re-check (`-a …pull-policy=Always`, layers cached) | **3.55 s** |
| Cold engine image pull (`--crossplane-version=v2.3.0`, ~106 MB) | **7.35 s** |

Fully cold (engine 106 MB + go-templating 80.1 MB + auto-ready 78.9 MB ≈ 265 MB) was not measured — the host already had all three cached. ⚠️ Extrapolating, a first CI run on a cold runner plausibly exceeds the **`--timeout=1m` default**. **Generate `--timeout=5m` into shipped Makefiles.**

### Verified end-to-end run on the user's own XQueue ✅

Reconstructed from the live cluster into `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/xqueue/` (`xr.yaml`, `composition.yaml`, `functions.yaml`, `xrd.yaml`).

```
crossplane composition render xr.yaml composition.yaml functions.yaml
```

produced (abridged):

```yaml
apiVersion: platform.hooli.tech/v1alpha1
kind: XQueue
metadata: {name: demo-queue, namespace: team-a}
spec:
  crossplane:
    resourceRefs:
    - {apiVersion: sqs.aws.m.upbound.io/v1beta1, kind: Queue, name: demo-queue-2d702055d0fb}
status:
  conditions:
  - {reason: WatchCircuitClosed, status: "True", type: Responsive}
  - {reason: ReconcileSuccess,   status: "True", type: Synced}
  - {message: 'Unready resources: main-queue', reason: Creating, status: "False", type: Ready}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations: {crossplane.io/composition-resource-name: main-queue}
  generateName: demo-queue-
  labels: {crossplane.io/composite: demo-queue}
  name: demo-queue-2d702055d0fb
  namespace: team-a
  ownerReferences:
  - {apiVersion: platform.hooli.tech/v1alpha1, blockOwnerDeletion: true, controller: true,
     kind: XQueue, name: demo-queue, uid: ac342a8b-3073-5ef8-90e7-894635caa1f2}
spec:
  forProvider: {maxMessageSize: 2048, region: eu-north-1,
                tags: {env: dev, owner: platform}, visibilityTimeoutSeconds: 45}
  providerConfigRef: {kind: ClusterProviderConfig, name: localstack}
```

Notes worth designing around:
- The XR's `namespace: team-a` **propagates to the composed MR** (v2 namespaced XR semantics).
- Default output shows **only** status + `metadata.name` + injected `spec.crossplane.resourceRefs` — the XR's own spec fields appear only with `-x`.
- `status.conditions[].lastTransitionTime` is **frozen at `"2024-01-01T00:00:00Z"`** — this is what makes goldens stable.

### Determinism ✅

Three consecutive runs → identical MD5 `3eb474d6df52dd0e6b8a6d53536bb732`. Generated name and owner-ref UID are content-derived hashes, not random. **Golden files are safe.**

### Template errors are precise ✅

Rendering an XR with `spec: {}`:
```
crossplane: error: cannot render composite resource: crossplane internal render in Docker:
pipeline returned fatal: ... pipeline step "render-queue" returned a fatal result:
cannot execute template: template: manifests:9:15: executing "manifests" at
<index (dict "EU" "eu-north-1" "US" "us-east-2") $spec.location>: error calling index:
value is nil; should be of type string
```
**Exit 1**, with `manifests:<line>:<col>`. ⚠️ Line numbers are relative to the *rendered template body*, not the Composition file — if the generator emits the template inline, it should keep an offset map to translate `manifests:9` back to a source line.

### `--xrd` does defaulting, not validation ✅

Proof: an XRD with `location.default: US` and `providerName.default: defaulted-pc`, rendered against `spec: {}`:

```yaml
spec:
  forProvider: {region: us-east-2}          # ← from default location: US
  providerConfigRef: {kind: ClusterProviderConfig, name: defaulted-pc}
```

Counter-proof: XR with `location: ASIA`, `maxMessageSize: 10` (below `minimum: 1024`), no `providerName`, plus unknown `bogusField` → **byte-identical output with and without `--xrd`, exit 0**. `--xrd` caught nothing.

### `--observed-resources` simulates updates ✅

Feeding an observed Queue with `status.conditions[Ready]=True` flipped the XR's Ready condition from `Creating/False` to `Available/True` — auto-ready read the observed state. This is how a generated test asserts the readiness-propagation path.

### `--required-schemas` sourcing ✅

Format is a **directory of JSON** files from the cluster's OpenAPI v3 endpoint:
```
kubectl get --raw '/openapi/v3' | jq -r '.paths | keys[]'
  → apis/platform.hooli.tech/v1alpha1
    apis/sqs.aws.m.upbound.io/v1beta1
    apis/sqs.aws.upbound.io/v1beta1
kubectl get --raw '/openapi/v3/apis/sqs.aws.m.upbound.io/v1beta1' > schemas/sqs_v1beta1.json   # 283 KB
```
⚠️ This requires a live cluster, so it is **not** the right schema source for an offline generator — use `xpkg get-crds` (§4) instead. `--required-schemas` is only for functions that request schemas at runtime; function-go-templating v0.12.0 did not need it here.

---

## 3. `crossplane resource validate` — the real validation gate

```
Usage: crossplane resource validate <extensions> <resources> [flags]
```

Both args accept **comma-separated lists of files, directories, or `-` (stdin)** ✅.

| Flag | Meaning |
|---|---|
| `--cache-dir="~/.crossplane/cache"` | Absolute path for downloaded schemas. |
| `--clean-cache` | Wipe cache before downloading. |
| `--crossplane-image="xpkg.crossplane.io/crossplane/crossplane:stable"` | Source of **built-in** schemas (Composition/XRD/etc). |
| `--error-on-missing-schemas` | **Non-zero exit when a schema is missing. Not the default.** |
| `-o, --output=text` | `text`, `json`, or `yaml`. |
| `--skip-success-results` | Print only problems. |
| `--update-cache` | Re-resolve semver constraints (adds network calls). |

Help text states: *"All validation happens offline using the Kubernetes API server's validation library, without requiring a Crossplane instance or control plane."* — ✅ confirmed, including **no Docker** (works with `DOCKER_HOST=tcp://127.0.0.1:1`).

### What it actually catches ✅

**Against provider CRDs** (extensions = a `Provider` manifest, or a dir of raw CRDs):
```
[x] schema validation error sqs.aws.m.upbound.io/v1beta1, Kind=Queue, demo-queue-2d702055d0fb :
    spec.forProvider.maxMessageSize: Invalid value: "string": ... must be of type number: "string"
[x] schema validation error ... spec.forProvider.totallyBogusField: Invalid value:
    "totallyBogusField": unknown field: "totallyBogusField"
Total 2 resources: 0 missing schemas, 1 success cases, 1 failure cases
crossplane: error: could not validate all resources
```
**Exit 1.** Type errors *and* unknown fields. Reports **all** errors, not just the first.

**Against the XRD** (`crossplane resource validate xrd.yaml xr.yaml`, no render, no cluster):
```
[x] spec.location: Unsupported value: "ASIA": supported values: "EU", "US"
[x] spec.maxMessageSize: Invalid value: 10: ... should be greater than or equal to 1024
[x] spec.providerName: Required value
[x] spec.bogusField: Invalid value: "bogusField": unknown field: "bogusField"
```
Enums, numeric bounds, required, unknown fields. **Exit 1.**

**CEL `x-kubernetes-validations`** ✅ — added a rule to the XRD and it fired:
```
[x] CEL validation error platform.hooli.tech/v1alpha1, Kind=XQueue, demo-queue :
    spec: Invalid value: EU queues must use an eu- prefixed providerConfig
```

**Function input schemas** ✅ — this is the standout capability. Validate descends into `spec.pipeline[].input` and checks it against the **function package's own input CRD**:
```
crossplane resource validate extensions/ composition.yaml --error-on-missing-schemas
schemas does not exist, downloading:  xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0
[✓] gotemplating.fn.crossplane.io/v1beta1, Kind=GoTemplate,  validated successfully
[✓] apiextensions.crossplane.io/v1, Kind=Composition, xqueues.aws.platform.hooli.tech validated successfully
```
Injecting `notARealField` into the GoTemplate input →
```
[x] schema validation error gotemplating.fn.crossplane.io/v1beta1, Kind=GoTemplate,  :
    notARealField: Invalid value: "notARealField": unknown field: "notARealField"
```
⚠️ Note: changing `source: Inline` → `Inlin3` was **not** caught — the GoTemplate CRD apparently has no enum on `source`. Don't assume input validation is exhaustive.

### JSON output ✅ — ideal for a generator's own test harness

```
crossplane resource validate xrd.yaml xr-bad.yaml -o json
```
```json
{
  "summary": {"total": 1, "valid": 0, "invalid": 1, "missingSchemas": 0},
  "resources": [{
    "apiVersion": "platform.hooli.tech/v1alpha1", "kind": "XQueue",
    "name": "bad-queue", "namespace": "team-a", "status": "invalid",
    "errors": [
      {"type": "schema", "field": "spec.location",
       "message": "spec.location: Unsupported value: \"ASIA\": supported values: \"EU\", \"US\"",
       "value": "ASIA"},
      {"type": "unknownField", "field": "spec.bogusField",
       "message": "spec.bogusField: Invalid value: \"bogusField\": unknown field: \"bogusField\"",
       "value": "bogusField"}
    ]
  }]
}
```
Error `type` values observed: `schema`, `unknownField`, and CEL failures (rendered as `CEL validation error` in text mode).

### ⚠️ Trap: missing schemas are silent by default

```
[!] could not find CRD/XRD for: sqs.aws.m.upbound.io/v1beta1, Kind=Queue
Total 2 resources: 1 missing schemas, 1 success cases, 0 failure cases
EXIT=0                      ← !!
```
With `--error-on-missing-schemas`: `crossplane: error: could not validate all resources, schema(s) missing`, **exit 1** ✅. **Always generate this flag** — otherwise a typo'd `apiVersion` in the generated Composition silently "passes".

### ⚠️ Trap: `-r` and `-c` break `--error-on-missing-schemas`

`--include-function-results` emits `render.crossplane.io/v1beta1 Kind=Result` and `--include-context` emits `Kind=Context`. Neither has a CRD, so both trip the flag ✅:

| Render flags | Result |
|---|---|
| `-x` | exit **0** |
| `-x -r` | `[!] could not find CRD/XRD for: render.crossplane.io/v1beta1, Kind=Result` ×2 → exit **1** |
| `-x -c` | `[!] … Kind=Context` → exit **1** |

**Use `-x` only in the validated pipe.** Keep `-r -c` in a separate human-facing `make explain` target.

### The cache ✅

`~/.crossplane/cache`, **1.2 MB total** — it stores extracted `package.yaml` per package, not the images. The 1.21 GB `provider-aws-sqs` image is *not* pulled.
```
~/.crossplane/cache/
├── xpkg.crossplane.io/crossplane/crossplane@stable/package.yaml
└── xpkg.upbound.io/
    ├── crossplane-contrib/function-{auto-ready@v0.5.0,go-templating@v0.12.0}/package.yaml
    └── upbound/provider-aws-sqs@v2/package.yaml
```
Cache this directory in CI — it's tiny and eliminates all registry traffic.

---

## 4. Validating an XRD itself, offline ✅

Pass an **empty directory** as extensions; validate falls back to Crossplane's built-in schemas from `--crossplane-image`:

```
mkdir -p empty-ext
crossplane resource validate empty-ext/ xrd.yaml
[✓] apiextensions.crossplane.io/v2, Kind=CompositeResourceDefinition, xqueues.platform.hooli.tech validated successfully
EXIT=0
```

Same works for Compositions ✅. Combined ✅:
```
crossplane resource validate empty-ext/ xrd.yaml,composition.yaml --error-on-missing-schemas
[✓] apiextensions.crossplane.io/v2, Kind=CompositeResourceDefinition, ... validated successfully
[!] could not find CRD/XRD for: gotemplating.fn.crossplane.io/v1beta1, Kind=GoTemplate
[✓] apiextensions.crossplane.io/v1, Kind=Composition, ... validated successfully
EXIT=1        ← only because the function input schema was absent
```
Add the `Function` manifests to extensions and it goes green ✅.

⚠️ **Limit:** this checks the XRD against the *CompositeResourceDefinition CRD* — field legality, types, required keys. I did **not** confirm it enforces full **structural-schema** legality (e.g. `x-kubernetes-preserve-unknown-fields` placement rules, or a `type`-less property node) the way the API server's CRD structural checks do. For that guarantee, `kubectl apply --dry-run=server` on the XRD is the authoritative check. **Unverified — flag as a gap.**

---

## 5. Fallbacks: `kubectl --dry-run` and `resource trace`

### `kubectl apply --dry-run=server` ✅

```
kubectl apply --dry-run=server -f rendered.yaml
xqueue.platform.hooli.tech/demo-queue created (server dry run)
queue.sqs.aws.m.upbound.io/demo-queue-2d702055d0fb created (server dry run)
EXIT=0
```
On the bad render:
```
Error from server (BadRequest): error when creating "rendered-bad.yaml": Queue in version
"v1beta1" cannot be handled as a Queue: strict decoding error: unknown field
"spec.forProvider.totallyBogusField"
EXIT=1
```

**Comparison vs `resource validate`:**

| | `resource validate` | `kubectl --dry-run=server` |
|---|---|---|
| Cluster needed | No ✅ | **Yes** |
| Docker needed | No ✅ | No |
| Namespace must exist | No | **Yes** (`team-a` existed here) |
| Errors reported | **All** (both type + unknown field) ✅ | **First only** — reported `totallyBogusField`, missed the `maxMessageSize` type error ✅ |
| CEL rules | Yes ✅ | Yes (server-side) |
| Machine-readable output | `-o json` ✅ | No |

`--dry-run=client` also exits 0 ✅ but only parses YAML — near-worthless as a gate.

**Verdict: `resource validate` is strictly better for CI.** Use server dry-run only as an optional pre-merge "against the real target cluster" job, and for the structural-schema gap in §4.

### `crossplane resource trace` ✅ (post-apply only, needs a cluster)

```
crossplane resource trace xqueue cncf-pre-talk -n team-a
NAME                                           SYNCED   READY   STATUS
XQueue/cncf-pre-talk (team-a)                  True     True    Available
└─ Queue/cncf-pre-talk-e28dacd7ec77 (team-a)   True     True    Available
```
Flags: `-o default|wide|json|dot|yaml`, `-n/--namespace`, `-c/--context`, `-w/--watch`, `-s/--show-connection-secrets` (names only, never values), `--show-package-dependencies=unique|all|none`, `--show-package-revisions=active|all|none`, `--concurrency=5`, `--as/--as-group/--as-uid`. Not part of the offline loop — put it in `make debug`.

---

## 6. `crossplane xpkg get-crds` — the schema source your generator wants ✅

Not in your brief, but it's the most important discovery for the *generator* half of the tool.

```
crossplane xpkg get-crds <extensions> --output-dir DIR [--flat] [--json-schema]
                         [--cache-dir] [--clean-cache] [--no-cache] [--update-cache]
                         [--crossplane-image]
```

Given only a `Provider` manifest (no cluster, no Docker), it wrote 8 CRDs in **0.32 s** (warm cache) ✅:
```
crds/sqs.aws.m.upbound.io/v1beta1/{queue,queuepolicy,queueredrivepolicy,queueredriveallowpolicy}.yaml
crds/sqs.aws.upbound.io/v1beta1/{...}.yaml
```
Note it emits **both** the v2 namespaced group (`sqs.aws.m.upbound.io`) and the legacy cluster-scoped group (`sqs.aws.upbound.io`) — your generator must pick deliberately. The user's Composition targets `sqs.aws.m.upbound.io/v1beta1`.

Pointed at the **Function** manifests it also vendors function input CRDs ✅ (`gotemplating.fn.crossplane.io/v1beta1/gotemplate.yaml`) — so one command vendors provider MR schemas *and* function input schemas: 9 CRDs total for this project.

`--json-schema` emits JSON Schema instead ✅, explicitly *"Useful for YAML language server integration"* — i.e. your generated repo can ship editor autocomplete for the MRs it templates, for free.

### Competitive note: the built-in generators are thin ✅

- `crossplane xrd generate <xr.yaml>` **requires a project file** with `apiVersion: dev.crossplane.io/v1alpha1` — it errors out otherwise even with `--path` (`error: failed to read project file`; and `unsupported project apiVersion "meta.dev.upbound.io/v1alpha1"`). With a valid project it wrote `apis/xqueues/definition.yaml`, but the schema is **type-inference only** — it produced `location: {type: string}`, dropping the `enum: [EU, US]`, and `maxMessageSize: {type: integer}`, dropping `minimum: 1024`. It also invents `tags` sub-properties from the example's literal keys (`env`, `owner`) instead of `additionalProperties`.
- `crossplane composition generate <xrd>` produces *"a single pipeline step that runs function-auto-ready"* 📖 — no provider resources at all.

**Neither reads provider MR schemas.** That is precisely the gap your tool fills, and `xpkg get-crds` is the supported, offline, provider-agnostic way to get those schemas.

---

## 7. Recommended `make test` for generated projects

Built and executed end-to-end ✅ at `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/genproj/Makefile`. **Full `make test` = 1.38 s warm; idempotent across runs.**

```makefile
CROSSPLANE ?= crossplane
APIS       := apis
XRD        := $(APIS)/definition.yaml
COMP       := $(APIS)/composition.yaml
FUNCS      := $(APIS)/functions.yaml
DEPS       := $(APIS)/providers.yaml,$(APIS)/functions.yaml
SCHEMAS    := schemas
VALIDATE   := $(CROSSPLANE) resource validate $(SCHEMAS)
VFLAGS     := --error-on-missing-schemas --skip-success-results
CASES      := $(wildcard tests/*/)

.PHONY: test schemas lint render golden explain guard clean

# 0. Vendor provider + function CRDs once. The ONLY step that touches the network.
schemas:
	$(CROSSPLANE) xpkg get-crds $(DEPS) --output-dir $(SCHEMAS)
	cp $(XRD) $(SCHEMAS)/_xrd.yaml

# 1. Are the generated XRD + Composition legal? Also checks the embedded
#    function input against function-go-templating's own GoTemplate CRD.
lint:
	$(VALIDATE) $(XRD),$(COMP) $(VFLAGS)

# 2. Per case: gate the input XR on the XRD, render, then schema-check every
#    rendered managed resource against the real provider CRDs.
#    No -r/-c here: they emit schema-less Result/Context docs that
#    --error-on-missing-schemas rejects.
render:
	@set -e; for c in $(CASES); do \
	  echo "==> $$c"; \
	  $(VALIDATE) $$c/xr.yaml $(VFLAGS); \
	  $(CROSSPLANE) composition render $$c/xr.yaml $(COMP) $(FUNCS) \
	    --xrd=$(XRD) --include-full-xr --timeout=5m > $$c/rendered.yaml; \
	  $(VALIDATE) - $(VFLAGS) < $$c/rendered.yaml; \
	done

# 3. Catches silent behaviour changes schemas can't see. render is byte-deterministic.
golden:
	@set -e; for c in $(CASES); do \
	  if [ -f $$c/golden.yaml ]; then diff -u $$c/golden.yaml $$c/rendered.yaml && echo "==> $$c golden OK"; \
	  else cp $$c/rendered.yaml $$c/golden.yaml; echo "==> $$c golden CREATED"; fi; \
	done

# 4. Go templates emit the literal string "<no value>" on a missed key.
#    It is schema-VALID and passes every gate above. This is the only catch.
guard:
	@! grep -rn '<no value>\|<nil>' tests/*/rendered.yaml || \
	  { echo "FAIL: unresolved template expression in rendered output"; exit 1; }

explain:   # human debugging: function messages + pipeline context
	@for c in $(CASES); do $(CROSSPLANE) composition render $$c/xr.yaml $(COMP) $(FUNCS) --xrd=$(XRD) -x -r -c; done

test: lint render guard golden

clean:
	rm -rf $(SCHEMAS) tests/*/rendered.yaml
```

### Regression coverage — each layer proven to fire ✅

| Injected defect | Caught by | Observed |
|---|---|---|
| `bogusMrField: oops` in the template | `render` | `spec.forProvider.bogusMrField: … unknown field`, make exit 2 |
| `notAField` in the GoTemplate input | `lint` | `Total 3 resources: … 1 failure cases`, make exit 2 |
| `eu-north-1` → `eu-west-1` in the region map | `golden` | `-    region: eu-north-1` / `+    region: eu-west-1`, make exit 2 |
| XR violating enum/min/required | `render` (XR gate) | 4 distinct errors, exit 1 |
| Missing required field → `<no value>` | **`guard` only** | see below |

### ⚠️ The `<no value>` hole — proven, and the reason `guard` exists ✅

With `providerName` made optional in the XRD, an XR omitting it renders:
```yaml
  providerConfigRef:
    kind: ClusterProviderConfig
    name: <no value>
```
and the **entire pipeline passes**:
```
[✓] platform.hooli.tech/v1alpha1, Kind=XQueue, nv2 validated successfully
[✓] sqs.aws.m.upbound.io/v1beta1, Kind=Queue, nv2-60a5aac40c8a validated successfully
Total 2 resources: 0 missing schemas, 2 success cases, 0 failure cases
EXIT=0
```
`<no value>` is a legal string, so no schema check can see it. Only `grep` catches it ✅. Related: `providerName: ""` renders as `name: null` and **is** caught (`must be of type string: "null"`) ✅ — so the empty-string case is safe but the missing-key case is not.

**Two mitigations your generator should apply:** (a) mark every field the template dereferences as `required` in the generated XRD — then the XR gate catches it upstream; (b) emit `{{ required "..." $spec.foo }}` or `default` in generated templates rather than bare `{{ $spec.foo }}`.

### Why `--include-full-xr` is mandatory in the pipe ✅

Without `-x` the XR's spec isn't in the stream, so XRD-level violations (enum, required, bounds) go unseen at the validate step. With `-x`, the same pipe catches both XR-level and MR-level errors. The explicit `$(VALIDATE) $$c/xr.yaml` line is belt-and-braces — it fails *before* burning a Docker render.

### CI lane split

- **Lane A (`lint` only)** — no Docker, no cluster, ~0.5 s. Runs anywhere. Catches malformed XRD/Composition/function-input.
- **Lane B (`test`)** — needs Docker with network-create. Full loop.
- **Lane C (optional, pre-merge)** — `kubectl apply --dry-run=server -f tests/*/rendered.yaml` against a real cluster, for the structural-schema gap in §4.

Cache `~/.crossplane/cache` (1.2 MB) and the Docker images (~265 MB) between runs.

---

## Flagged as unconfirmed

- Whether `--crossplane-binary` works at all with a **darwin build of the core Crossplane server binary** — the image only ships linux/arm64, and the CLI binary is rejected (`unexpected argument internal`). Unverified whether this path is viable on macOS.
- Whether `resource validate` enforces full **CRD structural-schema legality** on an XRD, or only field-level conformance to the CompositeResourceDefinition CRD. Only the latter was demonstrated.
- Fully-cold render timing (all three images absent). Measured only partial-cold (7.35 s for a 106 MB engine pull).
- Whether `render.crossplane.io/runtime: "Development"` genuinely removes the Docker requirement for the *engine* as well as functions — the engine container appeared in every run I sampled.

## Reference paths

- Reconstructed XQueue bundle: `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/xqueue/`
- Working generated-project skeleton + Makefile: `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/genproj/`
- Captured help text: `.../scratchpad/help-render.txt`, `.../scratchpad/help-validate-trace.txt`, `.../scratchpad/help-generate.txt`
- Docs: https://docs.crossplane.io/latest/cli/command-reference/