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

### P1

- [ ] **CF-130 — `spec.environment` has no GUI: environment keys can be declared only by
      hand in the Edit tab, and generation emits nothing to fill.** The DSL documents
      `spec.environment` (docs/dsl.md:114) and the canvas offers declared keys as wire sources
      (`web-proto/js/regions/canvas.js:270` `env.<key>`), but the inspector has no section to
      add, type, default or remove an environment key, the XRD card shows none, and
      `internal/emit/pipeline.go:40-51` emits the `function-environment-configs` step
      referencing an EnvironmentConfig named `default` without emitting an EnvironmentConfig
      scaffold that lists the declared keys. A canvas user cannot use the feature; a CLI user
      has nothing to fill in. Contract: declare/edit keys in the inspector, see them on the
      XRD card as wire sources, and get a scaffold with every declared key.

- [ ] **CF-092 — Loading a starter example overwrites the served blueprint file on disk with no
      file-level cue. [V]** The card says "replaces current blueprint · undoable"; nothing says
      which file. Observed: `~/xqueue.cf.yaml` (the user's own file, bind-mounted) went
      sqs-queue → xpostgres → s3-bucket across two clicks; the top bar then shows
      `blueprints/xpostgres.cf.yaml`, a path that does not exist, so the user cannot see that
      `xqueue.cf.yaml` is what changed. Undo covers the session only. The load must say which
      file it replaces, or write elsewhere.
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
- [ ] **CF-119 — Importing the Composition that Generate just wrote comes back with every
      parameter's `required` flag lost and the inferred `auto-ready` step turned into a custom
      pipeline step whose kind 404s.** J2 F1, twice: `required:false` for `dbName`,
      `instanceClass`, `region`; inspector `PIPELINE (1 CUSTOM)`; console
      `404 /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields`. The canvas's Import
      takes one file, so the XRD is never alongside — the CLI face of the same loss is CF-108.
      Round-Trip Rule: cf's own output must come back intact, or the loss must be named on screen.
### P2

- [ ] **CF-129 — *(engine)* After one failed source fetch, every later write answers with the
      bare document and never mentions that the declared source is still unloaded. [V]**
      Residue of CF-087: `internal/api/blueprint.go:549` skips sources memoised in
      `srv.failedSources`, so only the first `PUT /api/blueprint` reports
      `failed to sync sources: unable to fetch source "…"`; the second and third return the
      document as if all sources were served (`GET /api/providers` still `[]`). Twice on
      `6fda5b6`, empty cache, no registry credentials. A write must report an unloaded declared
      source every time until it loads, and the memo must not suppress a retry the caller asks for.

- [ ] **CF-093 — In the published image Validate always answers "validation check unavailable"
      and its fix tip prescribes `curl … | sh` in a container that has no curl and runs as
      uid 100.** `/api/render` → `unavailable: crossplane CLI not found on PATH`. The image ships
      one of the canvas's two headline actions non-functional and the tip is not actionable there.
      Either the image carries the `crossplane` CLI (static binary) or the canvas says the action
      is unavailable in this deployment and why.
- [ ] **CF-095 — The top bar and drawer show paths that do not exist: `blueprints/<name>.cf.yaml`
      and `compositions/<name>.yaml`, while the real files are `<served path>` and
      `compositions/<xrd plural>.<group>.yaml`.** `output.js:81` and `:617` synthesise the
      names from `metadata.name`; the served path is never fetched or shown. Show the real
      paths (relative to the workspace) or none.
- [ ] **CF-102 — *(engine)* `cf kinds` and `cf fields` silently fall back to "every cached
      provider" when the blueprint fails to load, and never warn when a declared source is
      uncached, so they disagree with `cf gen` and the canvas on the same file.**
      `cmd/cf/kinds.go:29-31`, `cmd/cf/fields.go:32-34` treat a load error as "no blueprint";
      `cmd/cf/options.go:29-58` is the third copy of the provider-set assembly with different
      behaviour. One loader, one warning policy.
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

## Open — floci lane: real AWS objects against an emulator

Facts, field names and the design sketch: [docs/research/2026-09-10-floci-lane.md](docs/research/2026-09-10-floci-lane.md).
These are lane items, not defects: each one's acceptance run is the first time the path is
executed, and every failure it surfaces is filed as its own item. Order: CF-131 first.

- [ ] **CF-131 — Lane D bring-up: floci reachable from the kind cluster, an emulator
      ProviderConfig, and one smoke path (sqs-queue starter → XR → Queue Ready → the queue is
      listed by `aws --endpoint-url http://127.0.0.1:4566 sqs list-queues`).** Contract: `make
      cluster` can start a pinned `floci/floci:<tag>` on the kind network (opt-in, e.g.
      `FLOCI=1`); the providerconfig scaffold gains a documented emulator variant using
      `spec.endpoint.url.static`, the `skip_*` flags and `s3_use_path_style` with the exact
      CRD field names; `make test-floci` installs provider-aws-sqs v2.7.0, applies the
      starter's XRD/Composition/XR in the workspace namespace and passes only when the
      managed Queue is Ready and visible through the AWS CLI. Gated on Docker like Lane C.
- [ ] **CF-132 — Status wires proven on real objects: the Queue's `status.atProvider.url` and
      `arn` reach the XR status and a composed Secret, and match what the AWS CLI reports.**
      Today the only oracle for status wires is `crossplane composition render` with a
      hand-written observed resource. Contract: a Lane D test that builds a blueprint with a
      status wire into a native Secret `stringData` key and one into an XR status field,
      applies it, waits for Ready, and asserts both values equal `aws sqs get-queue-url` /
      `get-queue-attributes` output. Merges after CF-131.
- [ ] **CF-133 — "crd mode" on the lane cluster: `cf serve --cluster` / Connect & Sync CRDs
      against the kind cluster with the providers installed, then build, apply and round-trip
      from discovered kinds.** Contract: the discovered kind set for an installed provider
      equals the cached package's kind set (parity assertion, names and apiVersions); a
      blueprint authored from discovered kinds generates byte-identically to one authored from
      the package cache; `cf gen` → `kubectl apply` → `kubectl get <xr> -o yaml` → `cf adopt`
      → `cf gen` reproduces the original bytes with server-added fields named in the loss
      report (the Round-Trip Rule as stated above, now on objects that actually reconciled).
      Merges after CF-131.
- [ ] **CF-134 — Every AWS starter through Lane D: s3-bucket, sqs-queue, irsa (IAM/STS are
      emulated) and rds-postgres (floci runs a real PostgreSQL behind RDS; needs the Docker
      socket mounted into floci).** Contract: each starter's XR reaches Ready, its objects are
      visible through the AWS CLI, and for rds-postgres the `writeConnectionSecretToRef` Secret
      holds an endpoint a `psql` connection from inside the cluster accepts. Each failure is a
      new backlog item against the emitter, not a special case in the lane. Merges after CF-132.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
