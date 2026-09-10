# Re-verification of CF-086..CF-129 archived 2026-09-09..11 (HEAD 64120e5)

Method: fresh detached worktree of origin/main, `go build -o cf-v ./cmd/cf`; own `cf serve` on 19240
(scratch copy of the real `internal/examples/sqs-queue.cf.yaml`, real `~/Library/Caches/compositionfactory`),
plus empty-cache servers on 19243 (fetchable source, anonymous `DOCKER_CONFIG`) and 19242 (unfetchable
`provider-aws-sqs:v9.9.9`). CLI repros against real cached CRDs; canvas repros via a Playwright driver
(`drive.js`) run twice (drive1.json / drive2.json). Every item was executed twice; results were identical
between runs unless noted. Wall clock 01:49-02:12.

| id | verdict | reason | guarding test (inputs) |
|---|---|---|---|
| CF-109 | HALF | refusal only fires on *top-level* leaves of a native object; `match: replicas` on a k8s `Deployment` (spec.replicas) is still silently ignored; docs/dsl.md:342 reworded to "do not match" instead of documenting a refusal | `internal/emit/templates_test.go` (synthetic Secret with top-level `immutable`) |
| CF-113 | HALF | `cf help` exits 0 and the providerName remedy no longer names `cf serve`; but `cf init` still writes `blueprint.cf.yaml` while kinds/fields/serve default to `doc.cf.yaml`, `cf fields Instanc` still silently resolves (now to `ClusterInstance`), and `--validate` failures still cite `line N` of files never written | `cmd/cf/main_test.go` (synthetic) |
| CF-122 | HALF | floated editor width fixed (528 px); docked editor is 40 px tall for a 73-line doc and opens scrolled to the end (scrollTop 1404/1444) - both docked complaints remain | `tests/cf122-float-editor-window-width.spec.js` (pristine fixture; checks float only) |
| CF-091 | HALF | regenerate-on-add is wired (chip clears in the trivial case), but the realistic pre-state - a declared source that failed to load - cannot be repaired from SOURCES: the failed source is not listed, `DELETE /api/providers/<ref>` answers 404, adding a good ref leaves generate at 400 and the chip on `error` | `tests/cf091-...spec.js` (pristine fixture; adds s3 to a healthy doc) |
| CF-100 | UNVERIFIABLE | rollback-rebuild failure not reachable black-box; code verified: all three `_ = srv.rebuildIndexLocked()` now return 500 | `internal/api/blueprint_test.go` (synthetic cache) |
| CF-086 | GENUINE | SOURCES rail swaps sqs -> rds after loading the real rds starter, no reload | `tests/cf086-...spec.js` (pristine fixture + real network load of rds starter) |
| CF-087 | GENUINE | PUT with `provider-aws-sqs:v9.9.9` -> 400 naming source + verbatim MANIFEST_UNKNOWN; not persisted | `internal/api/cf087_..._test.go` (synthetic) |
| CF-088 | GENUINE | empty cache + real ref: GET /api/kinds loads provider-aws-sqs on demand (Queue/QueuePolicy served, cache populated), generate 200, canvas opens `preview · 4 files` | `internal/api/cf088_..._test.go` (synthetic), `tests/cf088-...spec.js` |
| CF-089 | GENUINE | s3 added from SOURCES survives blueprint-editor Apply (served + spec.sources keep both) | `tests/cf089-...spec.js` (pristine fixture, real fetch) |
| CF-090 | GENUINE | banner: `Output written to . · Apply: kubectl apply -R -f .` - real outDir, recursive | `tests/cf090-...spec.js` (pristine fixture) |
| CF-094 | GENUINE | after add-parameter + full PUT round-trip the file has 0 `null`/`: ""` lines | `internal/api/blueprint_test.go` (synthetic) |
| CF-095 | GENUINE | crumb `doc.cf.yaml`, `/api/version.blueprint` = served path, drawer paths `compositions/xqueues.messaging.sparky.ee.yaml` match disk | `internal/api/version_test.go`, `tests/cf095-...spec.js` (fixture) |
| CF-096 | GENUINE | real write failure (functions.yaml made a directory) -> chip `error`, tree `1 file`, comp/xrd/fns tabs + 5 tree items disabled, bp enabled | `tests/cf096-...spec.js` (mocks /api/generate 400 - fixture-only) |
| CF-097 | GENUINE | `cf gen -o` keeps README.md and mydir/notes.txt, removes only stale `compositions/stale.yaml` | `cmd/cf/gen_test.go` TestGenPreservesUnmanagedFilesInOutputDir (synthetic) |
| CF-098 | GENUINE | lock path unwritable -> POST /api/providers (cached ref) 500; 200 after restore, lock written | `internal/api/providers_test.go` (synthetic) |
| CF-099 | GENUINE | `crds: ./missing-crds.yaml` -> `cf gen` error and `cf serve` refuses to start with `read crds ...: no such file` | `internal/api/server_test.go` (synthetic) |
| CF-101 | GENUINE | playwright.config.js webServer passes `--cache-dir ${scratchDir}/cache`; no `test.skip` left in slice16/17 (inspection) | `tests/cf101-...spec.js` |
| CF-102 | GENUINE | broken blueprint -> `cf kinds`/`cf fields` error out; uncached source -> stderr warning | `cmd/cf/explore_test.go` TestCF102... (synthetic) |
| CF-106 | GENUINE | `resourcez:` -> `cf gen` error `unknown field "resourcez"` | `cmd/cf/gen_test.go`, `internal/blueprint/load_test.go` (synthetic) |
| CF-110 | GENUINE | POST resources `Queuez` -> 400 `did you mean "Queue"`; PUT resources/dlq `regionn` -> 400 (per-resource message lacks the `did you mean` that PUT /api/blueprint gives - cosmetic) | `internal/api/blueprint_test.go` (synthetic) |
| CF-111 | GENUINE | adopt of cf's own kcl/python output -> `cannot adopt composition with function "function-kcl": cf adopt supports function-go-templating and function-patch-and-transform` | `internal/adopt/adopt_test.go` (synthetic) |
| CF-112 | GENUINE | `engine: postgres` -> `resource "db" field "spec.forProvider.engine": ... got bare scalar "postgres"; did you mean {value: postgres}?` | `internal/blueprint/load_test.go` (synthetic) |
| CF-114 | GENUINE | python engine, int param `port: 8080` into env value: real `crossplane render` validation ok (no `8080.0`), exit 0 | `internal/emit/quoting_test.go` (synthetic, checks emitted script only) |
| CF-116 | GENUINE | real MANIFEST_UNKNOWN from ghcr -> `Failed to fetch package from registry: package not found at ref ... (manifest unknown). Check that the package reference and tag are correct.` | `tests/cf116-...spec.js` (1 real fetch + 2 route mocks) |
| CF-117 | GENUINE | real `cf gen --validate` on sqs starter with optional `region`: `... (fed by optional parameter params.region; mark parameter required in the XRD or provide a default)` (canvas Validate not driven; same emit path) | `internal/emit/render_validate_test.go` (synthetic CRD) |
| CF-120 | GENUINE | raw-YAML import with `resourcez:` -> 400 `unknown field "resourcez"`, nothing persisted | `internal/api/import_test.go` (testdata) |
| CF-121 | GENUINE | kind -> XPostgres re-derives plural `xpostgreses`; subtitle, API and generated file names follow (old `xqueues.*` file is left on disk by /api/generate - separate concern) | `tests/cf121-...spec.js` (fixture) |
| CF-123 | GENUINE | drop on `status.atProvider.arn` row: no `wire-target-hover`, picker opens, no `status.*` field, chip stays `preview · 4 files`; rejected replaceDoc leaves chip unchanged | `tests/cf123-...spec.js` (fixture) |
| CF-124 | GENUINE | add + Enter-rename -> exactly one rename request, no toast, no banner | `tests/cf124-...spec.js` (fixture) |
| CF-125 | GENUINE | providerName input `readonly`, delete disabled, tooltip explains; API delete 400 without CLI advice | `tests/cf125-...spec.js` (fixture) |
| CF-126 | GENUINE | row has `.src-row-remove`; `#src-remove-btn` at y=355 with scrollTop 0, above kind list; title/dialog say "sources", not "cache" | `tests/cf126-...spec.js` (fixture + real fetch) |
| CF-129 | GENUINE | unfetchable declared source: every later write answers 400 naming it, and the server retries the fetch each time (log) | `internal/api/cf129_..._test.go` (synthetic) |

Summary: 27 GENUINE, 4 HALF (109, 113, 122, 091), 1 UNVERIFIABLE (100), 0 REGRESSED. Only the cf086/089/116/126 specs
touch a real input (a live ghcr fetch); the CF-114/117 Go guards do not run crossplane; everything else runs on synthetic
CRDs or `tests/fixtures/pristine-doc.json`.

## Evidence - HALF / UNVERIFIABLE

### CF-109 (HALF)
```
# match: replicas on a native Deployment (provider: k8s) - spec.replicas exists, convention silently ignored (x2)
$ cf-v gen cf109.cf.yaml -o out109
wrote out109/compositions/xts.g.io.yaml ; grep replicas out109/compositions -> (nothing)
# guard, internal/emit/composition.go:562-582: iterates crd.FieldTree() top-level nodes, `continue` on branches,
# so only Secret-style top-level leaves (unit test uses Secret.immutable) can ever trip the refusal.
# docs/dsl.md:342 now: "native Kubernetes kinds (provider: k8s) do not match conventions."  (refusal no longer documented)
```

### CF-113 (HALF)
```
$ cf-v help; echo $?            -> 0        (fixed)
$ cf-v init                     -> scaffolded blueprint.cf.yaml / next: cf kinds, ...   (kinds.go:20, fields.go:23, serve.go:54 still default:"doc.cf.yaml")
$ cf-v fields Instanc --blueprint rds.cf.yaml   -> KIND: ClusterInstance   (no warning; silent fuzzy match)
$ cf-v gen cf117.cf.yaml --validate            -> "line 54: resource "dlq" ..." with no file written (exit 1, no "wrote" lines)
providerName remedy now: "add providerName: {type: string, required: true}"   (fixed)
```

### CF-122 (HALF)
```
CF-122 drawer-body h before expand 164
CF-122 docked box {"x":190,"y":821,"width":1210,"height":40}
CF-122 docked scroll {"scrollTop":1404,"scrollHeight":1444,"clientHeight":40,"lines":73}
CF-122 floated box {"x":421,"y":657.5,"width":528,"height":169.5}
(#drawer-min-btn collapses the drawer, so 164 px body / 40 px editor is the expanded default)
```

### CF-091 (HALF)
```
19242 (doc declares provider-aws-sqs:v9.9.9, unfetchable):
POST /api/generate -> 400 provider "...:v9.9.9" could not be loaded: ... MANIFEST_UNKNOWN
DELETE /api/providers/ghcr.io%2Fcrossplane-contrib%2Fprovider-aws-sqs:v9.9.9 -> 404 {"error":"provider not found"}
POST /api/providers {ref: ...sqs:v2.7.0} -> 200 ; spec.sources still v9.9.9 ; POST /api/generate -> 400
canvas: chip at open "error"; SOURCES rows: [provider-aws-sqs:v2.7.0, k8s]  (v9.9.9 never listed)
        add v2.7.0 from SOURCES -> alert 'provider "...v2.7.0" is already cached'; chip after add "error"
```

### CF-100 (UNVERIFIABLE)
```
git show cd96c23 -- internal/api/blueprint.go: 3x `_ = srv.rebuildIndexLocked()` -> `if rerr := ...; rerr != nil { writeJSONError(w, 500, "... index restore failed: ...") }`
No black-box way to make BuildIndex fail only on rollback (a missing crds file now fails the original write first, CF-099).
```

## Evidence - GENUINE (abridged, literal)

```
CF-106  cf: error: cf106.cf.yaml: parse blueprint: json: unknown field "resourcez"
CF-112  cf: error: cf112c.cf.yaml: parse blueprint: resource "db" field "spec.forProvider.engine": field value must be a mapping with one of value, from, raw, or template (got bare scalar "postgres"; did you mean {value: postgres}?)
CF-102  cf: error: broken102.cf.yaml: parse blueprint: yaml: line 2: did not find expected ',' or ']'
        cf: warning: provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v9.9.9" is not in the cache - continuing without it; schemas load on demand
CF-097  removed out097/compositions/stale.yaml ; kept out097/README.md out097/mydir/notes.txt (both runs)
CF-111  cf: error: adopt composition: cannot adopt composition with function "function-kcl": cf adopt supports function-go-templating and function-patch-and-transform   (python: "function-python")
CF-114  cf-v gen cf114d.cf.yaml --validate --engine python -> render validation ok / exit=0  (Deployment env PORT from integer param default "8080")
CF-117  line 54: resource "dlq" (Queue): missing required field "spec.forProvider.region" in Queue spec.forProvider (fed by optional parameter params.region; mark parameter required in the XRD or provide a default)
CF-099  cf: error: crds source "./missing-crds.yaml": open missing-crds.yaml: no such file or directory
        serve: cf: error: read crds .../srv099/missing-crds.yaml: ... no such file or directory  (server exits)
CF-110  POST /api/blueprint/resources Queuez -> {"error":"kind \"Queuez\" not found in any cached provider; did you mean \"Queue\"?"} [400]
        PUT /api/blueprint/resources/dlq regionn -> {"error":"resource \"dlq\": field \"spec.forProvider.regionn\" is not in Queue spec.forProvider (...)"} [400]; file unchanged
CF-120  POST /api/blueprint/import (raw yaml, resourcez) -> {"error":"parse blueprint: json: unknown field \"resourcez\""} [400]; grep -c resourcez doc.cf.yaml = 0
CF-094  POST parameters newParam [200], PUT /api/blueprint round-trip [200]; grep -cE 'null|: ""$' doc.cf.yaml = 0
CF-087  PUT (all refs v9.9.9) -> {"error":"failed to sync sources: unable to fetch source \"...:v9.9.9\": ... MANIFEST_UNKNOWN: manifest unknown"} [400] x2; persisted v9.9.9: 0
CF-129  19242 POST parameters p1/p2 -> 400 "failed to sync sources: unable to fetch source ..." x2; serve.log shows a retry per write
CF-098  .cf.lock as directory -> POST /api/providers (cached sqs) [500] "read .../.cf.lock: is a directory"; restored -> [200], lock file written
CF-088  19243 (empty --cache-dir, DOCKER_CONFIG={}): GET /api/kinds n=24 ['Queue','QueuePolicy',...]; cache now has provider-aws-sqs-ea55e99387b7; POST /api/generate [200]; canvas chip "preview · 4 files"
        (with the host's credsStore=desktop and PATH stripped of docker-credential-*, the fetch fails "error getting credentials" - environmental, not the fix)
CF-095  crumb "doc.cf.yaml v1alpha1"; /api/version {"outDir":".","blueprint":"doc.cf.yaml"}; eb-path compositions/xqueues.messaging.sparky.ee.yaml, xrds/xqueues.messaging.sparky.ee.yaml; disk matches
CF-090  banner "Output written to . · Apply: kubectl apply -R -f . (kubectl apply -f) · Package: cf package"
CF-096  functions.yaml made a directory, click Generate -> chip "error", count "1 file", tabs disabled {comp,xrd,fns:true, bp:false}, tree items disabled 5
CF-121  xrd {"kind":"XPostgres","plural":"xpostgreses"}; subtitle "xpostgreses.messaging.sparky.ee · v1alpha1"; files after gen include xpostgreses.messaging.sparky.ee.yaml; eb-path follows
CF-124  rename requests ["POST /blueprint/parameters/newParam/rename"]; toast visible false; banner count 0; params include envRegion
CF-125  name input {"readonly":"","title":"providerName is required for managed resources in Namespaced XRD"}; delete btn disabled; API delete 400 without "cf serve"
CF-123  hover class "port status" (no wire-target-hover); picker visible true; status.* fields []; chip before/after/after-rejected-write "preview · 4 files"
CF-086  rail after rds load contains provider-aws-rds:v2.7.0 · 44 kinds, not provider-aws-sqs; served ["...provider-aws-rds:v2.7.0"]
CF-089  served after Apply [sqs:v2.7.0, s3:v2.7.0]; doc sources after Apply both
CF-126  row remove btn {"count":1,"title":"Remove this provider from sources"}; #src-remove-btn box y=355, lrail scrollTop 0, above first kind true; dialog "Remove ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 from sources?"
CF-116  "Failed to fetch package from registry: package not found at ref \"ghcr.io/crossplane-contrib/provider-aws-sqs:v9.9.9\" (manifest unknown). Check that the package reference and tag are correct."
CF-101  playwright.config.js:30 ... --cache-dir ${scratchDir}/cache ; grep test.skip tests/slice16*.js tests/slice17*.js -> none
ALL pageErrors [] (both passes)
```

Artefacts: drive.js, drive1.json, drive2.json, drive3.json, b1/ (CLI cases, val114.log, val117.log), srv*/serve.log in the scratch dir.
