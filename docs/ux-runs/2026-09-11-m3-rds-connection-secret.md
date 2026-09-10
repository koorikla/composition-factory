# UX run 2026-09-11 - M3 Connection secret: RDS PostgreSQL

Repo HEAD 6efa086 (binary reports v0.10.0-69-g1234f14-dirty). Engine `bin/cf serve` on 127.0.0.1:19230, blank start
(no doc file beforehand). Driver: throwaway Playwright script (`driver.js`, chromium headless 1440x900) acting by visible
labels and positions only; console errors, page errors, dialogs and >=400 responses collected in `events.log`.
Because the driver is scripted, hesitation is inferred from attempt counts, so P2s here are weaker than a live run's.
Scratch dir S = this directory; screenshots in `shots/`. Nothing was written inside the repo.

## Mission and outcome

**completed.** Wall clock to first green Validate: **9 min** (01:43:56 server up -> 01:52:55 "valid · 1 resource"),
of which ~3 min was driver setup. Export via Generate, round-trip via Import, both through the UI. No uncaught page
error in the whole run (0 `pageerror` events). Total run 01:43 - 02:05.

Oracle check: `out.first/compositions/xapps.platform.example.org.yaml` carries `writeConnectionSecretToRef.name`
(wired to `$spec.connectionSecretName`); the password appears only as `passwordSecretRef: {name, key}` plus
`autoGeneratePassword: true`, no literal password anywhere; Validate green before and after the round-trip.

## Narrative

The canvas opens straight into the "Choose an Example Blueprint" modal (shots/01-arrival.png). One card is literally
"AWS RDS PostgreSQL Database ... credentials connection secret envelope" - the whole mission, one click away. I closed
it, because the mission is to build, but a real person would take the shortcut and never learn the envelope UI.

Behind the modal the empty canvas shows a two-step hint, "1. DRAG KINDS FROM KINDS 2. ADD CLOUD PROVIDERS IN SOURCES",
with KINDS and SOURCES in bold. I clicked the bold SOURCES; nothing happened (attempt 1). The real tab is a small strip
above the palette; found it on attempt 2 (shots/04-sources-tab2.png).

In SOURCES > PROVIDERS CATALOGUE I typed the word I know, "postgres": only `provider-azure-dbforpostgresql` came back
(shots/05-search-postgres.png). "rds" found `provider-aws-rds` (and, oddly, `provider-gcp-dns`). Add was instant - the
row showed `sha256:16c8fb42f66c · 44 kinds` at 0.5 s (cache hit; a first fetch was not exercised).

KINDS grew a group `RDS.AWS.M.UPBOUND.IO` with `Instance 1 req`. Dragging it onto the canvas worked first time; the
Inspector opened on it with a header "155 leaf fields · 13 required" and the Required filter showing `region` and,
under a red "CROSSPLANE ENVELOPE" heading, `writeConnectionSecretToRef.name REQ` (shots/11-after-drop.png,
shots/15-envelope-section.png). So the envelope is findable without the word: it is the only red section and the
only REQ outside forProvider. The Val/Wire/Raw buttons on those rows are cut off at the panel edge though.

I set region and, in the All view, engine/engineVersion/instanceClass/allocatedStorage/username; the node card grew a
line per field (shots/17-text-fields-set.png). Booleans are plain text boxes reading "unset — omitted from output"; I
typed `true` for autoGeneratePassword and skipFinalSnapshot, and `yes` for publiclyAccessible to see what happens:
a toast "resource "instance" field "publiclyAccessible": value "yes" is not a valid boolean (use true or false)" and
the field cleared (shots/20-bool-yes.png). Good gate, but nothing told me it wanted `true` before I guessed.

The secret-shaped field: there is no plain `password` on this kind, only `passwordSecretRef.key`/`.name` (both
badged REQ) and `passwordWoSecretRef.*` (also REQ). The Inspector says nothing about which of these is the password
path or that autoGeneratePassword fills it; I set `db-master-password`/`password` from prior knowledge. Nothing in the
UI distinguishes "reference to a secret" from any other string, and nothing warns if you type a secret in plain.

While scrolling the All view I hesitated over eleven other REQ rows - `dbSubnetGroupNameRef.name`,
`kmsKeyIdRef.name`, `vpcSecurityGroupIdRefs[0].name`, ... - each with the placeholder "required — set a value or wire
it". I left them empty on a hunch; Validate went green, so they were never required. A user who trusts the badge
fills eleven fields for nothing or gives up.

Envelope: Wire on `writeConnectionSecretToRef.name` offered "wire to… / params.providerName / + new XRD parameter…"
- no way to say "the XR's name". I chose the new-parameter path: the Wire menu is a native select (attempt 1 clicked
the option text and timed out - driver, not UI), then a `parameterName` box with a type select and Add. My first Add
click hit the Annotations "Add" further down (driver error). Named it `connectionSecretName`; the composition gained
`{{- if hasKey $spec "connectionSecretName" }} writeConnectionSecretToRef: ...` - guarded, because the new parameter
is optional. So an XR that omits the parameter silently gets no connection secret.

Validate: "validating…" at 1 s, "valid · 1 resource" at 2 s (shots/30-validate-done.png). Generate asked
"Generate will write manifests to disk in '<out>', overwriting existing files. Proceed?" and wrote 4 files; the header
said "written · 4 files" while the ARTIFACTS panel says "7 files".

Import: the button opens a native single-file chooser; I fed it the exported Composition. No confirmation, no
summary - the header just changed to "xapps.platform.example.org.cf.yaml · preview · 4 files" and the served
`doc.cf.yaml` was overwritten. The canvas came back with the same resource, all ten fields, the envelope wire and both
parameters (shots/34-after-import.png). Two differences: the PIPELINE panel now read "PIPELINE (1 CUSTOM)" with an
editable auto-ready step ("Input YAML (uncached)") where before it was "auto-ready inferred default", and the console
logged a 404 for `/api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields`. Validate green again.

### Off-script: Edit tab and Examples (15 min)

The blueprint tab in the drawer has an "edit" toggle; ⛶ floats the drawer as a window (shots/38-edit-mode.png).
Apply with an invented `bogusKey` under spec was refused in place: "import failed: parse blueprint: json: unknown
field "bogusKey"" (shots/39-apply-unknown-key.png). Then the real edit: added `storageGb: integer default 20`,
`default: db-conn` on connectionSecretName, engineVersion 16.3 -> 16.4, second source `provider-aws-s3:v2.7.0`. After
Apply the canvas XR card, the Inspector (PARAMETERS (3), `20`, `db-conn`) and `curl /api/blueprint` agreed; SOURCES
listed rds + s3, KINDS gained `Bucket`; definition.yaml emitted `default: 20` as an integer although the API stores
`"20"` as a string; Validate green (shots/40-apply-real-edit.png, 45, 46).

Examples: loaded "AWS S3 Secure Storage Bucket" then "AWS SQS Queue with Dead Letter Queue", undo twice. At every
step canvas nodes, Inspector parameter list and `/api/blueprint` matched, and `doc.cf.yaml` on disk returned to the
byte-identical file (md5 ad3c6d05 -> b9595fc6 -> 53e48024 -> b9595fc6 -> ad3c6d05). Each starter load rewrote the
served file without any dialog; the header renamed itself "s3-bucket.cf.yaml" as if a different file were open.

One self-inflicted moment worth recording: I clicked × on the installed provider-aws-rds to re-test search. The
engine refused it (409, "still referenced by the blueprint's sources and by resources "instance"") - correct - but
my reflex undo then reverted my entire hand-edit, because the refused action left nothing on the stack. Redo restored
it. The red refusal banner stayed on screen for the rest of the session. (A later "frozen search" scare was my
driver clicking a stale position; verified false and dropped.)

## Findings

### F1 · P1 · False "REQ" badges on nested optional objects, and four different "required" counts
Goal: know which fields I must fill. Observed: with only region + the fields above set, Validate is green, yet the All
view badges 14 rows REQ: `dbSubnetGroupNameRef.name, kmsKeyIdRef.name, masterUserSecretKmsKeyIdRef.name,
monitoringRoleArnRef.name, parameterGroupNameRef.name, passwordSecretRef.key, passwordSecretRef.name,
passwordWoSecretRef.key, passwordWoSecretRef.name, performanceInsightsKmsKeyIdRef.name, region,
replicateSourceDbRef.name, vpcSecurityGroupIdRefs[0].name, writeConnectionSecretToRef.name`, each with placeholder
"required — set a value or wire it". Meanwhile the palette says `Instance 1 req`, the palette hover card says
"155 fields · 1 required", the Inspector header says "155 leaf fields · 13 required". Repro: SOURCES > add
provider-aws-rds > drag Instance > Inspector "All" > scroll. Reproduced twice (01:50 dump, 02:00 re-select).
Evidence: shots/17-text-fields-set.png (vpcSecurityGroupIdRefs[0].name REQ + placeholder),
shots/55-repro-required-filter.png, shots/11-after-drop.png (header), `insp-all.txt`. Report only (repo read-only).

### F2 · P2 · Catalogue search does not find provider-aws-rds by "postgres" (or "sql")
Observed: "postgres" -> only `provider-azure-dbforpostgresql`; "sql" -> aws-dsql + azure-* only; "database" and
"rds" do include provider-aws-rds; "rds" also returns provider-gcp-dns. Repro: SOURCES > type into "Search OSS
providers...". Reproduced twice (01:47 uninstalled, 02:04 installed). Evidence: shots/05-search-postgres.png,
shots/70-repro2-postgres-realbox.png. Report only.

### F3 · P2 · Import silently pins auto-ready as a custom step and logs a 404 on every later load
Observed: before import PIPELINE shows "2. auto-ready inferred default"; after importing the tool's own export it
shows "PIPELINE (1 CUSTOM)" with name/functionRef/package inputs and "Input YAML (uncached):", the blueprint gains a
`pipeline:` block, and the console logs `[API ERROR] 404 /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields
kind not found` (10 occurrences this run, once per load/undo). Nothing in the UI mentions the change. Repro: Generate
> Import > pick out/compositions/xapps.platform.example.org.yaml. Reproduced twice (22:53, 23:01 UTC in events.log).
Evidence: shots/34-after-import.png, shots/36-edit-tab.png, shots/61-repro-second-import.png, `events.log`. Report only.

### F4 · P2 · Boolean fields are free-text boxes
Observed: `autoGeneratePassword boolean` renders as a text input "unset — omitted from output"; only after typing
"yes" does the toast say "(use true or false)". No toggle, no true/false chooser. Repro: any boolean row in the
Inspector. Reproduced on autoGeneratePassword, skipFinalSnapshot, publiclyAccessible. Evidence: shots/18-autogen-row.png,
shots/20-bool-yes.png. Report only.

### F5 · P2 · Envelope wired to a new parameter comes out optional and guarded; existing-param path promises otherwise
Observed: "+ new XRD parameter…" on `writeConnectionSecretToRef.name` creates `connectionSecretName required: false`
and emits `{{- if hasKey $spec "connectionSecretName" }}` around the envelope, so an XR without the parameter gets no
connection secret and nothing says so. The same menu labels the existing-parameter route
"params.connectionSecretName (optional → req)", i.e. it would flip the flag. There is also no "XR name" option.
Repro: Inspector > envelope row > Wire. Reproduced twice (shots/23-envelope-wire.png, shots/58-repro-wire-menu.png;
guard visible in shots/27-param-added2.png drawer and `out.first/compositions/*.yaml`). Report only.

### F6 · P3 · Empty-canvas hint renders SOURCES/KINDS as bold pseudo-links that do nothing
Repro: blank canvas > click the bold "SOURCES" in "2. Add cloud providers in SOURCES" - still on KINDS. Reproduced
twice (01:45, 02:01 after deleting the node). Evidence: shots/03-sources-tab.png, shots/63-hint-sources-click.png.

### F7 · P3 · Val/Wire/Raw buttons on indented rows are clipped at the Inspector's right edge
Observed: for CROSSPLANE ENVELOPE rows and every nested `*Ref.*` row the button group starts at x~1400 and the Raw
button sits at x=1487 on a 1440-wide page. Reproduced twice. Evidence: shots/15-envelope-section.png,
shots/17-text-fields-set.png, shots/56-repro-envelope-wheel.png. (Programmatic scrollIntoView also shifted the whole
panel left, shots/25-new-param-flow2.png; not reproduced with wheel/Tab, so noted only.)

### F8 · P3 · SOURCES error banner never clears
Observed: "delete provider "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0": still referenced by the blueprint's
sources and by resources "instance"" stayed in the SOURCES pane through a starter load, an undo and six searches
(02:02 -> 02:04). Evidence: shots/69-search-probe.png. Reproduced (three reads).

### F9 · P3 · File counts disagree
Header "preview · 4 files" / "written · 4 files" vs ARTIFACTS "7 files" (the extra three are blueprint, package,
rbac previews). Constant throughout; shots/31-after-generate.png.

## Already filed - one line each
- CF-092 confirmed: S3 then SQS starter each rewrote `doc.cf.yaml` (md5 ad3c6d05 -> b9595fc6 -> 53e48024) with no
  dialog; header renamed to "s3-bucket.cf.yaml"/"sqs-queue.cf.yaml" as if a different file. Import behaves the same.
- CF-130 confirmed: the only "environment" text anywhere in the UI is the "+ function-environment-configs" step
  option; no editor for spec.environment.
- CF-119 not reproduced: importing the exported Composition kept `providerName required: true` and
  `connectionSecretName required: false` (the guarded one); unwired `storageGb` is absent, as expected for a
  Composition-only import.

## Recently fixed - one line each
- CF-094 confirmed fixed: `doc.cf.yaml` after the run has no `from: ""` or `: null` lines (grep clean).
- CF-120 confirmed fixed: Apply with `bogusKey` refused in place, API unchanged (grep -c bogusKey = 0).
- CF-122 confirmed usable: ⛶ floats a 720x360 window with editor, Apply and Cancel reachable. Once, the file-tab strip
  inside it was intercepted by the drawer header (engine select / drag handle); not reproduced on retry; Tree works.
- CF-089 confirmed fixed: provider-aws-rds added from SOURCES was still installed after the Edit-tab Apply that added
  provider-aws-s3 by hand; both then appeared in SOURCES and KINDS.

## What worked
- Cached provider Add is instant and shows the digest and kind count; refusing to remove a referenced provider
  (409 with a precise reason) protected the document.
- Drag-and-drop landed first time; the node card lists every set field, so state is legible without the Inspector.
- Invalid boolean rejected with an exact, actionable toast; unknown key in Edit tab rejected the same way.
- Validate in 2 s with a resource count; Generate confirms the path before overwriting.
- Round-trip kept all ten fields, the envelope wire and both parameters; starters + two undos were byte-identical on
  disk and consistent across canvas, Inspector and API every time.
- The envelope is discoverable without the word: red "CROSSPLANE ENVELOPE" heading, the one REQ outside forProvider.
