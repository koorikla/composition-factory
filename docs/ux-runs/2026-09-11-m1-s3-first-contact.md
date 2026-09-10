# UX run — M1 First contact: an S3 bucket, from nothing

Date 2026-09-11 · repo HEAD 6efa086 · port 19210 · driver: throwaway Playwright script
(`driver.js`, chromium headless 1440x900, visible-label/coordinate driving only; no selectors
were read until after the run). Screenshots in `shots/`, browser events in `events.log`.

**Build caveat.** `bin/cf` was rebuilt by another session at 01:46:25, three minutes into the
run (`v0.10.0-69-g1234f14-dirty` → `v0.10.0-101-g64120e5`). My first engine kept the old binary
in memory until I restarted it at ~02:04 for the blank-start re-checks. `web-proto/` (served
live) was last modified Sep 10 13:33, so the UI code was identical throughout; only the Go
side differs between the two phases. Phase is noted where it matters (CF-095, CF-089).

## Mission and outcome

**completed** (build + green Validate + Generate through the UI) — **round-trip degraded**:
the re-imported Composition came back with the same two resources and three wires, but the
`region` parameter silently lost `required` and its default, and Validate went red until I
re-ticked them by hand (see P1-2). Encryption half of the goal also built
(`BucketServerSideEncryptionConfiguration` wired to the Bucket, `sseAlgorithm: AES256`).

**Wall-clock to first green Validate: 6 min 25 s** from engine start (Validate clicked at
6:21, "● valid · 1 resource" at 6:25; Validate itself ~3 s). Full mission with encryption,
Generate and round-trip: ~19 min. Zero uncaught page errors over 90 screenshots; 11 console
errors, all the same 404 (P2-7).

## Narrative

Arrival put a "Choose an Example Blueprint" modal over everything (01). It listed an "AWS S3
Secure Storage Bucket" starter — tempting, but the brief was from nothing, so I closed it.
(It is once-per-browser: two later fresh blank starts did not show it.) Underneath was a clean
canvas: an `XApp` card already carrying `providerName*`, and a hint card "1. DRAG KINDS FROM
KINDS 2. ADD CLOUD PROVIDERS IN SOURCES" (02). That card is the reason I never got lost.

I did what anyone does first: typed `s3`, then `bucket`, into "Search kinds…". Both returned
"No kinds match search query." and nothing else (03, 04) — a dead end that only the hint card
rescued. SOURCES → "Search OSS providers…" → `s3` found `provider-aws-s3` by the word I knew
(06). Add showed "Adding…" and, within a second (cache hit), a toast "Installed
provider-aws-s3:v2.7.0 (schemas loaded) — Open KINDS" (08). Honest feedback; on a cold cache I
would want to see the same spinner survive the wait.

KINDS → `bucket` → `Bucket` first in the list (12). The first drag onto the canvas worked
first time; the hint card vanished and the inspector opened on `region string REQ` with the
placeholder "required — set a value or wire it" (14). The kind tooltip from the row I dragged
past stayed stuck over the XApp card until I moved the mouse.

"+ add field" on the XApp card gave an inline `parameterName` box; `region` → Add (17). I
dragged from the blue dot beside `region` on XApp to the `region` row on Bucket: wire on the
first try, and the inspector said "← params.region ⚠ optional param into required field" (21).
That warning is exactly the nudge a first-timer needs; I ticked `req`, typed default
`eu-north-1`, the ⚠ cleared (23), Validate went green (25).

Then the part the mission did not require but the user goal did: encryption. `encrypt` in
KINDS gave two truncated "BucketServerSideEncr…" rows told apart only by their group headers
(27). I dropped the namespaced one at the right of the canvas; the card is wide and its right
half sat under the inspector (28). The ⌂ button did nothing visible (29); dragging the empty
canvas panned (30). Zooming out with "−" fixed the view for good.

Here the run turned. In the inspector I clicked "Wire" next to `region` and got a box reading
"wire to…". I typed `reg` — and a green wire from
`resources.bucket.status.atProvider.accelerationStatus` appeared instantly (34). I had not
chosen anything. Undo removed it; I redid it one keystroke at a time and the single letter `r`
was enough (38). It is a native `<select>` dressed as a text field, and `r` jumps to the first
`resources.*` option. I fell back to a drag-wire from XApp.region (41), which worked.

Linking the encryption config to the bucket was the slowest step. The SSE card shows no
`bucket` input at all; the Bucket card lists five outputs and `id` is not among them. Dragging
the Bucket by its title onto the SSE just moved the card (46, 47). Only after switching the
inspector from "Required" to "All" did `bucketRef.name string REQ` appear — a field badged
required that the Required filter had hidden (42). Its "wire to…" dropdown had 57 entries and
the one I wanted, "bucket (name / ID)", was 56th, below fifty `status.atProvider.*` paths. I
picked it (49), set `sseAlgorithm` = AES256 (51), and Validate said "valid · 2 resources" (52).

Generate raised a browser confirm "…overwriting existing files. Proceed?" (the out dir was
empty), then "written · 4 files" and the drawer named the real path (53). Four files on disk.

Import accepted only one `.yaml`. I gave it the exported Composition. Resources and wires came
back; `region` came back optional with no default, the ⚠ triangles reappeared on both region
rows, the pipeline showed "PIPELINE (1 CUSTOM)" with a pinned auto-ready step, and no message
said anything had changed (58). Validate: "line 50: resource "bucket" (Bucket): missing
required field "spec.forProvider.region"…" and the same for line 74 (61). I re-ticked `req`
and the default and it went green again (68) — but while repairing, renaming `awsRegion` back
to `region` re-sorted the list under my cursor and my next clicks landed on `providerName`.

## Findings

### P1-1 — "wire to…" commits a wire on the first keystroke
- Goal: wire `region` from the inspector without dragging.
- Observed: with the box focused and empty, pressing `r` immediately created
  `← resources.bucket.status.atProvider.accelerationStatus`; composition grew 52→59 lines with
  `region: {{ … .status.atProvider.accelerationStatus | quote }}`. The control is
  `<select class="tsel" data-wire=…>` with first option "wire to…" (inspector.js:423-424).
- Repro (3/3): select the SSE card → inspector `region` → click **Wire** → click the
  "wire to…" box → type `r`. Shots 34, 38, 39; 31 shows the box before typing.
- Spec steps: `.node[data-id="bucket-server-side-encryption-configuration"]` click →
  `#insp` button "Wire" in the `region` row → `select.tsel[data-wire]` focus → `keyboard.press("r")`
  → expect `doc.resources[1].fields.region` unchanged / no `.wire-path` from `bucket` status.

### P1-2 — Round-trip of the exported Composition silently drops parameter `required`/`default`, Validate goes red
- Goal: re-import what I just exported and keep working.
- Observed: `doc.cf.yaml` diff after Import: `region: default: eu-north-1, required: true` →
  `required: false`; pipeline gained an explicit `auto-ready` step. No toast (toast scan
  returned []). Validate then failed: `line 50 … missing required field "spec.forProvider.region"
  in Bucket spec.forProvider` and `line 74 …` for the SSE.
- Repro: Import performed once (file: `out/compositions/xapps.platform.example.org.yaml`);
  the red Validate reproduced twice (61, 63). Shots 58, 59, 61, 63; `doc.before-import.cf.yaml`
  vs `doc.cf.yaml` diff in this dir.
- Spec steps: build doc with a required+default param wired into a required field → `#generateBtn`
  → `#importBtn` / `#importFile.setInputFiles(composition)` → expect either the param to keep
  `required`/`default` (XRD offered/consumed) or a visible loss notice; `#validateBtn` → `#valid`.

### P2-1 — "Required" filter hides a field badged REQ (`bucketRef.name`)
- Observed: Required view lists `region` and `writeConnectionSecretToRef.name`; All view adds
  `bucketRef.name string REQ`. Filter predicate uses `requiredChain` (inspector.js:627), badge
  uses `required` (:634). Probe: Required→false, All→true, twice each. Shots 31–33 vs 42.
- Spec: select SSE → `#insp button[data-f="req"]` → expect text `bucketRef.name` present.

### P2-2 — Linking two managed resources has no canvas affordance
- Observed: SSE card has no `bucket`/`bucketRef` input row until wired; Bucket card omits
  `id`; dragging the Bucket title onto the SSE only moves it (46, 47). The working route is the
  inspector `<select>` where "bucket (name / ID)" is option 56 of 57 (inspector.js:459).
  2 failed attempts before the select. Shots 41, 46, 47, 49.

### P2-3 — Renaming a parameter re-sorts the list; the row you are editing jumps
- Observed: `region`→`awsRegion` moved the row from 2nd to 1st (65); the reverse rename moved
  it back (order probe `["providerName","region"]`). My next clicks hit `providerName`, leaving
  `eu-north-1` in its default box (66). Shots 65, 66.
- Spec: rename param → expect row index unchanged until blur/re-open, or focus follows the row.

### P2-4 — On a fresh blank start `providerName` is deletable with no confirm (CF-125 gap)
- Observed on two engine restarts with no doc: name input editable, `req` enabled, × titled
  "Delete parameter"; clicking × → "PARAMETERS (0)", no dialog, no toast; undo restores. After
  the first doc edit the row re-renders locked (inspector.js:1203, `isParamLocked`).
  Shots 77–80 (state), 83/84 (delete). Reproduced 2/2.
- Spec: blank `resetDoc` → click XApp → `#insp button[data-param-del="providerName"]` → expect
  disabled, or a confirm, and `PARAMETERS (1)`.

### P2-5 — `writeConnectionSecretToRef.name` badged REQ but not required
- Observed under CROSSPLANE ENVELOPE for both Bucket and SSE: `REQ` + "required — set a value
  or wire it"; Validate passes with it unset (both green runs). Shot 32.

### P2-6 — Kinds search dead-ends without pointing to SOURCES
- Observed: "No kinds match search query." for `s3` and `bucket` (palette.js:208). Shots 03, 04.
- Spec: `#lsearch` fill `s3` with only k8s installed → expect empty state to mention SOURCES.

### P2-7 — Console error on every load of a doc with a pinned auto-ready step
- Observed 11×: `404 GET /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields`
  + `[API ERROR] … kind not found` after Import and on each reload/Validate. Not a pageerror.
  Import also turns "auto-ready inferred default" into "PIPELINE (1 CUSTOM)" (58) unasked.

### P3 (report only)
- P3-1 Inspector scrolls horizontally with long paths; rows read "ule[0].blockedEncryptionTypes" (50, 51).
- P3-2 Card positions are not persisted; reload auto-lays-out and puts the 3rd card half under the inspector (62; also 28 on drop).
- P3-3 ⌂ canvas button did nothing visible with a card out of view (29 vs 28).
- P3-4 Generate confirm says "overwriting existing files" on an empty out dir (events.log dialog).
- P3-5 Two "BucketServerSideEncr…" rows distinguishable only by group header; the `.m.` group carries no "namespaced" label (27).
- P3-6 Kind hover tooltip lingers after a drop until the mouse moves (14).

## Already filed — confirm/refute
- CF-130 (no GUI for spec.environment): **confirmed** — no "environment" string anywhere in the UI text.
- CF-092 (starter overwrites served file, no cue): **confirmed** — Load Blueprint rewrote `doc.cf.yaml` (`name: s3-bucket`), no dialog/toast; top bar just became "s3-bucket.cf.yaml" (70).
- CF-127 (required counts disagree): **confirmed** — Bucket row "1 req", hover "13 fields · 1 required", inspector "5 leaf fields · 1 required"; SSE header "3 required" vs 2 listed under Required (14, 31).
- CF-128 (catalogue entry after install): **confirmed** — "provider-a…" truncated, "INSTALLED · 50 KINDS" badge over the description (08, 10).

## Recently fixed — confirm/refute
- CF-086 SOURCES tracks doc: **confirmed** — installed list showed provider-aws-s3 after Add and after Import (60).
- CF-089 provider survives Edit-tab Apply: **confirmed** (build -101) — provider-aws-iam added from SOURCES, edit → Apply → still listed, still in doc (86–88).
- CF-090 Generate names real dir/files: **confirmed** — "written · 4 files", drawer path = real out dir, 4 files on disk (53).
- CF-091: not exercised.
- CF-095 top bar shows real path: **refuted on build -69** ("untitled.cf.yaml", no title attr, 02/55); **confirmed on build -101** (full path, 85).
- CF-121 kind rename updates plural: **confirmed** — `XBucket` → "xbuckets.platform.example.org" (64).
- CF-124 no false error after param rename: **confirmed** — `region`→`awsRegion`, no toast, template switched to `$spec.awsRegion` (65).
- CF-125 providerName reads as locked: **confirmed after first edit, refuted on the fresh blank scaffold** — see P2-4.

## What worked
- The on-canvas hint card is the single reason a first-timer finds SOURCES; keep it.
- Provider search matched `s3`, install feedback was immediate and named the next step ("Open KINDS").
- First drag to canvas and first port-to-port wire both landed on attempt 1; the "optional param
  into required field" warning and its clearing on `req` is the best moment in the product.
- Undo reliably reverted a wrong wire, a card move and a parameter delete.
- Validate is fast (~3 s) and its error text names line and field; the pill flips back to
  "preview" the instant the doc changes, so a stale green never lied to me.
- Generate names the real output path and the kubectl apply line.
