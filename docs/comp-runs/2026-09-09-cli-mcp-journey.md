# Mission J3 — CLI-only / MCP-only journey, cf v0.10.0

Binary: `/Users/kaurkallas/compositionfactory/bin/cf` (`cf --version` → `cf v0.10.0`).
Scratch: `$S` = `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/526c8b42-4bc5-470e-8d9d-150d5dea6a73/scratchpad/journeys/j3`.
All blueprints, outputs and the MCP driver scripts (`mcpdrv.py`, `mcpdrv2.py`) referenced below are in `$S`. Every finding was reproduced twice; "x2" marks where the second run's output was byte-identical to the first.

Blueprint authored from docs/dsl.md (`$S/db.cf.yaml`): RDS `Instance` (provider-aws-rds v2.7.0) + native `Secret` (`provider: k8s`), five parameters (`providerName`, `region` required; `storageGB` integer default 20; `engineVersion` string enum; `deletionProtection` boolean), three status wires (`resources.db.status.atProvider.{endpoint,address}` into `stringData[...]`, `.arn` into an annotation), one `raw:` with `$xr`, one envelope `writeConnectionSecretToRef.name`.

What worked as documented (no finding): `cf init` scaffold + overwrite refusal (exit 1); `cf gen` go-templating/kcl/python on a raw-free blueprint; `crossplane composition render` of all three engines' output (3 objects each); `cf gen --validate` on all three engines (`render validation ok`); determinism (`diff -r` of two runs empty, x2); `--check` exit 0/2/2 for in-sync/drifted/missing; `--group-suffix`; `--template-source filesystem`; `spec.emit.engine` + CLI override; `cf package` (.xpkg and `--yaml`), and `cf adopt <package.yaml>` round-trips to byte-identical output modulo the `# Source:` header; `cf provider add` bogus ref → `MANIFEST_UNKNOWN`, exit 1, lockfile untouched; `cf mcp` `initialize`/`tools/list`/`tools/call` over stdio, 19 tools exactly as docs/mcp.md lists; MCP `generate` refuses an `out` argument (schema `additionalProperties:false`) and writes only under `--out`; `list_kinds` over MCP returns the same 5 kinds as `GET /api/kinds?search=Instance&limit=5`; CLI `cf gen` on the MCP-rewritten blueprint produces output byte-identical to MCP `generate {"write":true}` (README:61 claim holds).

---

## Findings

### F1 — P0 — An unknown top-level key under `spec` (`resourcez:`) is silently accepted by `cf gen`, which emits a Composition with zero composed resources and exits 0; the HTTP and MCP front doors reject the same document.

Command (x2):
```
cf gen err/e9-unknownkey.cf.yaml -o err/out-e9
```
Literal output (both runs):
```
wrote err/out-e9/compositions/xdatabases.platform.example.org.yaml
wrote err/out-e9/functions.yaml
wrote err/out-e9/providerconfigs/aws.yaml
wrote err/out-e9/xrds/xdatabases.platform.example.org.yaml
exit=0
```
Emitted template body (entire content after `template: |`):
```
          {{- $spec := .observed.composite.resource.spec -}}
          {{- $xr := .observed.composite.resource.metadata.name -}}
          {{- $xrMeta := .observed.composite.resource.metadata -}}
  - step: auto-ready
```
Same document via HTTP (x2), `PUT /api/blueprint` on `cf serve --addr 127.0.0.1:19130 --no-ui`:
```
{"error":"invalid request body: json: unknown field \"resourcez\""}
HTTP 400
```
Same via MCP `replace_blueprint` (x2): `isError=True :: invalid request body: json: unknown field "resourcez"`.
Input: `db.cf.yaml` with the single line `  resources:` changed to `  resourcez:`. Contradicts README:10 ("Every field in your blueprint is strictly validated ... at author/generate time") for the CLI path.

### F2 — P1 — `cf adopt <composition.yaml>` (no XRD alongside) retypes every parameter as `string` and drops `required`, `default`, `enum` and `description` with no message; the `required` loss survives regeneration into the XRD and the Composition, so an XR may omit `region` and the rendered `Instance` lacks a CRD-required field.

Command (x2, `adopted-1.cf.yaml` and `adopted-2.cf.yaml` are identical):
```
cf adopt out-go/compositions/xdatabases.platform.example.org.yaml -o adopted-1.cf.yaml
```
Literal output (both runs):
```
Adopted blueprint written to adopted-1.cf.yaml
exit=0
```
Adopted `spec.xrd.parameters` (verbatim from `adopted-1.cf.yaml`):
```
      deletionProtection:
        required: false
        type: string
      engineVersion:
        required: false
        type: string
      providerName:
        description: Crossplane ProviderConfig name to use for managed resources
        required: true
        type: string
      region:
        required: false
        type: string
      storageGB:
        required: false
        type: string
```
(source had `region: required: true`, `storageGB: type: integer, default: 20`, `engineVersion: enum ["15","16"], default "16"`, `deletionProtection: type: boolean, default: false`, and descriptions on all five.)

Regenerating stops on the first type mismatch (x2):
```
cf gen adopted-1.cf.yaml -o out-adopted
cf: error: resource "db" field "allocatedStorage" has type "number" in the CRD schema, but parameter "storageGB" has type "string" — the wire would render a YAML scalar of the wrong type, which the API server rejects on apply
exit=1
```
After fixing `storageGB` to integer, the second one (x2):
```
cf gen adopted-1-fix1.cf.yaml -o out-adopted-fix1
cf: error: resource "db" field "deletionProtection" has type "boolean" in the CRD schema, but parameter "deletionProtection" has type "string" — the wire would render a YAML scalar of the wrong type, which the API server rejects on apply
exit=1
```
After fixing both types (`adopted-1-fix2.cf.yaml`), `cf gen` exits 0 (x2) and the `required` drop is now in the cluster-bound artefacts:
```
out-go/xrds/xdatabases.platform.example.org.yaml:46:            required: [providerName, region]
out-adopted-fix2/xrds/xdatabases.platform.example.org.yaml:36:            required: [providerName]
```
and the Composition gained a guard that lets `region` be absent:
```
>               {{- if hasKey $spec "region" }}
                region: {{ $spec.region | quote }}
>               {{- end }}
```
`cf fields Instance --required` reports `region string true`, so an XR created without `region` renders an `Instance` the API server rejects. Nothing is printed about the retyping; README:121 promises lossy adoption is "named on screen rather than silently dropped". Adopting the whole directory (`cf adopt out-go -o adopted-dir.cf.yaml`, which finds `xrds/`) preserves types/required/defaults/enum/descriptions, and regenerates byte-identically apart from the `# Source:` header (see F15).

### F3 — P1 — Omitting a CRD-required field (`region` on `Instance`) passes `cf gen` with exit 0 and no warning; only `--validate` catches it, and `PUT /api/blueprint` + `POST /api/generate` accept it too.

Command (x2):
```
cf gen err/e8-noregion.cf.yaml -o err/out
```
Literal output (both runs): four `wrote ...` lines, `exit=0`. The emitted `forProvider` has no `region` key.

With `--validate` (x2):
```
cf gen err/e8-noregion.cf.yaml -o err/out-e8 --validate
cf: error: render validation failed:
           line 71: resource "db" (Instance): missing required field "spec.forProvider.region" in Instance spec.forProvider
exit=1
```
HTTP: `PUT /api/blueprint` with the same document → `HTTP 200` and the blueprint echoed; `POST /api/generate {"write":false}` → `HTTP 200` with outputs. The requiredness is known to the tool (`cf fields Instance --required` prints exactly `region string true`; MCP `get_kind_fields required_only` returns `{"path":"region","required":true,"requiredChain":true}`), but the default generate path does not apply it. README:10 and README:206 ("Strict Schema Enforcement ... Required view shows effective requiredness") describe validation at generate time. (`line 71` in the `--validate` message refers to a file that was never written — see F8.)

### F4 — P1 — A `spec.conventions` entry whose suffix matches a native-kind field is silently ignored (not applied, not refused); docs/dsl.md:342 and :418 say it is refused.

Command (x2):
```
cf gen err/h2-conv-native.cf.yaml -o err/out-h2-conv-native
```
Blueprint delta from `db.cf.yaml`: `templates: {imm: "true\n"}` and `conventions: [{match: immutable, template: imm}]` (the `Secret` schema has a top-level `immutable` boolean; `cf fields Secret` lists it). Literal output (both runs): four `wrote ...` lines, `exit=0`. The emitted Secret contains no `immutable:` key; the `{{- define "imm" }}` block is emitted but never called. The same happens with `match: type` against the explicitly-set `type` field (`err/f5-convnative.cf.yaml`, x2, exit 0, `type: 'Opaque'` unchanged). Control: the same convention shape on a managed field works — `err/h1-conv-managed.cf.yaml` (`match: tags`) emits
```
              tags: {{ include "std-tags" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "db" "field" "tags") | trim | nindent 6 }}
```
The documented error text `resource "<name>": conventions cannot match native Kubernetes kind` never appears. A user relying on a convention (e.g. labels) for native kinds gets nothing on the cluster and no signal.

### F5 — P2 — The resource write path (MCP `add_resource`/`update_resource`, HTTP `POST /api/blueprint/resources`, `PUT /api/blueprint/resources/{name}`) accepts an unknown kind or a typo'd field path, returns success, and persists it to the blueprint file, after which every `generate`/`render_check` fails until the file is hand-fixed; `PUT /api/blueprint` on the same server does validate.

MCP, sequential driver `mcpdrv2.py` (x2):
```
--- add_resource(unknown kind Instanze): isError=None :: {"apiVersion":"factory.crossplane.io/v1alpha1","kind":"Blueprint",...
--- render_check(valid blueprint): isError=True :: kind "Instanze" not found in any cached provider; did you mean "Instance"?
```
and `grep -n bogus mcpws4/db.cf.yaml` → `100:    name: bogus` (persisted). MCP `mcpdrv.py` (x2):
```
--- update_resource(typo instanceClas): isError=None :: {"apiVersion":...
--- generate(write:false) after typo: isError=True :: resource "db": field "instanceClas" is not in Instance spec.forProvider; did you mean "instanceClass"? (an unknown field is silently pruned by the API server on apply, so it must be caught here)
--- render_check after typo: isError=True :: resource "db": field "instanceClas" is not in Instance spec.forProvider; did you mean "instanceClass"? (...)
```
HTTP (x2), `curl -X PUT .../api/blueprint/resources/db -d '{"name":"db","kind":"Instance","provider":"ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0","fields":{"region":{"from":"params.region"},"instanceClas":{"value":"db.t3.micro"}}}'` → `HTTP 200`, blueprint echoed; `grep -n instanceClas servews/db.cf.yaml` → `9:      instanceClas:`. `POST /api/blueprint/resources` with `"kind":"Instanze"` → `HTTP 200`. Contrast: `PUT /api/blueprint` with the same typo → `HTTP 400 {"error":"resource \"db\": field \"instanceClas\" is not in Instance spec.forProvider; did you mean \"instanceClass\"? ..."}`. So the MCP≡HTTP claim (docs/mcp.md:6-7) holds, but the contract is that a successful write can leave the workspace un-generatable; an agent has to know to call `generate {"write":false}` after every write.

### F6 — P2 — `cf adopt` refuses cf's own `--engine kcl` and `--engine python` Compositions with an error about a pipeline-step name collision, which does not tell the user the real cause (only function-go-templating / patch-and-transform are adoptable).

Command (x2 each):
```
cf adopt out-kcl/compositions/xdatabases.platform.example.org.yaml
cf adopt out-py/compositions/xdatabases.platform.example.org.yaml
```
Literal output (identical for both engines, both runs):
```
cf: error: adopt composition: validate adopted blueprint: spec.pipeline[0].name: "render-resources" collides with the built-in templating step's name; pick another -- Crossplane requires pipeline step names to be unique
exit=1
```
The user did not write a pipeline; the adopter treated `function-kcl`/`function-python` as a custom step named `render-resources` and then rejected its own construction. Not actionable without knowing the adopter's engine allow-list (docs/cli.md:195 lists the two supported forms but does not say kcl/python are refused).

### F7 — P2 — A field written as a bare scalar (`engine: postgres` instead of `engine: {value: postgres}`) fails with a Go JSON-internals message that names no resource, field or line.

Command (x2):
```
cf gen err/e12-nomode.cf.yaml -o err/out-x
```
Literal output (both runs):
```
cf: error: err/e12-nomode.cf.yaml: parse blueprint: json: cannot unmarshal string into Go value of type blueprint.rawField
exit=1
```
Contrast the same class of mistake with two modes set, which is precise: `cf: error: err/e11-twomodes.cf.yaml: resource "db" field "engine": set exactly one of from, value, raw or template (got 2)`.

### F8 — P3 — `--validate` failures cite `line N` of a composition that was never written to disk (validation aborts before the `wrote` step), so the number cannot be looked up.

Command (x2): `cf gen err/e8-noregion.cf.yaml -o err/out-e8 --validate` → `render validation failed:\n           line 71: resource "db" (Instance): missing required field ...`; `ls err/out-e8` → directory absent.

### F9 — P3 — `cf fields <kind>` fuzzy-resolves a misspelt kind to a different kind, and picks one of several exact matches, without saying so.

Command (x2): `cf fields Instanc` → header
```
KIND:       InstanceProfile
APIVERSION: iam.aws.m.upbound.io/v1beta1
```
(`Instance` in rds exists and is the closer match; `cf gen` with the same typo says `kind "Instanze" not found ...; did you mean "Instance"?`). `cf fields Instance` prints `rds.aws.m.upbound.io/v1beta1` although `cf kinds` lists two exact `Instance` rows (`rds.aws.m.upbound.io/v1beta1 Namespaced` and `rds.aws.upbound.io/v1beta3 Cluster`); no note that a choice was made. `cf fields Bogus` is fine: `kind "Bogus" not found in cache or blueprint sources; run: cf catalogue Bogus or cf provider add <ref>`.

### F10 — P3 — `cf provider add` on a ref that is already cached and pinned always contacts the registry and reports `added`, with no indication of a cache hit; offline it fails.

Online (x2):
```
cf provider add ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0 --lock $S/.cf.lock
added ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
  digest sha256:16c8fb42f66c5bae371abdabb9ce7f82f53d390a486b0bb51e9842137d83bee7
  44 managed resources of 44 CRDs
exit=0
```
(lockfile correctly not duplicated on re-add.) Offline (`HTTPS_PROXY=http://127.0.0.1:9`, x2):
```
cf: error: fetch "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0": Get "https://ghcr.io/v2/": proxyconnect tcp: dial tcp 127.0.0.1:9: connect: connection refused
exit=1
```
`cf gen` offline with cached providers works; with an uncached provider it says `provider "ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0` (x2, exit 1) — actionable.

### F11 — P3 — `cf help` is an error (exit 80) although `cf --help` says `Run "cf <command> --help"`.

Command (x2): `cf help` → prints top-level usage then `cf: error: unexpected argument help`, exit 80.

### F12 — P3 — Default blueprint filename is inconsistent: `cf init` writes `blueprint.cf.yaml`, but `cf kinds`, `cf fields`, `cf serve` default to `--blueprint doc.cf.yaml`; `cf init`'s own next-step hint points at `cf kinds`, which will not read the file just created.

Command (x2): `cf init` →
```
scaffolded blueprint.cf.yaml
next: cf kinds, cf provider add, or cf gen blueprint.cf.yaml
```
`cf kinds --help` → `--blueprint="doc.cf.yaml"`. With a blueprint whose source is uncached, `cf kinds --blueprint kinds-probe.cf.yaml VPC` aborts the whole listing (`cf: error: load provider schemas ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0: provider ... is not in the cache; run: cf provider add ...`, exit 1) rather than listing cached kinds.

### F13 — P3 — The `providerName` remedy sends a CLI user to `cf serve`.

Command (x2): `cf gen err/e3-noprovidername.cf.yaml -o err/out` →
```
cf: error: err/e3-noprovidername.cf.yaml: spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}
exit=1
```
(matches docs/cli.md:314 verbatim; `cf init` is the CLI scaffold.)

### F14 — P3 — Blueprints rewritten by the MCP/HTTP write path carry zero-value noise, and parameter defaults are serialised as strings.

On-disk after MCP `update_resource` (x2, `diff db.cf.yaml mcpws3/db.cf.yaml`):
```
>   conventions: null
>   resources:
>   - fields:
>       instanceClass:
>         from: ""
>         raw: ""
>         template: ""
>         value: db.t3.micro
...
>     forEach: ""
>     when: ""
```
`get_blueprint` JSON: `"storageGB":{"type":"integer","required":false,"enum":null,"default":"20",...}`, `"deletionProtection":{...,"default":"false"}`; adopted-from-directory YAML: `default: "20"`, `default: "false"`. Generation coerces correctly (XRD emits `default: 20` / `default: false`; CLI `gen` on the noisy file reproduces MCP output byte-for-byte), so this is cosmetic, but a human editing the file after an agent will see it.

### F15 — P3 — Adopt round trip renames the blueprint to the Composition name, so regenerated files differ in the `# Source:` header only.

`cf adopt out-go -o adopted-dir.cf.yaml; cf gen adopted-dir.cf.yaml -o out-adopted-dir; diff -r out-go out-adopted-dir` (x2, also via `cf adopt pkg.package.yaml`):
```
2c2
< # Source: xdatabase
---
> # Source: xdatabases.platform.example.org
```
in each of compositions/, xrds/, functions.yaml. Nothing else differs.

### F16 — P3 — KCL and Python engines emit a `"default"` fallback for `providerConfigRef.name` that go-templating does not.

`out-kcl/compositions/...yaml`: `name = _spec?.providerName or "default"`; `out-py/...`: `"name": spec.get("providerName", "default")`; `out-go/...`: `name: {{ $spec.providerName }}`. The XRD marks `providerName` required so the branch is unreachable today, but the tool's own `scope: Cluster` refusal names "silently bind every composed resource to the ProviderConfig named "default"" as the hazard it exists to avoid.

### F17 — P3 — `GET /api/functions` returns `{"functions":null}` for an empty lockfile rather than an empty list.

`curl -s http://127.0.0.1:19130/api/functions` → `{"functions":null}`, `HTTP 200` (x2).

### F18 — P3 — `spec.templates` bodies cannot reference `$xr`/`$spec` (refused at parse) and are instead called with a dot-context (`.spec`, `.xr`, `.xrMeta`, `.observed`, `.resource`, `.field`), none of which docs/dsl.md documents.

Command (x2): `cf gen err/g2-conv.cf.yaml -o err/out-g2 --validate` with template `Name: {{ $xr }}` →
```
cf: error: err/g2-conv.cf.yaml: spec.templates.std-tags: does not parse under the rendering engine (text/template, missingkey=error, function-go-templating's function set): template: cf-validate:2: undefined variable "$xr"
exit=1
```
The emitted call site (F4 control) shows the actual context: `include "std-tags" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "db" "field" "tags")`. docs/dsl.md:202-211 lists `$xr` etc. for `raw:` only; :214-226 shows `.spec.oidcProviderArn` inside a template without saying `.xr`/`.xrMeta`/... exist.

---

## Error ergonomics — verbatim messages and verdict

| Case | Command | Literal error | Actionable without source? |
|---|---|---|---|
| Misspelt field path | `cf gen err/e1-typo.cf.yaml -o err/out` | `cf: error: resource "db": field "instanceClas" is not in Instance spec.forProvider; did you mean "instanceClass"? (an unknown field is silently pruned by the API server on apply, so it must be caught here)` exit 1 | Yes |
| Unknown kind | `cf gen err/e2-kind.cf.yaml -o err/out` | `cf: error: kind "Instanze" not found in any cached provider; did you mean "Instance"?` exit 1 | Yes |
| `providerName` omitted | `cf gen err/e3-noprovidername.cf.yaml -o err/out` | see F13 | Yes (but points to `cf serve`) |
| `from: params.<unknown>` | `cf gen err/e4-unknownparam.cf.yaml -o err/out` | `cf: error: err/e4-unknownparam.cf.yaml: resource "db" field "allocatedStorage": references unknown parameter "storageGb"` exit 1 | Yes (no nearest-match hint) |
| Status wire to unknown resource | `cf gen err/e5-unknownres.cf.yaml -o err/out` | `cf: error: err/e5-unknownres.cf.yaml: resource "conn" field "stringData[endpoint]": references unknown resource "database"` exit 1 | Yes |
| Status wire to unknown status leaf | `cf gen err/e6-unknownstatus.cf.yaml -o err/out` | `cf: error: resource "conn" field "stringData[endpoint]": "atProvider.endpint" is not a scalar leaf in Instance's status schema; did you mean "atProvider.endpoint"? (a status path the provider never writes would leave the guard false forever and the field silently absent, so it must be caught here)` exit 1 | Yes |
| String literal into number field | `cf gen err/e7-type.cf.yaml -o err/out` | `cf: error: resource "db" field "allocatedStorage": value "twenty" is not a valid number (NaN and Inf are refused)` exit 1 | Yes |
| CRD-required field omitted | `cf gen err/e8-noregion.cf.yaml -o err/out` | (none) exit 0 | F3 |
| Unknown top-level key | `cf gen err/e9-unknownkey.cf.yaml -o err/out` | (none) exit 0 | F1 |
| Boolean param wired to string field | `cf gen err/e10-parammismatch.cf.yaml -o err/out` | (none) exit 0; emits `engineVersion: {{ $spec.deletionProtection \| quote }}` | Valid YAML string, API-server-legal; not a finding |
| Two modes on one field | `cf gen err/e11-twomodes.cf.yaml -o err/out` | `cf: error: err/e11-twomodes.cf.yaml: resource "db" field "engine": set exactly one of from, value, raw or template (got 2)` exit 1 | Yes |
| Bare scalar field | `cf gen err/e12-nomode.cf.yaml -o err/out` | see F7 | No |
| `scope: Cluster` | `cf gen err/f1-cluster.cf.yaml` | `cf: error: err/f1-cluster.cf.yaml: spec.xrd.scope: Cluster is not supported in M1 -- use Namespaced. The cluster-scoped managed-resource envelope differs from the namespaced one (providerConfigRef is {name, policy}, not {kind, name}, and deletionPolicy exists) and the Composition emitter does not yet render it; emitting it untested would silently bind every composed resource to the ProviderConfig named "default". Cluster scope is planned work, not a permanent restriction` exit 1 | Yes |
| `type: array` param | `cf gen err/f2-array.cf.yaml` | `cf: error: err/f2-array.cf.yaml: spec.xrd.parameters.deletionProtection: type "array" is not supported in M1. The XRD emitter cannot write the required items: schema for it, and a from: mapping would render Go's fmt of the slice ("[a b c]") -- valid YAML, silently wrong. Use a scalar parameter, or a raw: field for a literal list` exit 1 | Yes |
| Status wire in envelope | `cf gen err/f3-envstatus.cf.yaml` | `cf: error: err/f3-envstatus.cf.yaml: resource "db" envelope "writeConnectionSecretToRef.name": from must start with params. or env. (got "resources.conn.status.atProvider.arn") -- cross-resource status wires are supported in fields:, not in envelope entries, in v1` exit 1 | Yes |
| `template:` on native field | `cf gen err/f4-tmplnative.cf.yaml` | `cf: error: err/f4-tmplnative.cf.yaml: resource "conn" field "type": template: fields are not supported on a native Kubernetes resource (provider "k8s") in v1 -- a template call's output re-indents to the fixed forProvider column, which a native field at an arbitrary nesting depth breaks. Set the field with value:, raw: or from:` exit 1 | Yes |
| Convention on native kind | `cf gen err/f5-convnative.cf.yaml` | (none) exit 0 | F4 |
| Unknown template name | `cf gen err/f6-unknowntemplate.cf.yaml` | `cf: error: err/f6-unknowntemplate.cf.yaml: resource "db" field "engine": references unknown template "nope" (declare it under spec.templates)` exit 1 | Yes |
| Uncached provider | `cf gen err/f7-uncachedprovider.cf.yaml` | `cf: error: provider "ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0` exit 1 | Yes |
| YAML syntax error | `cf gen err/f8-yamlsyntax.cf.yaml` | `cf: error: err/f8-yamlsyntax.cf.yaml: parse blueprint: yaml: line 37: did not find expected node content` exit 1 | Yes |
| `environment` key without type | `cf gen err/f9-envkey.cf.yaml` | `cf: error: err/f9-envkey.cf.yaml: spec.environment.vpcId: type is required (must be string, integer, number, or boolean)` exit 1 | Yes |
| `when:` unquoted literal | `cf gen err/f10-whenbad.cf.yaml` | `cf: error: err/f10-whenbad.cf.yaml: resource "conn": when must be params.<name>/env.<key> (a boolean), params.<name>/env.<key> == "<literal>" or params.<name>/env.<key> != "<literal>" — exactly one space around the operator, double quotes around the literal, no backslashes or embedded quotes (got "params.region == prod")` exit 1 | Yes |
| `forEach:` over string param | `cf gen err/f11-foreachstr.cf.yaml` | `cf: error: err/f11-foreachstr.cf.yaml: resource "conn": forEach parameter "region" has type "string", want integer -- the loop bound renders as until (int $spec.region), a repetition count` exit 1 | Yes |
| `template:` under `--engine kcl` | `cf gen err/g1-tmplkcl.cf.yaml --engine kcl` | `cf: error: resource "db" field "engine": engine "kcl" does not support template: fields` exit 1 | Yes |
| `raw: {{ }}` under kcl/python | `cf gen db.cf.yaml -o out-kcl --engine kcl` | `cf: error: resource "db" field "tags[Name]": raw "{{ $xr }}" contains Go-template syntax which is only supported with the go-templating engine (current engine is "kcl")` exit 1 | Yes |
| `--engine jsonnet` | `cf gen db.cf.yaml --engine jsonnet` | `cf: error: --engine must be "go-templating", "kcl", or "python", got "jsonnet"` exit 1 | Yes |
| `--template-source disk` | `cf gen db.cf.yaml --template-source disk` | full usage text then `cf: error: --template-source must be one of "inline","filesystem" but got "disk"` exit 80 | Yes (inconsistent with `--engine`, which exits 1 without usage) |
| `provider add` unparsable ref | `cf provider add 'not a ref!'` | `cf: error: parse reference "not a ref!": could not parse reference: not a ref!` exit 1 | Yes |
| MCP unknown tool | `tools/call no_such_tool` | JSON-RPC error `{"code": -32602, "message": "unknown tool \"no_such_tool\""}` | Yes |
| MCP wrong arg name | `get_kind_fields {"apiVersion":...}` | `validating "arguments": validating root: unexpected additional properties ["apiVersion"]` | Yes (schema says `api_version`) |
| MCP wrong arg type | `list_kinds {"limit":"five"}` | `validating "arguments": validating root: validating /properties/limit: type: five has type "string", want "integer"` | Yes |
| MCP delete referenced param | `delete_parameter {"name":"region"}` | `delete parameter "region": still referenced by resources "db", "conn"` | Yes |
| MCP partial param update | `update_parameter {"name":"storageGB","parameter":{"type":"integer"}}` | `refusing a partial update of parameter "storageGB": PUT replaces the whole parameter, so omitting default, description would silently discard the value each of them currently holds. Send those keys explicitly — their zero values (false, null, "") are how you clear one.` | Yes |
| MCP duplicate param | `add_parameter {"name":"region",...}` | `add parameter: "region" is already declared` | Yes |

---

## Doc vs binary mismatches

| Doc location | Doc says | Binary does |
|---|---|---|
| README.md:10 | "Every field in your blueprint is strictly validated ... at author/generate time" | Unknown `spec` keys accepted and resources dropped (F1); CRD-required field omission not caught without `--validate` (F3). |
| README.md:121 | Adoption losses are "named on screen rather than silently dropped" | `cf adopt` prints only `Adopted blueprint written to ...`; parameter types/required/defaults/enums/descriptions dropped silently (F2). |
| README.md:206 | "Required view shows effective requiredness" (under Strict Schema Enforcement) | Requiredness is displayed (`cf fields --required`, MCP `get_kind_fields`) but not enforced by `cf gen` (F3). |
| docs/cli.md:21,28 vs :47,:180 and `cf serve --help` | `cf init` default `blueprint.cf.yaml`; `kinds`/`fields`/`serve` default `doc.cf.yaml` | Both true; the two defaults never meet (F12). |
| docs/cli.md:195 | adopt supports "`function-go-templating` and classic patch-and-transform" | Also true; but cf's own kcl/python output is refused with a pipeline-name error, not an engine error (F6). |
| docs/cli.md:198-203 | `cf adopt <composition.yaml> -o <blueprint> [--provider <ref>]` | Binary also accepts a Configuration directory, `-` for stdin, `--cache-dir`, and the alias `import` (`cf adopt --help`). Docs omit all four. |
| docs/cli.md:242-251 | `cf provider add <ref>`; no flags listed | Binary has `--cache-dir` and `--lock=".cf.lock"` (`cf provider add --help`). |
| docs/cli.md:151-153 | filesystem tree `templates/<plural>.<group>/000-context.yaml` and `<resource>.yaml` | Actual: `000-context.yaml`, `001-db.yaml`, `002-conn.yaml` (numbered, order-prefixed). |
| docs/cli.md:217 | `--yaml` stream "importable back via the GUI or `POST /api/blueprint/import`" | Also importable via `cf adopt <package.yaml>` (verified, F15-only diff); docs omit the CLI route. |
| docs/cli.md:314 | providerName error text | Verbatim match (points to `cf serve`, F13). |
| docs/dsl.md:342, :418 | Conventions "refused on native Kubernetes kinds"; error `resource "<name>": conventions cannot match native Kubernetes kind` | Silently ignored, exit 0 (F4). |
| docs/dsl.md:414 | `resource "<name>" field "<path>": unknown path -- did you mean "<suggestion>"?` | Actual: `resource "db": field "instanceClas" is not in Instance spec.forProvider; did you mean "instanceClass"? (an unknown field is silently pruned by the API server on apply, so it must be caught here)` |
| docs/dsl.md:415 | `... template: fields are not supported on a native Kubernetes kind...` | Actual: `... template: fields are not supported on a native Kubernetes resource (provider "k8s") in v1 -- ...` |
| docs/dsl.md:417 | `resource "<name>" envelope "<path>": status wires (...) are not supported in envelope` | Actual: `resource "db" envelope "writeConnectionSecretToRef.name": from must start with params. or env. (got "...") -- cross-resource status wires are supported in fields:, not in envelope entries, in v1` |
| docs/dsl.md:408, :410 | Cluster-scope and array-type messages | Match as prefixes; binary appends multi-sentence rationale. |
| docs/dsl.md:411, :416 | environment-type and engine-template messages | Verbatim match. |
| docs/dsl.md:202-226 | `$xr`, `$spec` ... "available in `raw`"; template example uses `.spec.x` | Templates refuse `$xr` at parse; the actual template context (`.spec .xr .xrMeta .observed .resource .field`) is undocumented (F18). |
| docs/mcp.md:3 | "`cf mcp` serves compositionfactory's full authoring surface" | 19 tools vs 36 HTTP routes. No MCP tool for: `GET /api/kinds/{apiVersion}/{kind}`, `POST /api/blueprint/import`, `GET /api/package`, `POST /api/sources/crds`, `DELETE /api/providers/{ref}`, `GET /api/functions`, `GET /api/rbac`, `GET /api/examples`, `GET /api/examples/{id}`, `POST /api/examples/{id}/load`, `GET /api/catalogue`, `GET /api/cluster`, `POST /api/cluster/sync`, `POST /api/cluster/connect`, `GET /api/version`, `GET /healthz`. |
| docs/mcp.md:6-7 | Tools and HTTP "validate identically and report identical error messages" | Holds in every case tested (typo, unknown key, unknown kind), including the shared gap in F5. |
| docs/mcp.md:65-85 tool table, :87 "19 MCP tools" | 19 tools | Matches `tools/list` exactly. |
| docs/mcp.md:68 | `get_kind_fields` ... `GET /api/kinds/{apiVersion}/{kind}/fields` | Tool argument is `api_version` (snake_case); passing `apiVersion` is rejected (`unexpected additional properties ["apiVersion"]`). Docs never name the argument. |
| docs/mcp.md:93-129 HTTP route table | 36 routes | Matches `internal/api/server.go` `mux.HandleFunc` lines 172-206 exactly. |
| docs/mcp.md:58 | "everything else works entirely offline" | Verified: `cf gen` with cached providers succeeds behind a dead proxy; `cf mcp` needed no network for all non-`add_provider` tools. |
| `cf --help` footer | `Run "cf <command> --help"` | `cf help` itself is an error, exit 80 (F11). |
| docs/cli.md:123 / `cf gen --help` | `--check` exits 0 in sync, 2 drifted | Verified: `in sync`/0; `drift: out-drift/functions.yaml` + `generated output is stale; run: cf gen`/2; missing tree lists all four paths as `drift:` /2. |
