# Front-end building blocks for a Crossplane node-graph schema editor

All numbers below marked **[measured]** were produced on this machine (node v26.7.0, vite 8.2.2 / Rolldown, `gzip -9`, brotli q11) or against the live `kind-platform` cluster. Everything else is marked **[docs]** or **[unverified]**.

## Decisions this enables

1. **Take `@xyflow/react` 12.11.5 (MIT).** It is the only candidate that renders nodes as real DOM, which is a hard requirement for putting forms inside nodes. Cytoscape and LiteGraph draw to `<canvas>` and are disqualified. Confirmed by prior art: `crossplane-contrib/crossview` v4.5.0 ships `@xyflow/react` ^12.9.3.
2. **Take `@rjsf/core` 6.8.0, not JSONForms.** [measured] Against the same real Upbound EC2 `LaunchTemplate` `forProvider` schema, rjsf rendered **88 inputs**; JSONForms rendered **15** — and JSONForms rendered **literally nothing** (75 bytes of HTML, 0 inputs) for `additionalProperties` maps, `oneOf`, and any object nested deeper than one level, even with `Generate.uiSchema()`. That is disqualifying for a tool that must eat arbitrary provider CRDs with zero hand-authoring.
3. **Take CodeMirror 6, not Monaco.** [measured] Same feature set (yaml + lint + autocomplete + folding + find), same build: CM6 = **251 KB gzip total bundle**; minimal tree-shaken Monaco = **899 KB gzip + 137 KB CSS**; naive `import 'monaco-editor'` = **3.30 MB gzip across 92 files**, including a 6.9 MB TypeScript worker you will never use. crossview independently chose CodeMirror.
4. **Serve schemas from the Go backend, do not bundle them — but for parse/memory reasons, not transfer reasons.** [measured] All 204 EC2 CRD schemas are 4.27 MB raw but only **238 KB gzip / 54 KB brotli**, because CRD schemas contain **zero `$ref`** and are massively repetitive. Stripping `description` alone takes it to **37 KB gzip**. Transfer size is a non-issue; the reason to keep it server-side is that you will have N providers and want one lazily-fetched schema per node type.
5. **Skip y.js entirely.** This is YAGNI, and the cost is not the 30 KB — it is that CRDT-backed state forbids the `immer` + snapshot-array undo model that a graph editor actually wants. Ship `zustand` + `immer` + a bounded snapshot stack.

---

## 1. Canvas library

### Versions and licenses [measured via `npm view`]

| Package | Version | License | Last publish |
|---|---|---|---|
| `@xyflow/react` | 12.11.5 | MIT | 2026-08-25 |
| `reactflow` (v11, legacy name) | 11.11.4 | MIT | — |
| `@xyflow/svelte` | 1.6.5 | MIT | — |
| `rete` | 2.0.6 | MIT | 2025-06-30 |
| `rete-react-plugin` | 2.1.2 | MIT | 2026-07-10 |
| `litegraph.js` | 0.7.18 | MIT | **2024-01-08 (dead)** |
| `@comfyorg/litegraph` | 0.17.2 | MIT | 2026-08-21 |
| `cytoscape` | 3.34.2 | MIT | 2026-08-25 |
| `elkjs` | 0.12.0 | **EPL-2.0 OR GPL-3.0-or-later** | — |
| `@dagrejs/dagre` | 3.1.1 | MIT | — |

### The xyflow license question, settled

The package is **MIT**, full stop. [measured] `node_modules/@xyflow/react/LICENSE` reads `MIT License / Copyright (c) 2019-2025 webkid GmbH`, and `package.json` declares `"license": "MIT"`.

History: React Flow was MIT from 2019. In 2022 the team introduced "React Flow Pro" and wording implying you must subscribe to remove the attribution watermark, which triggered [xyflow/xyflow discussion #2015](https://github.com/xyflow/xyflow/discussions/2015). The maintainers' resolution there: attribution removal is **a request, not a legal requirement** — "We won't change the MIT license so you can do whatever you want with it." The current [Remove Attribution docs page](https://reactflow.dev/learn/troubleshooting/remove-attribution) still leads with the Pro subscription ask and says "React Flow will always be open source and MIT-licensed."

[measured] Hiding it is a plain runtime code path, not a license gate — `dist/esm/index.js` contains `function Attribution({ proOptions, position = 'bottom-right' ...`, driven by `proOptions.hideAttribution`, and `hideAttribution` is a documented field in the shipped `types/general.d.ts`. For an open-source tool the honest posture is: set `proOptions={{hideAttribution: true}}` only if you also credit xyflow in your README, or just leave the watermark on.

**Practical note:** `elkjs` being EPL-2.0/GPL-3.0 is the one real license trap in this list. EPL-2.0 is a weak/file-level copyleft and fine to depend on from an Apache-2.0 or MIT tool, but it is not MIT, and the GPL-3.0 alternative in that `OR` will make some corporate legal reviews slow. `@dagrejs/dagre` is MIT and has no such issue.

### Why React Flow, criterion by criterion

**Custom node bodies containing forms — the decisive criterion.** React Flow nodes are React components rendered into absolutely-positioned DOM inside a transformed container. You register them via `nodeTypes` and get `NodeProps<NodeType extends Node>`. [measured] I compiled a node component whose body is a live `<Form>` from rjsf, with `<Handle type="target">` / `<Handle type="source">` on either side; it builds and typechecks.

- **Cytoscape.js: disqualified.** It renders the graph to `<canvas>`. Putting a form in a node requires the third-party `cytoscape-dom-node` extension, which overlays absolutely-positioned DOM on top of canvas coordinates — you inherit two coordinate systems and all the hit-testing and z-order bugs that follow. Cytoscape is a graph *analysis and visualization* library (it ships BFS, PageRank, centrality); you need a *node editor*.
- **LiteGraph: disqualified.** Canvas-drawn widgets with its own immediate-mode widget system. `litegraph.js` proper last published 2024-01-08 and is effectively abandoned; the living fork `@comfyorg/litegraph` is maintained *for ComfyUI* and its roadmap follows ComfyUI's needs, not yours.
- **Rete.js 2.0.6: viable but wrong shape.** It is genuinely modular and framework-agnostic with `rete-react-plugin` 2.1.2 for React node bodies, so forms-in-nodes works. The cost is that "modular" means you assemble ~6 packages (`rete`, `rete-area-plugin`, `rete-connection-plugin`, `rete-react-plugin`, `rete-render-utils`, `rete-auto-arrange-plugin`) and its data model is a classic node/socket/control graph you must map onto Crossplane concepts. React Flow's model is closer to what you want and its docs/examples are far denser.
- **Svelte Flow 1.6.5:** same core (`@xyflow/system`), same quality. Only pick it if the whole app is Svelte. Given rjsf is React-only, choosing Svelte means hand-rolling the schema form layer too — a large, avoidable cost.

**Edge validation.** [measured, from shipped `.d.ts`] React Flow 12 exposes `isValidConnection?: IsValidConnection` both on `<ReactFlow>` and per-`<Handle>`, with the shipped docstring explicitly saying: *"we recommend you move this logic to the `isValidConnection` prop on the main ReactFlow component for performance reasons."* Also present: `onReconnect`, `onReconnectStart`, `onReconnectEnd`, and a `connectionState` of `"valid"`/`"invalid"` that lets you style an in-flight connection. This is exactly the hook you need to reject a wire whose source field type does not match the target's `...Ref`/`...Selector` shape.

**Large graphs.** [measured] `onlyRenderVisibleElements?: boolean` exists on the `ReactFlow` props type. [docs] The [official performance page](https://reactflow.dev/learn/advanced-use/performance) gives no node-count number; its guidance is: memoize custom node components with `React.memo` or declare them outside the parent, never read the whole `nodes` array from inside a node component ("every update to the `nodes` array triggers a re-render of all dependent components, even if the change is unrelated"), keep selection state out of the nodes array, and simplify CSS (shadows/animations) at scale. **[unverified]** I did not benchmark node counts headlessly. My assessment: because nodes are real DOM, expect smooth interaction into the low hundreds and degradation beyond that — which is far above what a Composition will ever hold. A Composition with 50 managed resources is already an outlier. Large-graph performance is simply not a real risk for this tool, which is another reason not to pay Cytoscape's canvas tax to get it.

**TypeScript quality.** [measured] Ships `.d.ts` + `.d.ts.map` for every module (`nodes`, `edges`, `store`, `instance`, `component-props`, `general`). `NodeProps<NodeType extends Node = Node>` is properly generic, so you can type node data as your own `ManagedResourceNodeData` and get it through to the component. This is materially better than Rete's typing and vastly better than LiteGraph's.

**Bundle size.** [measured] On top of React 19.2.8 (190,316 raw / 59,118 gzip), `@xyflow/react` 12.11.5 adds **178,470 raw / 56,233 gzip JS** plus **15,413 raw / 2,555 gzip CSS**. Dependency tree is small and clean: `classcat`, `zustand@^4.4.0`, `@xyflow/system@0.0.81` (which pulls `d3-drag`, `d3-selection`, `d3-zoom`, `d3-interpolate`).

**⚠ [measured] React Flow pins `zustand@^4.4.0` internally.** If you also use zustand 5 for app state you ship two copies:
```
node_modules/zustand                        5.0.15
node_modules/@xyflow/react/node_modules/zustand  4.5.7
```
Both end up in the bundle. It is only a few KB, but be aware you cannot share a store instance between your app state and React Flow's internal store, and `npm ls zustand` will look alarming in a bug report.

### Auto-layout: dagre, not elk

[measured] `@dagrejs/dagre` 3.1.1 `dagre.js` is **106,501 bytes raw**, MIT, one dependency (`@dagrejs/graphlib` 4.0.5). `elkjs` 0.12.0 `elk.bundled.js` is **1,609,707 raw / 466,718 gzip** — it is a GWT-transpiled Java library, which is why it is that size, and why it really wants to run in a worker (`elk-worker.min.js`, 1,595,334 bytes).

ELK produces better layouts for dense graphs with ports and orthogonal routing. A Composition DAG is small and shallow. **Use dagre with `rankdir: 'LR'`.** Paying 466 KB gzip and an EPL/GPL dependency to lay out 15 nodes is not a trade worth making. Keep ELK on the shelf as a later opt-in if someone shows up with a 200-resource Composition.

---

## 2. Schema-driven forms — the load-bearing decision

### What CRD schemas actually look like [measured, live cluster + real package]

I extracted `xpkg.upbound.io/upbound/provider-aws-ec2:v2` with `crossplane xpkg extract` (8.56 MB of YAML, 206 documents) and analyzed the live cluster's CRDs.

| Property | SQS `Queue` (your provider) | EC2 provider (204 CRDs) | Kyverno `ClusterPolicy` (worst on cluster) |
|---|---|---|---|
| openAPIV3Schema bytes | 17,161 | 4,275,487 | 647,066 |
| property nodes | 79 | 23,194 | 1,445 |
| max nesting depth | **4** | **9** | **15** |
| `$ref` count | **0** | **0** | **0** |
| `oneOf` | 0 | — | 10 |
| `not` | 0 | — | 16 |
| `additionalProperties` (object) | 4 | — | 67 |
| arrays of objects | 1 | — | 150 |
| `x-kubernetes-preserve-unknown-fields` | 0 | — | 116 |
| `x-kubernetes-map-type` | 4 | — | 53 |
| `x-kubernetes-list-type` | 1 | — | 70 |
| `x-kubernetes-validations` (CEL) | 0 | — | 0 |

Deepest path found anywhere on the cluster, at depth 16 — a Crossplane CRD, not a provider one:
```
deploymentruntimeconfigs.pkg.crossplane.io
$.spec.deploymentTemplate.spec.template.spec.volumes[].projected.sources[]
   .clusterTrustBundle.labelSelector.matchExpressions[].values[]
```
Deepest in EC2, at depth 9:
```
Fleet.spec.forProvider.launchTemplateConfig[].override[]
   .instanceRequirements.acceleratorCount.max
```

**Three findings that shape the design:**

- **Zero `$ref`, everywhere.** The Kubernetes API server flattens CRD schemas. You never need a `$ref` resolver — but you also get no structural sharing, which is why these schemas are enormous and why they compress ~18:1.
- **Depth 5+ is normal, not exotic.** Your SQS Queue is a gentle depth-4 case. Assume depth 9–16.
- **[measured] `provider-aws-ec2:v2` ships every resource twice:** 102 CRDs in `ec2.aws.m.upbound.io` (scope `Namespaced`) and 102 in `ec2.aws.upbound.io` (scope `Cluster`). The `.m.` group has only `v1beta1`; the legacy group has `v1beta1` **and** `v1beta2`. Since your XRD is `scope: Namespaced` and your Composition renders `sqs.aws.m.upbound.io/v1beta1`, **filter to `.m.` groups and halve the catalog on day one.** Also note 14 of 204 CRDs carry two versions — pick the storage/served version deliberately rather than `versions[0]`.

### rjsf vs JSONForms, tested against those schemas [measured]

I rendered both through `react-dom/server`'s `renderToString` against real schemas and a set of synthetic CRD constructs.

**rjsf 6.8.0 + `@rjsf/validator-ajv8` — every case rendered, nothing threw:**

| Construct | Result |
|---|---|
| `additionalProperties: {type: string}` (tags map) | OK — key/value editor, 2 buttons |
| `x-kubernetes-preserve-unknown-fields: true`, **no `type`** | OK — add-property button, no crash |
| `x-kubernetes-int-or-string` (`anyOf: [integer, string]`) | OK — branch `<select>` + input |
| `oneOf` with two object branches | OK — branch `<select>` + input |
| array of objects | OK — "Add" button, **0 children rendered until added** |
| 6-level deep nested objects | OK |
| unknown `x-kubernetes-*` keys on a scalar | OK — ignored harmlessly, input still renders |

Real schemas through rjsf:

| Schema | renderToString | HTML bytes | inputs |
|---|---|---|---|
| SQS `Queue` `forProvider` (your provider) | 28 ms | 11,534 | 17 |
| EC2 `VPC` | 46 ms | 17,107 | 17 |
| EC2 `SpotFleetRequest` | 18 ms | 18,347 | 21 |
| EC2 `Instance` | 39 ms | 86,044 | 76 |
| EC2 `LaunchTemplate` (263 props, depth 6) | 45 ms | 101,434 | 88 |
| Kyverno `ClusterPolicy.spec` (300 KB, depth 15) | 19 ms | 11,864 | 10 |

**JSONForms 3.8.0 (`@jsonforms/core` + `react` + `vanilla-renderers`), same inputs, tested both with no uischema and with `Generate.uiSchema(schema)`:**

| Construct | inputs | HTML bytes |
|---|---|---|
| `additionalProperties` map | **0** | 75 |
| `oneOf` | **0** | 75 |
| 6-level deep nested objects | **0** | 75 |
| array of objects | **0** | 397 |
| `x-kubernetes-preserve-unknown-fields` | **0** | 35 |
| **EC2 `LaunchTemplate`** | **15** | 8,891 |
| SQS `Queue` `forProvider` (flat, depth 4) | 17 | 6,410 |

Read that table carefully. JSONForms matches rjsf exactly on the *flat* SQS Queue (17 inputs each) and then falls off a cliff the moment the schema nests, branches, or maps. It does not crash — it silently renders an empty div. That failure mode is worse than crashing, because your tool would appear to work on the one provider you tested and quietly drop 80% of the fields on every other one. JSONForms is built on the premise that a human authors a UI schema per form; that premise is exactly what a generic CRD tool cannot satisfy.

### The lazy-rendering finding

The Kyverno row above is the important one. A 300 KB, depth-15 schema with 1,445 properties rendered in **19 ms** producing **10 inputs**. The reason: **rjsf defaults arrays to zero items, so anything nested beneath an array costs nothing until the user clicks "Add".** Combined with the fact that only the *selected* node's form is mounted, you get lazy rendering for free and do not need to build a virtualized form renderer. The "2000+ property schema" scenario in the brief resolves itself: EC2's largest single CRD is `LaunchTemplate` at 263 properties in `forProvider`, and it renders in 45 ms.

The one case where you must intervene is a schema with 200+ *scalar* siblings at one level (no arrays to hide behind). None exist in the EC2 provider. If one shows up, collapse by `x-kubernetes` grouping or add a field-filter box — do not reach for virtualization pre-emptively.

### Verdict on the other two

- **uniforms 4.0.0:** MIT, but [measured] last published **2025-02-28** — ~18 months stale. Its JSON-Schema bridge requires you to supply your own ajv instance and its `oneOf`/`additionalProperties` story is weaker than rjsf's. Skip.
- **Hand-rolled:** don't, at least not initially. The edge-case matrix above is the actual work, and rjsf already passes it. Where you *will* hand-roll is a small set of **custom widgets registered into rjsf**, which is the library's designed extension point:
  - a **reference picker** for `...Ref` / `...Selector` fields that offers the other nodes on the canvas instead of a free-text field — this is what turns the graph into more than decoration;
  - a **tags/map editor** for `additionalProperties`, since rjsf's default is functional but plain;
  - a **Go-template escape hatch** widget letting any field hold `{{ .observed... }}` instead of a literal — non-negotiable for a function-go-templating generator, and the reason a pure-forms tool would fail.

Register these via rjsf's `widgets` / `fields` / `templates` props and a `uiSchema` you *generate* from the CRD by walking for `x-kubernetes-*` markers and Upbound's `...Ref`/`...Selector` naming convention.

### Real usage

- **Headlamp** (`kubernetes-sigs`) built its CRD form generation on rjsf — [issue #2087](https://github.com/kubernetes-sigs/headlamp/issues/2087): *"I've used react-jsonschema-form (@rjsf-core) to generate the forms."* [docs] The only difficulty recorded there is MUI 5 theming, not schema handling. Note the issue does **not** discuss nesting/`x-kubernetes-*`/size, so treat it as evidence of *choice*, not of *stress-testing* — my measurements above are the stress test.
- rjsf is also the schema-form engine behind Backstage's scaffolder templates. **[unverified]** — I did not confirm the current version in this session.

---

## 3. Editor: CodeMirror 6, and what schema-awareness really costs

### Head-to-head [measured, identical harness]

| Setup | JS raw | JS gzip | CSS raw | files |
|---|---|---|---|---|
| React + React Flow (baseline) | 368,786 | 115,351 | 15,413 | 1 |
| **+ CodeMirror 6** (basicSetup, lang-yaml, lint, autocomplete) | **793,249** | **251,329** | 15,413 | 1 |
| + Monaco 0.56 **minimal** (`editor.api` + yaml + 5 features) | 3,485,759 | 898,508 | **137,420** | 2 |
| + Monaco 0.56 **naive** (`import 'monaco-editor'`) | 13,853,437 | 3,296,050 | — | **92** |

The naive Monaco build's largest chunks: `ts.worker` **6,913,951**, main index **4,241,135**, `css.worker` 1,074,885, `html.worker` 739,943, `json.worker` 429,596. You will not use any of those workers for YAML.

Net: CM6 costs **~136 KB gzip**; minimal Monaco costs **~783 KB gzip + 122 KB CSS**. **~5.8×.**

**[measured] Monaco 0.56 restructured its ESM layout** — worth knowing if your memory predates it. `esm/vs/basic-languages/` is gone. The `exports` map is now `{".": "./esm/vs/index.js", "./*": "./esm/vs/*.js"}`, and imports look like:
```js
import * as monaco from 'monaco-editor/editor/editor.api';
import 'monaco-editor/features/hover/register';
import 'monaco-editor/features/suggest/register';
import 'monaco-editor/languages/definitions/yaml/register';
```
There are now ~40 individually-importable `vs/features/*/register` modules. This is a real tree-shaking improvement over 0.4x — but even fully minimized it is still 5.8× CodeMirror.

### Why CodeMirror is right here beyond size

Your editor is a **side panel showing generated YAML plus Go-template fragments**, not an IDE. CM6's per-feature extension model fits that; Monaco's value (multi-file models, full LSP, peek definition, diff editor) is value you will not use. CM6 also injects its own styles via JS — [measured] zero extra CSS bytes in the build, versus Monaco's 137 KB stylesheet.

Go templates inside YAML need a **mixed-language highlighting** story either way: `{{ ... }}` inside YAML scalars. CM6's Lezer parser supports mixed parsing via `parseMixed`, which is a cleaner primitive than Monaco's Monarch tokenizer for this. **[unverified]** — I did not build a working `parseMixed` YAML+Go-template grammar in this session; budget real time for it, and ship plain YAML highlighting first.

### Schema-aware completion: three options, measured

**Option A — `codemirror-json-schema` 0.8.1.** [measured] It does have a `./yaml` export (`yamlSchema(schema)`), so this path is real. Two problems:
- Last published **2025-04-21**, ~16 months stale, and its peer deps pin `@codemirror/view ^6.27.0` (current 6.43.9) — works today, but it is a maintenance risk on your critical path.
- [measured] It drags in **shiki** and **markdown-it** to render hover documentation. The editor chunk goes from **135,648 → 291,367 gzip**, and the build emits extra chunks including `javascript-CppQSorr.js` at 198,024 raw (a TextMate grammar for *JavaScript*, in a YAML editor) plus `vitesse-dark`/`vitesse-light` themes. Those extras are separately chunked and lazily loaded, but the 291 KB editor chunk is eager.

**Option B — `yaml-language-server` 1.24.0 in a Web Worker.** [measured] This works and is better than I expected. `package.json` has **no `browser` field and no `exports` map** (`main` points at `./out/server/src/index.js`, a Node server entry) — but the package ships a parallel `lib/esm/` build, and importing the language *service* directly bundles cleanly:
```js
import { getLanguageService } from 'yaml-language-server/lib/esm/languageservice/yamlLanguageService.js';
```
Bundled as a Vite module worker: **1,246,772 raw / 325,596 gzip**, built with no resolution errors and no manual shims required. Because it is in a worker, none of that blocks first paint or main-thread interaction. It gives you genuine k8s-grade YAML diagnostics, completion, and hover against a JSON Schema — the same engine as VS Code's YAML extension.

**Option C — `monaco-yaml` 5.5.1.** Only relevant if you take Monaco. [measured] Two things worth knowing:
- It does **not** depend on `yaml-language-server`; it vendors the language service (67 references in its prebuilt `yaml.worker.js`, which is 380,305 bytes).
- **It stubs out ajv.** `monaco-yaml/fillers/ajv.ts` is literally:
  ```ts
  export default class AJVStub {
    compile(): () => boolean { return getTrue }   // always true
  ```
  with `ajv-draft-04.ts` extending it. So ajv-driven meta-schema validation is a no-op; you get only yaml-language-server's own hand-rolled JSON Schema validator. Fine in practice, but do not assume full draft-07 `format` semantics.

**Recommendation:** ship **Option B** — CodeMirror 6 for the editing surface, `yaml-language-server` in a worker for diagnostics, wired through `@codemirror/lint`'s async `linter()` and `@codemirror/autocomplete`'s `override`. You pay 326 KB gzip *off the main thread* and get the real thing, instead of 156 KB gzip *on* the main thread from a stale package that ships a JavaScript TextMate grammar you did not ask for. Fall back to Option A only if wiring the worker protocol proves slower than expected.

---

## 4. Go backend with embedded SPA

### Verified working [measured — built and curled]

I built this with go1.27.0 and exercised it:

```go
// web/embed.go
package web

import ("embed"; "io/fs"; "net/http"; "strings")

//go:embed all:dist
var distFS embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" { p = "index.html" }
		if _, err := fs.Stat(sub, p); err != nil {
			r = r.Clone(r.Context())   // SPA history fallback
			r.URL.Path = "/"
		} else if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
```

Observed responses:

| Request | Result |
|---|---|
| `GET /` | `200`, `Content-Type: text/html; charset=utf-8` |
| `GET /assets/index-abc123.js` | `200`, `Cache-Control: public, max-age=31536000, immutable`, `Content-Type: text/javascript` |
| `GET /some/spa/route` | `200` + index.html (fallback works) |
| `GET /api/schemas` | `200` `{"ok":true}` (API route not shadowed) |

Binary: 8,411,298 bytes with a trivial frontend — that is the Go runtime floor.

**Three details that matter:**
- **`//go:embed all:dist`, not `//go:embed dist`.** Without the `all:` prefix, `embed` skips files beginning with `.` or `_`. Vite emits `.vite/manifest.json` and some plugins emit `_`-prefixed assets.
- **`embed` cannot reach outside its own directory** — no `../frontend/dist`. Either put the Go file inside the frontend tree, or set Vite's `build.outDir` to write into the Go package's directory. Register the output in `.gitignore` and add a `dist/.gitkeep` so `go build` doesn't fail on a clean checkout before `vite build` has run.
- **⚠ `http.FileServer` does not compress.** [measured] No `Content-Encoding` on any response above. Given that your schema payloads compress ~18:1 and your JS ~3:1, this is the single highest-leverage line of server code you will write. Either add gzip/brotli middleware, or pre-compress at build time and serve `.br`/`.gz` on `Accept-Encoding` match. Do not skip this.

### Reference repos

- **`crossplane-contrib/crossview`** — **the reference for this project.** Apache-2.0, v4.5.0. Its root `package.json` `start` script is literally `vite build && cd crossview-go-server && go run main.go app:serve`. Go 1.25, `k8s.io/client-go` v0.34.3. Front-end stack: `@xyflow/react` ^12.9.3, `@uiw/react-codemirror` ^4.25.7 + `@codemirror/lang-yaml`, `yaml` ^2.8.2, Chakra UI v3, `vite` ^7.2.6, React 18. This independently corroborates React Flow + CodeMirror 6 + Vite + Go. Its backend is heavier than you need (gin + cobra + viper + uber/fx + gorm with Postgres and SQLite drivers) — take the wiring, not the dependency list.
- **[docs]** [`danhawkins/go-vite-react-example`](https://github.com/danhawkins/go-vite-react-example) — the canonical small example. Its useful idea is the **dev-mode proxy**: in production serve from `embed.FS`; in development have Go proxy everything except `/api/*` to Vite's dev server on `:5173`, preserving HMR while keeping one origin and no CORS. Implement that same split with `httputil.NewSingleHostReverseProxy` behind a build tag or env var.

---

## 5. State and undo

**Recommended: `zustand` 5.0.15 + `immer` 11.1.18, with a bounded snapshot stack. No y.js.**

React Flow ships its own internal zustand store for viewport/selection/drag transients. **Do not fight it.** Split the state:

- **React Flow's store** owns viewport, drag positions, selection — ephemeral, high-frequency, never in undo history.
- **Your zustand store** owns the *document*: which managed resources exist, their field values, the reference edges, XRD spec. This is the only thing undo touches.

Undo model, in preference order:

1. **Snapshot stack (do this).** With immer, every mutation produces a new immutable document tree that structurally shares everything unchanged. Push the previous root onto a bounded stack (50–100 entries). [measured] A full EC2 CRD schema set parses to only **6.1 MB retained heap**; a *document* is orders of magnitude smaller. Snapshots are effectively free and the implementation is a dozen lines. `immer`'s `produce` is already in the stack.
2. **immer patches (`enablePatches`) — only if you need it.** `produceWithPatches` gives you inverse patches for free, which is more memory-efficient and gives you a serializable edit log. Worth it only if you later want to show an edit history or replay. Not day one.

**Two rules that prevent the classic bugs:**
- **Coalesce.** A drag emits a position change per frame; a text field emits per keystroke. Debounce commits to the undo stack (~300–500 ms) or group by interaction, or Ctrl+Z will undo one character.
- **Node positions are document state, not transients.** Users expect undo to restore layout. Mirror React Flow's positions into your document on drag *end* only, never on drag *move*.

### Collaboration is YAGNI — and the cost is architectural

`yjs` 13.6.32 is excellent and MIT. Skip it anyway.

The bundle size is not the argument. The argument is that **CRDTs and undo stacks are different architectures, and retrofitting is easy while pre-fitting is expensive.** Adopting y.js now means:
- your document must live in `Y.Map`/`Y.Array`, not plain immer-managed objects — so you lose `produce` and the free snapshot undo, and instead adopt `Y.UndoManager` with its origin-tracking semantics;
- you need a server-side awareness/sync transport (WebSocket), which turns a single stateless Go binary into a stateful one;
- every rjsf `onChange` must be translated into Y-type mutations rather than a plain state set.

That is a large tax paid up front against a hypothesis. And the counter-hypothesis is strong: **this tool authors a file that then goes into Git.** Git *is* the collaboration model for Compositions and XRDs. The user's cluster already runs Argo CD and Kargo — the collaboration story is a pull request, not a shared cursor. If multi-user editing ever becomes real, the migration is bounded because your document is a plain serializable tree, which is exactly what `Y.Map` wants to be seeded from.

**Do not build a WebSocket for collaboration. Do consider one later for live cluster watch** (streaming managed-resource status onto the canvas) — a genuinely different and more valuable feature, and one the existing tools (crossview, Komoplane) already validate demand for.

---

## 6. Bundle size and the schema-loading strategy

### What the app weighs [measured]

Full recommended stack in one eager bundle — React 19.2.8 + `@xyflow/react` 12.11.5 + `@rjsf/core` 6.8.0 + `validator-ajv8` + CodeMirror 6 + `zustand` + `immer` + `@dagrejs/dagre` + `yaml`, with a custom node type whose body is a live rjsf form:

```
JS   raw = 1,339,161   gzip = 425,988   brotli = 359,534
CSS  raw =    15,413   gzip =   2,536
TOTAL gzip ≈ 428.5 KB   |   brotli ≈ 362 KB
```

With trivial `React.lazy` code-splitting — canvas eager, inspector and editor lazy:

| Chunk | raw | gzip |
|---|---|---|
| **eager (React + React Flow + zustand + immer)** | 380,708 | **120,092** |
| `Inspector` (rjsf + ajv8) lazy | 390,938 | 125,795 |
| `Editor` (CM6 + lang-yaml + lint) lazy | 423,787 | 135,648 |

**First paint at 120 KB gzip.** The inspector loads on first node click; the editor on first YAML-panel open. Both are sub-150 KB and land during a user pause. Take this split — it is three `React.lazy` calls.

For contrast, the same app with minimal Monaco instead of CM6 would be ~**1.2 MB gzip**, and with naive Monaco ~**3.4 MB gzip**.

### What CRD schemas would do to it [measured]

All 204 EC2 CRDs, four payload tiers:

| Tier | raw | gzip | brotli |
|---|---|---|---|
| Full `openAPIV3Schema` × 204 | 4,275,487 | 238,371 | **53,680** |
| Descriptions stripped | 1,226,557 | 37,479 | **15,219** |
| `spec.forProvider` only, no descriptions | 364,287 | 17,981 | **7,848** |
| Index only (group/kind/version/prop-count) | 15,215 | 1,332 | **1,022** |

Per-CRD, descriptions stripped: max `LaunchTemplate` **34,083**, median **4,565**, min **1,421** bytes.

Cost to consume the largest tier: [measured] `JSON.parse` of the full 4.27 MB = **9.4 ms median** (5 runs: 11.7, 10.8, 9.4, 9.4, 9.3), retaining **6.1 MB** of heap.

**The surprising conclusion: bundling would actually be fine on every metric except the one that matters.** 54 KB brotli for an entire provider family and a 9 ms parse are nothing. The reasons to serve from the API anyway are structural:

- **N providers, not one.** EC2 is *one member* of the `provider-family-aws`. Ship a tool that bundles schemas and you must rebuild and re-release the binary every time any provider publishes a version. That is the wrong coupling for a tool whose whole premise is "ANY Crossplane provider."
- **You already have the authoritative source.** The Go backend has `client-go` and a live cluster. `kubectl get crd` *is* the schema API. Reading from the cluster means the tool always matches what is actually installed — including the `.m.` vs legacy group split and per-CRD version differences that a bundled snapshot would get wrong.
- **Descriptions are 71% of the payload and you want them.** They are the field help text in the form. Serve them per-CRD on demand rather than choosing between bloat and dropping them.

### Recommended loading strategy

1. **Eager, from the Go API at startup:** the **index tier** — group/kind/version/scope/property-count for every CRD, filtered to `.m.` groups. [measured] **1.0 KB brotli** for 204 CRDs. Powers the resource palette and search instantly.
2. **On demand, per node type:** the single CRD's full schema when the user drops that resource or opens its form. [measured] median **4.5 KB** raw, ~1–2 KB compressed. Cache in a `Map` in the zustand store; a session touches maybe 5–20 kinds.
3. **Never ship schemas in the JS bundle.** Serve `GET /api/crds` and `GET /api/crds/{group}/{version}/{kind}/schema` from Go, reading through `client-go` with an informer-backed cache.
4. **Turn on compression in Go** (see §4). With an 18:1 ratio on schema JSON this is the difference between a 34 KB and a 2 KB response.
5. **`ETag` + `If-None-Match` per CRD**, keyed on the CRD's `metadata.resourceVersion`. Schemas change only on provider upgrade, so nearly every request after the first becomes a 304.

---

## Recommended stack

```jsonc
{
  "@xyflow/react":            "12.11.5",  // MIT — canvas
  "react":                    "19.2.8",
  "react-dom":                "19.2.8",
  "@rjsf/core":               "6.8.0",    // Apache-2.0 — schema forms
  "@rjsf/utils":              "6.8.0",
  "@rjsf/validator-ajv8":     "6.8.0",
  "codemirror":               "6.0.2",    // MIT — editor
  "@codemirror/lang-yaml":    "6.1.3",
  "@codemirror/lint":         "6.9.7",
  "@codemirror/autocomplete": "6.20.3",
  "yaml-language-server":     "1.24.0",   // MIT — in a Web Worker
  "yaml":                     "2.x",      // eemeli/yaml — AST w/ positions
  "zustand":                  "5.0.15",   // MIT — document state
  "immer":                    "11.1.18",  // MIT — immutable updates + undo
  "@dagrejs/dagre":           "3.1.1",    // MIT — auto-layout
  "vite":                     "8.2.2"     // MIT — build
}
```
Backend: **Go 1.25+**, `net/http` + `embed.FS` (stdlib is enough — `crossview` uses gin, but `http.ServeMux` in Go 1.22+ has method/pattern routing), `k8s.io/client-go` for CRD discovery, plus gzip/brotli middleware.

Deliberately excluded: JSONForms (silently renders nothing for maps/`oneOf`/nesting), uniforms (stale), Monaco (5.8× CM6 for features you won't use), Cytoscape/LiteGraph (canvas — cannot host forms), elkjs (466 KB gzip + EPL/GPL for a 15-node DAG), y.js (YAGNI, and architecturally costly to pre-fit).

## Top 2 risks

**1. The reference-wiring layer is the actual product, and none of these libraries help with it.**
Everything above is commodity. The hard, unsolved part is semantic: knowing that `Queue.spec.forProvider.redrivePolicy` should wire to another `Queue`, that Upbound's convention is a `fooIdRef` / `fooIdSelector` sibling pair next to `fooId`, and that a valid edge must emit a *correct Go-template expression* into the Composition. [measured] The CRD schema gives you almost nothing to work from: SQS `Queue` has **zero** `x-kubernetes-validations`, zero `oneOf`, and the only vendor hints are 4 `x-kubernetes-map-type` and 1 `x-kubernetes-list-type` — none of which encode cross-resource relationships. You will be inferring references from **naming conventions** (`*Ref`, `*Selector`, `*IdRef`) that are an Upbound/crossplane-runtime convention, not a spec, and that other providers may not follow. `isValidConnection` gives you the hook; it cannot give you the rules. **Mitigation:** build the reference inference as an explicit, testable, data-driven layer in Go (not scattered through React), seed it from the convention, allow per-provider overrides, and always let the user override an edge by hand. Prototype this against a second, non-Upbound provider early — before committing to the convention — or you will hard-code Upbound's naming into your core.

**2. Round-tripping generated YAML, and the Go-template/schema impedance mismatch.**
Two coupled failure modes. First, users will hand-edit the generated Composition and expect the graph to reflect it; a lossy generate-only tool gets abandoned. Parsing an arbitrary `function-go-templating` Composition *back* into a graph is materially harder than emitting one, because the inline template is a Turing-complete program, not data. Second, `x-kubernetes-int-or-string` and `x-kubernetes-preserve-unknown-fields` fields, plus **any** field a user wants to fill with `{{ .observed.composite.resource.spec.foo }}`, break the schema/form contract — rjsf will insist the field is an integer while the user needs to put a template string there. [measured] rjsf does not crash on these constructs, but "does not crash" is not "produces the right editing affordance." **Mitigation:** declare round-tripping a non-goal in v1 and say so loudly — generate-only, with the graph as the source of truth persisted in its own sidecar document, and regeneration overwriting the Composition. Use `eemeli/yaml`'s AST with source positions (not `js-yaml`) so you retain comments and can do surgical edits later. Design the **template escape hatch as a first-class per-field mode toggle in the rjsf custom widget from day one** — retrofitting it after the form layer is built means touching every widget.

---

### Scratch artifacts (all under ``)

- `bundletest/` — Vite harness used for every bundle measurement; `rtest/render.mjs`, `rtest/edge.mjs`, `rtest/jf2.mjs` are the rjsf/JSONForms comparison scripts; `measure2.mjs` computes the schema payload tiers; `parseperf.mjs` the parse/heap benchmark
- `xpkg/ec2.yaml` — 8.56 MB, 204 CRDs, extracted via `crossplane xpkg extract xpkg.upbound.io/upbound/provider-aws-ec2:v2`
- `goembed/` — the verified `embed.FS` + SPA-fallback server (`web/embed.go`, `main.go`)

### Explicitly unverified
React Flow node-count limits (not benchmarked headlessly); a working CM6 `parseMixed` YAML+Go-template grammar (not built); Backstage's current rjsf version; whether Headlamp stress-tested rjsf against deep CRDs (their issue does not say).

Sources: [xyflow #2015](https://github.com/xyflow/xyflow/discussions/2015) · [Remove Attribution](https://reactflow.dev/learn/troubleshooting/remove-attribution) · [React Flow performance](https://reactflow.dev/learn/advanced-use/performance) · [Headlamp #2087](https://github.com/kubernetes-sigs/headlamp/issues/2087) · [crossplane-contrib/crossview](https://github.com/crossplane-contrib/crossview) · [danhawkins/go-vite-react-example](https://github.com/danhawkins/go-vite-react-example) · [yaml-language-server](https://github.com/redhat-developer/yaml-language-server) · [cytoscape-dom-node](https://github.com/rkatka/cytoscape-dom-node)