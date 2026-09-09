# Code-hygiene audit — compositionfactory at f45c2a8 (2026-09-09)

Read-only. Nothing in the repo was edited; no ports touched; `bin/cf` not rebuilt.
Tools run: `go vet`, `staticcheck@v0.8.1 ./...`, `deadcode@latest` (with and without `-test`),
`npx eslint web-proto/js/`, `git branch/worktree/rev-list`, `wc`, `grep`, awk function-span scan.

## 0. Static gates and previous-audit status

```
go vet $(go list ./... | grep -v /node_modules/)   -> VET_EXIT=0
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./... -> SC_EXIT=0
npx eslint web-proto/js/                             -> ESLINT_EXIT=0
```

Prior (2026-09-02) findings, re-checked at HEAD:

| Prior finding | Status at f45c2a8 | Evidence |
|---|---|---|
| Index-rebuild duplication across add/delete/cluster-sync | **Resolved.** One `rebuildIndexLocked` (internal/api/server.go:542) called from crdsource.go:81, blueprint.go:172/180/610/634, cluster.go:121, providers.go:210/328 | `grep -rn rebuildIndex` |
| `rebuildIndexLocked` result swallowed on rollback paths | **Still holds** — blueprint.go:172, :180, :634 (`_ = srv.rebuildIndexLocked()`) | §5 E1 |
| KCL/Python emitters re-parse gotpl text | **Reduced, not gone.** Emitters consume `structured.go` RHS values, but kcl.go:276-277 and python.go:267-268 still string-strip `{{ … }}` from an RHS that structured.go:194-348 just formatted with `fmt.Sprintf("{{ %s }}")` — a format-then-unformat round trip inside one package | `grep -nE '\{\{' internal/emit/kcl.go python.go structured.go` |
| docs/superpowers spec/M3 plan stale | **Still holds.** `docs/superpowers/plans/2026-08-28-m3-canvas.md` marked "Superseded 2026-09-01" at :4 yet still 700+ lines of React/xyflow plan; dir untouched since 2026-09-02 | §3 D12 |
| docs/mcp.md parity claim wrong | **Resolved.** 19 tools in docs/mcp.md = 19 `Name:` in internal/mcp/tools.go; 35 routes in the table = 35 `mux.HandleFunc` in server.go:172-206; `TestListToolsAdvertisesTheFullOperationSet` (mcp/server_test.go:238) pins the set | counted |
| Dead `web/` tree | **Resolved.** `git ls-files web | wc -l` = 0. One remnant: Makefile `clean` still `rm -rf … web/dist` | §3 D8 |
| `/api/kinds` refetched on every doc emit | **Resolved**, by copy-paste: the same 4-line sources-signature guard now lives in inspector.js:2384-2388, palette.js:1091-1095, canvas.js:1903-1907 | §6 W2 |
| PUT persists a doc whose source fetch failed | **Still holds** — blueprint.go:556-560 | §5 E2 |

---

## 1. Duplicated logic across the three doors and between web-proto regions

MCP is genuinely a thin bridge (internal/mcp/server.go:7-20; every tool is `s.bridge(http.Method…, "/api/…")` in tools.go:318-661), so CLI-vs-API is where the duplication is.

**G1. "Already cached → pin the lock" written three times, with three different error policies.**
- `internal/cache/store.go:366-406` `Store.FetchAndSave` — fetch, parse, `ReadLock → l.Set → l.Write`, then `Save`; every error returned.
- `internal/api/providers.go:170-181` (`handleAddProvider`) — `Store.Load` → `LoadDigest` → `ReadLock` → `Set` → `_ = l.Write(srv.Lock)` — write failure **discarded**.
- `internal/api/blueprint.go:533-552` (`syncBlueprintSourcesLocked`) — identical `Load → LoadDigest → ReadLock → Set → Write` sequence, but `return fmt.Errorf("write lock: %w", err)`.
Same operation; whether a failed pin is an error depends on which door you came through.

**G2. `fetch := srv.fetch; if fetch == nil { … xpkg.Fetch(ctx, ref) }` triplicated** at blueprint.go:524-528, functions.go:68-72, providers.go:185-189 — while `Store.FetchAndSave` already applies exactly that default at store.go:367-371. Two of the three callers pass the wrapper straight into `FetchAndSave`.

**G3. Provider-set assembly (blueprint sources → dedupe → cluster label → BuildIndex) exists in four places with divergent behaviour.**
- `cmd/cf/options.go:29-58` `buildAPIOptions` (serve + mcp): warns on a source missing from cache (`cf: warning: … continuing without it`), adds `cluster.ProviderLabel`.
- `cmd/cf/kinds.go:24-47` and `cmd/cf/fields.go:27-49`: near-identical to each other (`os.Stat → blueprint.Load → collect s.Provider → if empty, store.List() → api.BuildIndex`); **no** warning on a missing cache entry, **no** cluster label, and a `blueprint.Load` error is silently treated as "no blueprint" (kinds.go:30 `if loaded, err := blueprint.Load(...); err == nil`).
- `internal/api/blueprint.go:588-608` re-implements the cluster-label reconciliation for the runtime path.
Consequence: `cf kinds` and the canvas served from the same `doc.cf.yaml` can disagree about which kinds exist (cluster CRDs appear only in the latter).

**G4. Provider-vs-function package classification loop duplicated four times** with mirrored error strings:
- `cmd/cf/provider.go:41-52` ("is a function package, not a provider (use 'cf function add …')")
- `cmd/cf/function.go:33-45` ("is a provider package, not a function (use 'cf provider add …')")
- `internal/api/functions.go:80-92` (same string as function.go)
- `internal/api/functions.go:36` (list handler, same predicate `IsFunctionInput() || crd.Function`)
`POST /api/providers` (providers.go:130-165) has no such check, so the CLI refuses a function package as a provider but the canvas/MCP accept it.

**G5. The crossplane render pipeline is implemented twice.**
- `cmd/cf/gen.go:96-147` (`--validate`): `exec.LookPath("crossplane")` → temp dir → `emit.Generate` → classify outputs by parent dir (`compositions`/`xrds`/`functions.yaml`) → `emit.SampleXR` → `exec.Command("crossplane","composition","render", xr, comp, fns, "--xrd", xrd, "--timeout","5m")` → `emit.ValidateRendered`.
- `internal/api/render.go:73-110` + `runCrossplaneRender` at :203-207: the same sequence, same classification (comment at render.go:97-99 even says "rather than re-deriving file names here"), same `--timeout 5m`.
MCP `render_check` bridges HTTP, so this is two copies, not three — but the CLI copy lacks the Docker-unavailable classification render.go:212-230 performs, so `cf gen --validate` reports "render failed: Cannot connect to the Docker daemon" as a composition error.

**G6. web-proto helpers duplicated across regions** (bodies compared):
- `slug`: canvas.js:63 and palette.js:649 — byte-identical.
- `uniqueResourceName`: canvas.js:873 and palette.js:653 — identical except palette's null-guard on `d.spec`.
- `mapResourceCoordinates`: inspector.js:311 (**exported**) and output.js:395 — identical; output.js re-declares instead of importing.
- `famOf`: canvas.js:44 maps *provider string* → `k8s|azure|gcp|helm|aws`; palette.js:79 maps *kind* → `cluster|aws|k8s` (`/(^|[^a-z])aws|upbound\.io/`). Not a copy — two disagreeing classifiers: a `provider-azure-*` resource is "azure" on its card and "k8s" in the palette; a `crossplane-contrib/provider-gcp` kind is "k8s" in the palette (no `upbound.io`) and "gcp" on the card.
- `kindMeta` (doc resource → `/api/kinds` entry): canvas.js:80 is sync and prefers `namespaced`; inspector.js:152 is async, takes the first match, and re-fetches on miss. Same resource can resolve to different apiVersions in the two regions.
- sources-signature invalidation: inspector.js:2384-2388, palette.js:1091-1095, canvas.js:1903-1907 — identical 4 lines each.

---

## 2. Dead code, dead files, dead branches

**X1. `internal/manifest` — a whole package with no importer.**
```
grep -rln 'internal/manifest"' --include='*.go' .   -> (no output)
```
6 files / 808 lines (dialect.go, parse.go, patch.go, scrub.go, types.go, manifest_test.go). Added in 87fdcb6 "…manifest spike…", last touched b20f51e (2026-09-03). deadcode (no `-test`) lists every entry point: parse.go:14 `ParseComposition`, :81 `extractTemplate`, :108 `parseTemplateBody`, :161 `parseResourceDoc`, patch.go:18 `Apply`, :56 `SetFieldLiteral`, :73 `SetFieldWire`, :97 `DeleteResource` (unreachable **even with `-test`**), scrub.go:25 `ScrubKubectlExport`, :64 `scrubMetadataNode`. `docs/research/2026-09-02-cf-dialect-specification.md:3` still calls itself "authoritative reference … Applies to: internal/emit, internal/manifest". Decide: delete, or move to a branch and drop the "authoritative" label.

**X2. `internal/schema/tree.go:167 RequiredLeaves` + :173 `appendRequiredLeaves`** — referenced only from `tree_test.go:485`. Production uses `internal/index/fields.go:123 filterRequiredOnly` instead. deadcode: `internal/schema/tree.go:167:6: unreachable func: RequiredLeaves`.

**X3. `cmd/cf/serve.go:85 defaults`** — test-only seam (serve_test.go:18, :45) living in a production file; deadcode flags it. Move to `serve_test.go` or add it to the Non-findings list.

**X4. Go code under `node_modules` is inside the module.**
```
go list ./... | grep node_modules
github.com/koorikla/compositionfactory/node_modules/flatted/golang/pkg/flatted
```
`make test`/`test-race`/`lint` filter it (`grep -v /node_modules/`), but `lint-strict` (`staticcheck ./...`, Makefile:63) and `test-docker` (`go test ./... -run Acceptance`, Makefile:20) do not, and deadcode reports 7 hits from it. Filter consistently.

**X5. Branches (ahead/behind main, `git rev-list --count`):**

| Branch | ahead | behind | last commit | worktree | verdict |
|---|---|---|---|---|---|
| `canvas-parity` (local) | 0 | 423 | 2026-08-28 | /Users/kaurkallas/compositionfactory-parity | fully merged; delete branch + worktree |
| `engine-mvp` (local) | 0 | 419 | 2026-08-28 | /Users/kaurkallas/compositionfactory-engine | fully merged; delete |
| `subagent-Design-Token-Engineer-task-worker-74edcb7c` (local) | 0 | 4 | 2026-09-09 | ~/.gemini/…/worktrees/… | merged (CF-071 landed as 12597ef/3a17618); prune |
| `wip-2026-09-09-uncommitted` (local + origin) | 1 | 72 | 2026-09-09 | — | 63 files / +3331 −247 "snapshot of the uncommitted working tree" touching inspector.js, output.js, palette.js, store.js; needs triage — cherry-pick or delete, do not leave a pushed WIP snapshot |
| `origin/claude/vigilant-hugle-ef66f7` | 0 | 122 | 2026-09-03 | — | merged; delete remote |
| `origin/worktree-emitter-guards-correctness` | 0 | 196 | 2026-09-02 | — | merged; delete remote |
| `origin/worktree-fs-export-v2` | 0 | 225 | 2026-09-02 | — | merged; delete remote |
| `origin/CF-049-comp-tester-findings` | 2 | 73 | 2026-09-04 | — | only BACKLOG.md (+132) and docs/code-audit.md (+23); BACKLOG.md:60 already declares it "superseded" — delete |
| `origin/catalogue/weekly-refresh-2026-09-07` | 1 | 73 | 2026-09-07 | — | bot refresh, 1-line providers.json change; merge or close |

`.worktrees/cf-audit` (f45c2a8, created 17:34 today, untracked `tests/cf086-sources-tab-tracks-doc.spec.js`) and the scratchpad `journeys/j4/wt` are live concurrent-agent worktrees, not stale.

**X6. Makefile:73 `clean: rm -rf … web/dist`** — `web/` no longer exists.

**X7. `docs/superpowers/`** — plans/ (3 files) and specs/ (2 files) referenced only from docs/research/*.md and from each other; M3 plan self-declared superseded (m3-canvas.md:4) but retains the full React/Vite/MSW task list (:10-12, :208-210, :433-712).

**X8. Unused/unnecessary web-proto exports:** `dom.js:36 qsa` — zero references anywhere; `wires.js:108 fanOutMap` and `main.js:55 showErrorToast` — used only inside their own module; `output.js:1274 toYaml` — only output.js:273 uses it.

---

## 3. Docs drift (quote both sides)

**D1. CONTRIBUTING.md §3 "Port Allocation Contract"**
> Port 8081: Automated Playwright test runner (`make test-e2e`). / Port 8086: Headless demo recorder

vs AGENTS.md §2 "Dynamic Worktree Port (18000–27999) … Dynamic Demo Port (28000–37999)", playwright.config.js:13 `18000 + (parseInt(hash, 16) % 10000)`, scripts/record-demos/run.sh:8 `PORT=${CF_DEMO_PORT:-$((28000 + PORT_OFFSET))}`.

**D2. docs/record-demos.md:10**
> starts `cf serve` on an isolated port (`8086`)

vs run.sh:8 above.

**D3. Go version.** README.md:242 "Requires Go 1.25+", CONTRIBUTING.md:11 "Go: Version 1.25 or later" vs go.mod:3 `go 1.27.0`, ci.yml `go-version: "1.27"` (×4), Dockerfile:6 `golang:1.27-alpine`.

**D4. CONTRIBUTING.md §2**
> `cmd/cf`: … (`gen`, `serve`, `package`, `push`, `adopt`, `provider`, `mcp`, `version`)

vs cmd/cf/main.go:18-32 which also registers `Init`, `Function`, `Kinds`, `Fields`, `Catalogue`, and `Adopt … aliases:"import"`.

**D5. Non-existent path `blueprints/xqueue.cf.yaml`** in web-proto/README.md:11 (`cf serve --blueprint blueprints/xqueue.cf.yaml`) and docs/mcp.md:26 and :36. `ls blueprints` → "No such file or directory"; the file is `testdata/xqueue.cf.yaml` (Makefile:5 `BLUEPRINT ?= testdata/xqueue.cf.yaml`).

**D6. web-proto/README.md:21**
> `js/store.js` single state container `{doc, selectedResource, positions, lastGenerate}` … **frozen contract, do not edit**

vs store.js:55-62 state also has `undoStack`, `redoStack`; api.js ("frozen contract, do not edit", README:20) has since gained `previewExpression`, `loadExample`, `addCRDSource`, `adoptComposition`, `syncCluster`… (api.js:285-349). The "frozen" labels are process residue from the region-agent build and now mislead.

**D7. `make lint` description omits eslint.** AGENTS.md §3 "`make lint`: Code formatting verification (`gofmt`…) and Go vet analysis"; README.md:248 and CONTRIBUTING.md:38 "make lint # Check formatting and vet"; vs Makefile:58-61 `lint:` also runs `npm run lint:js`. README:242 says Node is needed only "for Playwright e2e tests" — `make lint` fails without `npm ci`.

**D8. Makefile:73** `clean: rm -rf … web/dist` — see X6.

**D9. CHANGELOG.md vs tags.**
```
git tag … | head -1  -> v0.10.0 2026-09-09 f45c2a8
grep -n '0\.10' CHANGELOG.md -> (no output)
```
HEAD is tagged v0.10.0 but CHANGELOG has no `## [0.10.0]` section; `[Unreleased]` (:16-34) still lists `cf init`, `spec.environment`, `cf function`, `POST /api/preview-expression`. Also :28-31 "`BACKLOG.md` records 44 defects … 859 Go tests" — BACKLOG.md now contains zero items and there are 943 `func Test*` declarations.

**D10. BACKLOG.md is 90 lines of scaffolding with no items.** Three "## Open — …" headers (:52, :62, :70) each followed by nothing; :72-74 explains a CF-049→CF-073 renumbering for a section that has no entries; "Non-findings" (:82-90) lives here rather than in docs/backlog-archive.md (which has no such section: `grep -n -i non-finding docs/backlog-archive.md` → nothing). AGENTS.md §4 says this file "is read into every agent's context, so its length is a running cost".

**D11. docs/cli.md `cf adopt`** documents `<composition.yaml>` only; adopt.go:14 accepts "Composition YAML file **or Configuration directory** (or - for stdin)" and main.go:28 registers the `import` alias — neither is in cli.md. `--file` alias for `cf serve --blueprint` is in README:99 but not in cli.md:181.

**D12. docs/superpowers** — see X7; docs/research/2026-09-02-cf-dialect-specification.md:3 "Status: authoritative reference" for a package with no importers (X1).

Checked and **not** drifted (so nobody re-raises them): docs/cli.md flag names for gen/serve/kinds/fields/catalogue/package/function/mcp all match cmd/cf/*.go tags; docs/mcp.md tool and route tables match code; README's "476 OSS providers" = `len(catalogue/providers.json)` = 476; `#region-*`/`#cw` ids in web-proto/README match index.html.

---

## 4. Test-suite health

**Counts.** 79 Playwright spec files, 225 `test(` blocks (`tests/`); 943 Go `func Test|Benchmark|Fuzz` declarations (incl. scripts/build-catalogue); root `acceptance_test.go` is 1650 lines in `package main_test`.

**T1. The e2e engine uses the developer's real schema cache.** playwright.config.js:27 webServer command:
```
./bin/cf serve --addr 127.0.0.1:${port} --blueprint ${scratchDir}/doc.cf.yaml --out ${scratchDir}/out --lock ${scratchDir}/.cf.lock
```
No `--cache-dir` (`grep -n cache-dir playwright.config.js tests/helpers.js ci.yml` → nothing), so `cf serve` defaults to `${cachedir}` = the OS cache. slice17-catalogue.spec.js:26 documents the consequence: `test.skip(have.providers.some(p => p.ref.includes(REF_HINT)), 'nop already cached from a prior run')`; slice16-provider-remove.spec.js:29 `test.skip(!(await s3row.count()), 's3 provider not cached')`. Both tests are order/state-dependent on the host, and `make test-e2e` writes provider-nop into the human's `~/Library/Caches/compositionfactory`. helpers.js:1-4 claims "Isolation is structural, not manners" — for the doc and port, yes; for the cache, no. Fix: add `--cache-dir ${scratchDir}/cache` to the webServer command (1 line).

**T2. Skips.** Go: acceptance_test.go:67 (env gate), internal/api/render_test.go:372-412 (Docker/crossplane gates) — legitimate. Playwright: helpers.js:33 (engine down), slice16:29, slice17:26 (T1). No `fixme`.

**T3. Timing sleeps.** slice92-validate-vocabulary.spec.js:23 `await new Promise((r) => setTimeout(r, 200))` inside a poll loop and :56 `await page.waitForTimeout(600)`; nothing else. No `time.Sleep` in Go tests.

**T4. Real-render tests overlap.** Nine tests set `test.setTimeout(90000)` and click `#validateBtn` for a real `crossplane composition render` (slice3:11,:18; slice92:10,:36; slice83:9,:53; slice89:62; slice29:26; plus slice13/slice65). slice3:11 ("Validate renders … reports the resource count"), slice92:10 ("clicking Validate displays validating… then valid · N resources") and slice89:62 assert the same regex on `#valid` (`/(?:valid|validate ok) · \d+ resources?|validation check unavailable/`) — three renders to prove one behaviour. Only one needs Docker; the others could `page.route('/api/render', …)`.

**T5. Duplicate spec.** slice67-theme-native-controls.spec.js:163 "token definitions in proto.css and canvas-prototype.html remain in sync" and slice91-output-drawer-sizing.spec.js:65 "token definitions in proto.css and canvas-prototype.html remain in sync (zero token drift)" — both read `docs/design/canvas-prototype.html` and `proto.css` and diff the token sets.

**T6. Helper/style sprawl.** tests/helpers.js exports via three separate statements (:39, :58, :152-154). slice84-generate-overwrite-confirmation.spec.js:1 uses ESM `import { test, expect } from '@playwright/test'` while the other 78 specs use `require` in a `"type": "commonjs"` package; slice84 also drives the page without `guardPageErrors()` (only it and the request-only slice22 omit it) and without `resetDoc`. Its describe title "CF-057" refers to an id BACKLOG.md:72 says was renumbered to CF-081.

---

## 5. Error-handling sites that swallow errors (file:line → what the user sees)

**E1. internal/api/blueprint.go:172, :180, :634** `_ = srv.rebuildIndexLocked()` on rollback after `srv.Providers = origProviders`. `rebuildIndexLocked` assigns `srv.Index` only on success (server.go:565-570), so on failure the index still reflects the *un-rolled-back* provider set. User sees the original 400/500; `/api/kinds` afterwards may list kinds of a provider the server no longer thinks it serves.

**E2. internal/api/blueprint.go:556-560**
```go
pkg, err := fetch(ref)
if err != nil {
    fmt.Fprintf(os.Stderr, "cf: warning: unable to fetch source %q: %v — continuing offline\n", ref, err)
    continue
}
```
`PUT /api/blueprint` then persists the doc (blueprint.go:176) and returns 200. The canvas shows the new source row (palette falls back to doc sources, palette.js:178) with zero kinds; nothing in the UI says the fetch failed. Same path via MCP `replace_blueprint`.

**E3. internal/api/server.go:483-486** — a `crds:` source whose file is missing: `if os.IsNotExist(err) { continue }`. Kinds from that file silently vanish from the index; PUT/serve succeed; no stderr line either.

**E4. cmd/cf/options.go:49-50**
```go
if clusterCRDs, err := cl.FetchCRDs(context.Background()); err == nil && len(clusterCRDs) > 0 {
    _ = store.SaveCRDs(cluster.ProviderLabel, cl.Context(), clusterCRDs)
```
`cf serve --cluster` against an unreachable cluster starts with no cluster kinds and prints nothing; a failed cache save is likewise silent. Contrast `POST /api/cluster/sync` (api/cluster.go:96-106) which returns 502/500 for the same two failures.

**E5. internal/api/providers.go:178** `_ = l.Write(srv.Lock)` — lockfile pin failure ignored; `POST /api/providers` returns 200 and `.cf.lock` lacks the digest, silently breaking the AGENTS.md §1 reproducibility guarantee. The same operation in blueprint.go:547-550 returns `write lock: …`. (Also providers.go:168 `pkgDigest, _ = srv.Store.LoadDigest(req.Ref)` — benign, falls through to fetch.)

**E6. cmd/cf/gen.go:198** `existingFiles, _ := c.findExistingManagedFiles()` followed by :199-203 `os.Remove(path)` for every file not in `expected`. With `-o <dir>` other than `.`, `findExistingManagedFiles` (gen.go:250) walks the **entire** `<dir>` — so `cf gen doc.cf.yaml -o ~/gitops/platform` deletes any hand-written file there (a `kustomization.yaml`, a README). Output is one `removed …` line per file. gen_test.go:394-395 pins this for `orphaned.yaml`, so it is designed — but docs/cli.md:118 describes `-o` only as "Output directory (defaults to `.`)" and README:150 shows `-o out`; neither says the directory is owned and pruned.

**E7. internal/cache/store.go:434-438** `Store.List()`: unreadable or corrupt `crds.json` → `continue`. `cf kinds` falls back to `store.List()` (kinds.go:43 `cached, _ := store.List()`), so a corrupt provider entry disappears with no message.

**E8. cmd/cf/kinds.go:29-31 / fields.go:32-34** `if loaded, err := blueprint.Load(c.Blueprint); err == nil { … }` — an invalid `doc.cf.yaml` degrades `cf kinds` to "all cached providers" silently, while `cf gen` on the same file fails loudly.

**E9. internal/blueprint/load.go:390** `if err != nil || json.Unmarshal(...) != nil || meta.Kind != "Configuration" { continue }` — a YAML→JSON failure inside a package stream is folded into the later "Configuration package has no … annotation" error, masking the real parse error.

**E10. cmd/cf/gen.go:207, :233, :250** `_ = os.Remove(dir)` / `_ = filepath.Walk(...)` — directory-prune and walk errors ignored; benign (best-effort cleanup) but the Walk error masks E6's scope.

---

## 6. web-proto

**W1. Largest functions** (awk span between consecutive top-level `function` declarations; verified no nested top-level declarations inside the first):

| lines | location | function |
|---|---|---|
| 413 | inspector.js:1599 | `commitEnvelopeValue` (runs to :2011) |
| 360 | palette.js:720 | `bindPaletteEvents` |
| 244 | inspector.js:2081 | `onBoxChange` |
| 192 | inspector.js:928 | `renderResource` |
| 185 | inspector.js:1167 | `paramDetailRow` |
| 185 | canvas.js:175 | `resourceCardHTML` |
| 171 | output.js:836 | `bindOutputEvents` |
| 166 | canvas.js:1205 | `buildFieldPickerCandidates` |
| 153 | palette.js:340 | `drawSources` |

**W2. Duplicated state.**
- `/api/kinds` is cached three times with three invalidation policies: palette.js:58 `kinds` (reloaded on search input :741, on every add/remove :758/:819/:834/:871, on sources-sig change :1095), inspector.js:61 `kindsPromise` (nulled on sources-sig :2388 and on any miss :160), canvas.js:30 `kindsCache` (reloaded on sources-sig :1905). The store (store.js:55-62) holds none of it.
- `/api/kinds/{v}/{k}/fields` cached twice: inspector.js:62 `fieldsCache`, canvas.js:31 `schemaCache` (+ `schemaLoading`).
- `providers` list lives only in palette.js:58 and is reset to `null` on every add/remove (:755) — not in the store, so inspector/canvas cannot see provider digests/kind counts.
- The sources-signature computation is copy-pasted ×3 (G6).

**W3. Unused/needless exports.** `dom.js:36 qsa` (0 refs); `wires.js:108 fanOutMap` (only wires.js:133); `main.js:55 showErrorToast` (only main.js:78); `output.js:1274 toYaml` (only output.js:273); `inspector.js:311 mapResourceCoordinates` exported but output.js:395 keeps its own copy.

**W4. `console.*` leftovers:** none. The only call is api.js:76 `console.warn("[API ERROR]", …)` in the error path. eslint clean.

**W5. Semantic divergence:** `famOf` (G6) and `kindMeta` (G6) — two classifiers/resolvers that can disagree for the same resource.

---

## 7. Size hot spots (> 1500 lines) and the seam to split at

| file | lines | natural seam (existing section markers / function groups) |
|---|---|---|
| web-proto/js/regions/inspector.js | 2402 | markers at :65 helpers, :360 rendering:resource, :1118 rendering:XRD, :1340 dispatch, :1403 mutations, :1495 expression preview & snippets, :1576 events, :2335 init. Split: `inspector/xrd.js` (:1118-1339), `inspector/preview.js` (:1495-1575), `inspector/events.js` (:1576-2334, holds the 413- and 244-line handlers). |
| web-proto/js/regions/canvas.js | 1932 | markers at :354 dependency layout (pure, "slice 46"), :527 wires, :639 pan/zoom, :766 context menu, :1104 drag-to-wire (**585 lines**, to :1688), :1689 palette drop. Split: `canvas/layout.js` (pure → unit-testable), `canvas/drag-to-wire.js`. |
| internal/adopt/adopt.go | 1824 | function groups: XRD/OpenAPI parsing :386-715 (`parseXRDDoc`, `parseOpenAPISpec`, `parseParameter`), name inference :807-952, go-template pipeline :953-1215, classic P&T :1216-1352, resource/field extraction :1420-1751, reference rewrite :1752-1823. Split: `xrd.go`, `gotemplate.go`, `classic.go`, `fields.go`. |
| internal/emit/composition.go | 1512 | validation :436-785 (`checkFieldPaths`…`closestPath`), planning :786-1222 (`planFields`, `chainGuard`, `statusWire`, `forEachStatusBound`, `statusGuard`), writer :1223-1404 (`writeForProviderTree`, `writeMapField`, `writeField`, `formatKey`), kind resolution :1405-1512. Split: `composition_validate.go`, `composition_plan.go`, `composition_write.go`. |
| internal/api/blueprint_test.go 1715, internal/blueprint/load_test.go 1703, acceptance_test.go 1650 | — | test files; splitting not warranted on size alone. |

---

## 8. Proposed consolidation refactors (max 8), ordered by risk ↑ / benefit

1. **Prune branches, worktrees, and scaffolding** (X5, X6, D10). Delete the 6 fully-merged branches and their two worktrees; triage `wip-2026-09-09-uncommitted` (cherry-pick or drop) and `CF-049-comp-tester-findings`; remove `web/dist` from `make clean`; collapse BACKLOG.md's empty sections. Risk: none. Tests: none needed.
2. **Doc corrections** (D1–D9, D11–D12). Pure text; add a `## [0.10.0]` section. Risk: none. Tests: `TestListToolsAdvertisesTheFullOperationSet` already guards mcp.md's tool list; nothing guards port/version claims.
3. **Isolate the e2e cache and filter node_modules** (T1, X4). One-line `--cache-dir ${scratchDir}/cache` in playwright.config.js:27; use `$(go list ./... | grep -v /node_modules/)` in `lint-strict` and `test-docker`. Risk: low. Tests: slice16/slice17 stop skipping; CI `test`/`acceptance` jobs.
4. **Delete or park `internal/manifest`; drop `RequiredLeaves`** (X1, X2). Risk: low (no importers). Tests: only the package's own; `internal/schema/tree_test.go:479` goes with `RequiredLeaves`.
5. **One "ensure cached + pinned" primitive in `cache.Store`** (G1, G2, E5). Make `FetchAndSave` short-circuit on a cache hit (Load → LoadDigest → pin) so providers.go:170-181 and blueprint.go:533-552 collapse to one call, and delete the three `fetch := srv.fetch` wrappers by passing `srv.fetch` straight through. Fixes E5 as a side effect (lock write becomes an error everywhere). Risk: medium (lock semantics). Tests: internal/cache/store_test.go (579 lines), internal/api/providers_test.go (661), internal/api/blueprint_test.go source-sync cases, cmd/cf/provider_test.go, internal/mcp/server_test.go `add_provider`, e2e slice2/slice15/slice16/slice17/slice18/slice22.
6. **One provider-set loader for CLI and API** (G3, E4, E8). Extract `loadProviderRefs(store, b, cl)` from options.go:29-58; have kinds.go/fields.go call it (gaining the warning and cluster label) and have blueprint.go:588-608 reuse the cluster-label reconcile. Risk: medium. Tests: cmd/cf/explore_test.go (kinds/fields), cmd/cf/serve_test.go, internal/api/blueprint_test.go, internal/api cluster tests, e2e slice45-live-cluster-source.
7. **Single render pipeline** (G5). `emit.RenderCheck(b, crds, dir, exec)` (or `internal/render`) used by gen.go `--validate` and api/render.go, carrying the Docker-unavailable classification to the CLI. Risk: medium (subprocess seam). Tests: internal/api/render_test.go (fake `lookPath`/render seams + Docker-gated real run), cmd/cf/gen_test.go validate cases, acceptance_test.go, e2e slice3.
8. **Shared web-proto helpers and one kinds cache** (G6, W2, W3). Move `slug`, `uniqueResourceName`, one `famOf`, `mapResourceCoordinates`, `sourcesSignature(doc)` into `wires.js`/a new `util.js`; put `kinds` (+ sources-sig invalidation) in `store` and let palette/inspector/canvas subscribe; drop the unused exports. Do this **before** splitting inspector.js/canvas.js (§7) — moving code is cheaper once the helpers are shared. Risk: medium (three regions touched). Tests: guardPageErrors catches any import breakage suite-wide; slice1, slice2, slice4 (unique names), slice20 (provider picker), slice28 (kind preview), slice34 (effective required), slice46 (layout), slice60 (drag-to-card picker), slice86 (`mapResourceCoordinates` diagnostics).

Then, and only then, the §7 file splits — each is a pure move once 5–8 have removed the cross-region/cross-door copies.
