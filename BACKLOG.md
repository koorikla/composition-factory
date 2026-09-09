# Backlog

Open work only — concise, prioritized, and verified against the codebase.

Completed work is archived in [docs/backlog-archive.md](docs/backlog-archive.md); full history is in `git log -p BACKLOG.md`.

Each open item carries a permanent `CF-NNN` id. Ids are never reused, including after
archival. When an item is dispatched it gets a self-contained brief at
`docs/tasks/CF-NNN-<slug>.md`; the agent executing it works to
[docs/task-execution-contract.md](docs/task-execution-contract.md). Filing new items
is `.claude/skills/backlog-authoring/`.

---

## Architectural Principle: DSL (`.cf.yaml`) as Canonical Intermediate Representation

The `factory.crossplane.io/v1alpha1` `Blueprint` document (`.cf.yaml`) is and remains the single source of truth and intermediate representation (IR) for `composition-factory`. 

All user interfaces (Canvas, CLI, API, MCP) operate on this model. Crossplane manifests (`composition.yaml`, `definition.yaml`, `functions.yaml`, `package.yaml`) are deterministic, generated artifacts. Manifest import and adoption act as high-fidelity converters *into* the canonical Blueprint format.

---

## Architectural Principle: The Round-Trip Rule

**Anything cf generates must survive Kubernetes and come back.** Apply it to a
real cluster, read it back with `kubectl get <kind> -o yaml`, and cf must be
able to import that — the server-round-tripped form, not just the file cf
wrote. The API server defaults fields, reorders maps, injects `managedFields`,
`creationTimestamp`, `uid`, `resourceVersion` and `status`, and prunes anything
the schema does not know; an importer that only reads cf's own output has not
been tested against the only version of the document that matters operationally.

Recorded in AGENTS.md §1 as an Engine Truth, so it binds every agent and not
just this backlog.

This is the acceptance bar for Track 1, and it is testable rather than
aspirational: `cf gen` → `kubectl apply` → `kubectl get -o yaml` → `cf import`
→ `cf gen` must reproduce the original bytes, with the server-added fields
scrubbed and named in a loss report. Lane C already stands up the kind cluster
this needs on every push, so the oracle exists — it just is not pointed at this
yet. Any generated artifact that cannot make the trip is a bug in the emitter,
not an exception for the importer to special-case.

---

## Open — 2026-09-09 journey fan-out at `f45c2a8` (v0.10.0)

Severities in this section use the **UX** scale unless marked *engine*: **P0** lost work,
impossible, or the interface states something false · **P1** completable only with knowledge
that exists solely in the source · **P2** completable, wastefully · **P3** polish. Engine scale
(P0 wrong output silently · P1 loss that survives to the cluster · P2 source-only knowledge or
unsafe API contract · P3 docs) is in `.claude/skills/backlog-authoring/SKILL.md`.
Earlier run reports: [docs/ux-runs/](docs/ux-runs/2026-09-04-m1-first-contact.md),
[docs/comp-runs/](docs/comp-runs/2026-09-04-cachedservice-namespaced-roundtrip.md).

### P0

- [ ] **CF-089 — Adding a provider from SOURCES and then applying any full-document write
      (blueprint editor Apply, engine selector) silently drops that provider again. [V]** Neither
      add path in `web-proto/js/regions/palette.js:869` / `:887` refreshes the client document
      after `POST /api/providers`, so `store.replaceDoc` PUTs a `spec.sources` that predates the
      add and the server's reconciliation evicts the provider. Scripted repro on `f45c2a8`, twice:
      after Add `/api/providers` = s3+iam, the editor text lists only s3, after Apply
      `/api/providers` = s3 and the file on disk has lost iam; no error anywhere. The client
      document must never carry stale `spec.sources` into a write.
- [ ] **CF-090 — The Generate toast says `Output written to compositions · Apply: kubectl apply -f
      compositions` while three of the four files land outside `compositions/`. [V]**
      `output.js:452-453` builds the toast from one path; `POST /api/generate {write:true}` on
      `f45c2a8` writes `compositions/<xrd>.yaml`, `xrds/<xrd>.yaml`, `functions.yaml`,
      `providerconfigs/aws.yaml`. Following the toast applies a Composition without its XRD or
      functions. The button title and confirm say the destination is `.`, which for the container
      user is the mounted `$HOME`; the resolved directory is shown nowhere. The toast, the
      button and the confirm must name the real directory and an apply command that covers
      every written file.

### P1

- [ ] **CF-091 — After the user repairs a missing provider from SOURCES, the top bar keeps saying
      `error … not in the cache` and the composition pane stays at `0 lines` until an unrelated
      edit or a reload.** Adding a provider triggers `POST /api/providers, GET /api/providers,
      GET /api/kinds` and never a regenerate (J1, two runs). Validate in that state says
      "validation check unavailable" and the pane still shows nothing. A successful provider add
      must re-run the generate cycle and clear the stale error. Follows CF-088.
- [ ] **CF-092 — Loading a starter example overwrites the served blueprint file on disk with no
      file-level cue. [V]** The card says "replaces current blueprint · undoable"; nothing says
      which file. Observed: `~/xqueue.cf.yaml` (the user's own file, bind-mounted) went
      sqs-queue → xpostgres → s3-bucket across two clicks; the top bar then shows
      `blueprints/xpostgres.cf.yaml`, a path that does not exist, so the user cannot see that
      `xqueue.cf.yaml` is what changed. Undo covers the session only. The load must say which
      file it replaces, or write elsewhere.
- [ ] **CF-097 — *(engine)* `cf gen -o <dir>` deletes every file under `<dir>` it did not write,
      recursively, with no flag, no confirmation and no mention in the docs. [V]**
      `cmd/cf/gen.go:198-203` removes whatever `findExistingManagedFiles` (`:250`, a walk of the
      whole directory when `-o` is not `.`) did not just emit; `gen_test.go:394` pins it as
      designed. Repro on `f45c2a8`, twice: `-o gitops/` holding `kustomization.yaml`, `README.md`,
      `base/app.yaml` → all three `removed …`, exit 0. `docs/cli.md:118` says only "Output
      directory (defaults to `.`)". A user pointing at a GitOps folder loses hand-written files.
      Pruning must be opt-in or confined to files cf itself wrote, and documented either way.
- [ ] **CF-107 — *(engine)* Omitting a CRD-required field (`region` on every `Bucket`) passes
      `cf gen`, `PUT /api/blueprint` and `POST /api/generate` with exit 0 and no warning; only
      `--validate` (a real render) catches it. [V]** `cf fields Bucket --required` already knows
      `region string true`. Repro on `f45c2a8`, twice: delete the three `region:` lines from the
      s3-bucket starter → gen exit 0, `grep -c region compositions/*.yaml` = 0. The API server
      rejects the composed resource at apply time. README:10 claims strict validation at generate
      time; generation must refuse, or at least warn, when an effective-required field has no
      value, wire or guard. (CF-055 covered the optional-param-into-required case only.)
- [ ] **CF-108 — *(engine)* `cf adopt <composition.yaml>` without the XRD alongside retypes every
      parameter as `string` and drops `required`, `default`, `enum` and `description` with no
      loss report; the lost `required` regenerates into an XRD that lets `region` be omitted.**
      Verbatim before/after and the regenerated `required: [providerName]` vs
      `[providerName, region]` in [docs/comp-runs/2026-09-09-cli-mcp-journey.md](docs/comp-runs/2026-09-09-cli-mcp-journey.md)
      F2. Adopting the directory (XRD present) is lossless. README:121 promises losses are named
      on screen. When the XRD is absent the adopter must say what it could not recover, per
      parameter, and must not invent `type: string`.
- [ ] **CF-109 — *(engine)* A `spec.conventions` entry that matches a native-kind field is
      silently ignored — not applied, not refused — although docs/dsl.md:342 and :418 say it is
      refused with `conventions cannot match native Kubernetes kind`.** J3 F4: `match: immutable`
      against a `Secret` → exit 0, the `define` block is emitted and never called, the Secret has
      no `immutable:`. The same convention on a managed field works. A user relying on a
      convention for labels on native kinds gets nothing on the cluster and no signal.
- [ ] **CF-119 — Importing the Composition that Generate just wrote comes back with every
      parameter's `required` flag lost and the inferred `auto-ready` step turned into a custom
      pipeline step whose kind 404s.** J2 F1, twice: `required:false` for `dbName`,
      `instanceClass`, `region`; inspector `PIPELINE (1 CUSTOM)`; console
      `404 /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields`. The canvas's Import
      takes one file, so the XRD is never alongside — the CLI face of the same loss is CF-108.
      Round-Trip Rule: cf's own output must come back intact, or the loss must be named on screen.
- [ ] **CF-120 — The Edit-blueprint Apply (and `POST /api/blueprint/import`) silently discards an
      unknown key while applying the rest, under a note that promises "invalid YAML never lands";
      `PUT /api/blueprint` rejects the same key. [V]** J2 F5, twice (and twice again by the author via the raw-YAML import route: 200, `xrd` keys unchanged, nothing on disk): a hand-typed `xrd.status:` block
      vanished, the `sources` change in the same Apply landed, no toast, header still `preview`.
      Same class as CF-106 on the CLI; the import gate must refuse unknown keys like PUT does.
- [ ] **CF-121 — Renaming the XRD kind in the inspector leaves `plural` at the old value, the
      plural is shown as static text and is editable nowhere, so Generate names every file and
      the Composition after a kind that no longer exists.** J2 F3, twice: `XApp` → `XPostgres`,
      subtitle stays `xapps.platform.example.org`, `/api/blueprint` `"kind":"XPostgres","plural":"xapps"`,
      files `xapps.platform.example.org.yaml`. Renaming must re-derive or expose the plural.
- [ ] **CF-122 — "Float editor window" turns the blueprint editor into a 24 px-wide textarea
      (one character per line); docked, it is 80 px tall for an 80-line document and opens
      scrolled to the end.** J2 F4, twice: textarea box `{w:24,h:80}` floated vs `{w:684,h:80}`
      docked. Hand-editing is impossible in the mode built for it.

### P2

- [ ] **CF-093 — In the published image Validate always answers "validation check unavailable"
      and its fix tip prescribes `curl … | sh` in a container that has no curl and runs as
      uid 100.** `/api/render` → `unavailable: crossplane CLI not found on PATH`. The image ships
      one of the canvas's two headline actions non-functional and the tip is not actionable there.
      Either the image carries the `crossplane` CLI (static binary) or the canvas says the action
      is unavailable in this deployment and why.
- [ ] **CF-094 — Every server-side write of the blueprint fills the file with zero-value noise the
      editor never shows: `conventions: null`, `templates: null`, `enum: null`, `from: ""`,
      `raw: ""`, `template: ""`. [V]** 35 such lines in a doc written by `f45c2a8` after a
      starter load; the drawer's edit view of the same document shows a clean form, so what the
      user reads is not what is in their directory (and what they commit to git). The writer must
      omit empty fields, byte-identically for documents that already lack them.
- [ ] **CF-095 — The top bar and drawer show paths that do not exist: `blueprints/<name>.cf.yaml`
      and `compositions/<name>.yaml`, while the real files are `<served path>` and
      `compositions/<xrd plural>.<group>.yaml`.** `output.js:81` and `:617` synthesise the
      names from `metadata.name`; the served path is never fetched or shown. Show the real
      paths (relative to the workspace) or none.
- [ ] **CF-098 — *(engine)* `POST /api/providers` for an already-cached ref answers 200 while
      discarding the lockfile write error.** `internal/api/providers.go:178` `_ = l.Write(srv.Lock)`;
      the same operation in `blueprint.go:547-550` returns an error. A failed pin leaves `.cf.lock`
      without the digest the reproducibility rule (AGENTS.md §1) depends on. The add must fail
      loudly when the pin cannot be written.
- [ ] **CF-099 — *(engine)* A `crds:` source whose file is missing is skipped silently: its kinds
      vanish from the index with no error, no warning and no UI state.** `internal/api/server.go:484-486`
      `if os.IsNotExist(err) { continue }` inside `BuildIndex`. Cards bound to those kinds lose
      their schema and the user sees nothing. A missing declared source must surface like an
      unfetchable provider (CF-087/CF-088).
- [ ] **CF-100 — *(engine)* The three write-rollback paths ignore the index rebuild's error, so a
      failed write can leave `/api/kinds` serving a provider the server no longer holds.**
      `internal/api/blueprint.go:172`, `:180`, `:634` `_ = srv.rebuildIndexLocked()`; the rebuild
      swaps `srv.Index` only on success (`server.go:565`). After the rollback `srv.Providers` and
      `srv.Index` disagree. Rollback must restore both or report that it could not.
- [ ] **CF-101 — *(engine)* The Playwright suite runs against the developer's real schema cache:
      `playwright.config.js:27` starts the engine without `--cache-dir`, so specs skip or pass
      depending on what the host has cached and `make test-e2e` writes provider-nop into
      `~/Library/Caches/compositionfactory`.** `tests/slice17-catalogue.spec.js:26` and
      `slice16-provider-remove.spec.js:29` carry host-state `test.skip`s as the symptom; CI runs
      cold and locally runs warm, which is one source of the "flaky e2e" pattern. The suite must
      run against a scratch cache like it runs against a scratch document.
- [ ] **CF-102 — *(engine)* `cf kinds` and `cf fields` silently fall back to "every cached
      provider" when the blueprint fails to load, and never warn when a declared source is
      uncached, so they disagree with `cf gen` and the canvas on the same file.**
      `cmd/cf/kinds.go:29-31`, `cmd/cf/fields.go:32-34` treat a load error as "no blueprint";
      `cmd/cf/options.go:29-58` is the third copy of the provider-set assembly with different
      behaviour. One loader, one warning policy.
- [ ] **CF-110 — *(engine)* `POST /api/blueprint/resources`, `PUT /api/blueprint/resources/{name}`
      and the MCP `add_resource`/`update_resource` tools accept an unknown kind or a misspelt field
      path, answer success and persist it; `PUT /api/blueprint` on the same server rejects the
      same content with the nearest-match error.** J3 F5, twice over HTTP and MCP: `Instanze`
      and `instanceClas` land in the file and every later generate fails until it is hand-edited.
      An agent must call generate after every write to find out whether the write was valid. The
      per-resource routes must validate like the whole-document route.
- [ ] **CF-111 — *(engine)* `cf adopt` refuses cf's own `--engine kcl` and `--engine python`
      output with `spec.pipeline[0].name: "render-resources" collides with the built-in templating
      step's name`, which names the adopter's own construction, not the cause (only
      function-go-templating and patch-and-transform are adoptable).** J3 F6, twice per engine.
      The refusal must say which engines adopt supports and which one it found.
- [ ] **CF-112 — *(engine)* A field written as a bare scalar (`engine: postgres` instead of
      `engine: {value: postgres}`) fails with `json: cannot unmarshal string into Go value of type
      blueprint.rawField`, naming no resource, field or line.** J3 F7, twice. The two-modes case
      next to it is precise (`resource "db" field "engine": set exactly one of …`); this one must
      reach the same standard.
- [ ] **CF-116 — A mistyped provider ref in SOURCES shows `Server unavailable (HTTP 502 Bad
      Gateway): fetch "…": GET https://ghcr.io/v2/…: MANIFEST_UNKNOWN … The backend server may be
      restarting or unreachable.`** Residue of CF-059: `providers.go:193` answers 502 for a
      registry lookup failure and `api.js:67-72` labels every 502 as the backend restarting, so
      the advice is false and the raw Go error still reaches the user. Twice (J4). The message must
      say the package was not found at that ref.
- [ ] **CF-117 — When Validate fails on a required field fed by an optional parameter, the error
      still says `missing required field "spec.forProvider.region"` and never names the parameter
      or the promote action.** Residue of CF-055: the picker badge and inspector warning exist,
      the render-time message is unchanged (twice, J4). The message must name `params.region`,
      say it is optional, and point at the promote action.
- [ ] **CF-123 — A parameter dot can be dropped on a resource card's *output* row; the server
      answers 400 with `field "status.atProvider.…" is not in Instance spec.forProvider (an unknown
      field is silently pruned …)` and the header chip sticks on `error` until the next successful
      edit.** J2 F6, twice. Output rows must not accept the drop, and a rejected write must not
      leave the chip in `error` for a document that is unchanged.
- [ ] **CF-124 — Every successful parameter add or rename is followed by a false error toast and
      a sticky inspector banner `rename parameter: "newParam" is not declared`.** J2 F7, five
      times: Enter and blur each send the rename; the second 404s and is reported as a failure
      though the first succeeded. One rename per edit, and no error for an operation that worked.
- [ ] **CF-125 — `providerName` looks editable (name input, enabled `×`) but rename and delete
      are reverted with `run cf serve without --blueprint to scaffold one`, a terminal instruction
      shown in the browser.** J2 F8, three attempts. The row must read as locked and the message
      must say why in canvas terms. (Same CLI-in-browser pattern as CF-088 and CF-091.)
- [ ] **CF-126 — "Remove provider" is reachable only ~1300 px down inside the expanded provider
      entry, after its full kind list; the row itself offers nothing on hover, click or
      right-click, and the control speaks of "the cache" for what the user sees as the blueprint's
      sources.** J2 F9, three gestures before it was found. The refusal itself (still referenced
      by resources "instance") is correct and well placed.

### P3

- [ ] **CF-096 — While generation has failed (`error`, `0 lines`) the ARTIFACTS panel still
      announces `6 files` with tabs for composition, definition, functions, package and rbac.**
      Observed on the first load of a blueprint with an uncached source. A failed generate must
      empty or grey the artifact list rather than list files that do not exist.
- [ ] **CF-103 — Docs state things the tree contradicts (ports 8081/8086, Go 1.25, the `cf`
      subcommand list, `blueprints/xqueue.cf.yaml`, the "frozen" store/api contracts, `make lint`
      scope, no `[0.10.0]` changelog entry, `cf adopt` accepting directories and the `import`
      alias).** Twelve items with both sides quoted in
      [docs/code-audit.md](docs/code-audit.md) §3 (D1–D12). Acceptance: none — documentation;
      verify each against the quoted command.
- [ ] **CF-104 — Dead weight still tracked: `internal/manifest` (808 lines, no importer),
      `schema.RequiredLeaves`, `cmd/cf/serve.go:85 defaults`, Go code under `node_modules`
      inside the module (reaches `lint-strict`/`test-docker`), `make clean` removing `web/dist`,
      six merged remote branches, two 0-ahead worktrees, the `wip-2026-09-09-uncommitted`
      snapshot (72 behind, nothing main lacks) and `CF-049-comp-tester-findings` (superseded).**
      Inventory with ahead/behind counts in [docs/code-audit.md](docs/code-audit.md) §2. Delete,
      then `make lint-strict && make test` stay green.
- [ ] **CF-105 — Consolidation: the same logic exists two to four times across doors and regions
      and already disagrees.** `cf gen --validate` reports a stopped Docker daemon as a
      composition error because `cmd/cf/gen.go:96-147` lacks `render.go:212-230`'s
      classification; "cached → pin lock" is written three times with three error policies
      (CF-098); `famOf` classifies an Azure provider as `azure` on the card and `k8s` in the
      palette; `/api/kinds` is cached three times with three invalidation policies. The set,
      ordered by risk with the tests that cover each seam, is
      [docs/code-audit.md](docs/code-audit.md) §8 items 5–8; do them in that order, one branch
      each, before any file split from §7.
- [ ] **CF-113 — CLI/docs polish from the same run: `cf help` exits 80 as an error while
      `cf --help` says to use it; `cf init` writes `blueprint.cf.yaml` but `kinds`/`fields`/`serve`
      default to `doc.cf.yaml` and init's own hint points at `cf kinds`; the `providerName`
      remedy sends CLI users to `cf serve`; `--validate` failures cite `line N` of a file never
      written; `cf fields Instanc` silently resolves to `InstanceProfile`; docs/dsl.md quotes
      four error messages the binary no longer emits; docs/cli.md omits `cf adopt <dir>`, `-`,
      the `import` alias and `provider add --lock/--cache-dir`; docs/mcp.md:3 says "full
      authoring surface" for 19 of 36 routes and never names the `api_version` argument.**
      Every item quoted both sides in
      [docs/comp-runs/2026-09-09-cli-mcp-journey.md](docs/comp-runs/2026-09-09-cli-mcp-journey.md)
      (F8–F13, F17, F18, mismatch table). Acceptance: none — documentation and messages.
- [ ] **CF-115 — The cross-engine acceptance diff (`TestAcceptanceAlternativeEnginesRender`) runs
      on `testdata/xqueue.cf.yaml`, which has no integer→string field and no status wire, so the
      CF-114 class it was added for (CF-081) passes it.** Extend the fixture (or diff the
      k8s-workload starter too) so the three engines are compared on the fields that diverge.
- [ ] **CF-118 — No test renders the seven starter examples; CF-084 was closed by editing YAML
      and `TestAllExamplesAreValidBlueprints` only calls `Validate()`.** A starter can regress
      to failing its own Validate button again without any gate noticing. Add a table-driven
      real-render check over `internal/examples/*.cf.yaml` behind the same tool gate as the
      acceptance tests.
- [ ] **CF-127 — Required-field counts for one kind disagree in three places: KINDS row `1 req`,
      hover card `155 fields · 1 required`, inspector `155 leaf fields · 13 required`, while the
      inspector's Required filter lists two fields.** J2 F11. One definition of "required",
      shown once.
- [ ] **CF-128 — In the SOURCES catalogue an installed entry's name truncates to `provider-a…`,
      its ref wraps onto four lines under the `INSTALLED · 44 KINDS` badge, and the search-term
      highlight from an earlier query is painted inside the installed list's name.** J2 F12,
      screenshots in [docs/ux-runs/2026-09-09-canvas-xpostgres-build.md](docs/ux-runs/2026-09-09-canvas-xpostgres-build.md).

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
