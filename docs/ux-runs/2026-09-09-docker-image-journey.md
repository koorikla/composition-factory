# J1 — published Docker image journey (ghcr.io/koorikla/composition-factory:v0.10.0)

Setup: fresh named volume + `/workspace` bind mount holding a copy of `internal/examples/sqs-queue.cf.yaml` as `xqueue.cf.yaml`; `serve --blueprint xqueue.cf.yaml --addr 0.0.0.0:8080 --i-know-this-is-unauthenticated` on 127.0.0.1:19110. Driven with headless Chromium by visible labels. Every finding below was observed in at least two independent fresh-instance runs (runA.log, runB.log, runC.js, plus the incremental p1–p6 scripts). Screenshots in `shots/`, full logs in `runA.log` / `runB.log`.

Image facts observed: `/usr/local/bin/cf` only; `which curl crossplane kubectl` → nothing (`wget` and `/bin/sh` exist); process runs as `uid=100(cf)`; server cwd is `/workspace`; `/api/version` → `{"version":"v0.10.0", ..., "outDir":"."}`.

## Findings

### P0-1 — Loading a starter example silently overwrites the user's own blueprint file on disk
Repro: start with `/workspace/xqueue.cf.yaml` = sqs-queue (2012 bytes). Click **Examples** → card "AWS RDS PostgreSQL Database" → **Load Blueprint**. Then Examples → "AWS S3 Secure Storage Bucket" → Load Blueprint.
Observed (host): `xqueue.cf.yaml` 2012 bytes `name: sqs-queue` → 4128 bytes `name: xpostgres` → 2847 bytes `name: s3-bucket`. No confirm dialog, no toast mentioning the file, nothing in `docker logs`. The top bar afterwards reads `blueprints/xpostgres.cf.yaml` / `blueprints/s3-bucket.cf.yaml`, so the user has no cue that `xqueue.cf.yaml` was replaced. For the real user (`-v $HOME:/workspace`) this is a file in `$HOME`.

### P0-2 — Applying an edit in the blueprint editor silently drops a provider source added from the SOURCES catalogue in the same session
Repro: load the S3 example. Palette **SOURCES** → "Search OSS providers…" = `provider-aws-iam` → **Add** (wait for "INSTALLED · 46 KINDS"). Open the drawer tab `s3-bucket.cf.yaml` → **edit** → change `default: eu-north-1` to `eu-west-1` → **Apply**.
Observed: editor text listed only `- provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0` while disk already had both s3 and iam. Host diff after Apply:
```
83d82
<   - provider: ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0
108c107
<         default: eu-north-1
>         default: eu-west-1
```
`curl /api/providers` before Apply: s3 + iam; after Apply: `{"providers":[{"ref":"ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0",...}]}` only. The SOURCES tab still shows `INSTALLED PROVIDERS 3 … provider-aws-iam:v2.7.0 sha256:5883bfa8a85c · 46 kinds … INSTALLED · 46 KINDS`. No error, no toast. (Same root cause as the stale SOURCES cache: the client's blueprint document is not refreshed after `POST /api/providers`, and Apply pushes the stale document over the server's.)

### P0-3 — Generate toast states the output location incorrectly and gives an incomplete apply command
Repro: with the S3 example loaded, click **Generate** → accept the confirm.
Observed toast: `✓ Output written to compositions · Apply: kubectl apply -f compositions · Package: cf package`. Chip: `written · 4 files`. Files actually written (host, mount root): `./compositions/xbuckets.storage.sparky.ee.yaml ./xrds/xbuckets.storage.sparky.ee.yaml ./functions.yaml ./providerconfigs/aws.yaml`. Three of the four files are outside `compositions/`, and `kubectl apply -f compositions` would apply the Composition without its XRD or functions. (For the real user these land in `$HOME/compositions`, `$HOME/xrds`, `$HOME/functions.yaml`, `$HOME/providerconfigs/aws.yaml`.)

### P1-4 — After fixing the missing provider from the UI, the top bar keeps saying `error` / "not in the cache" and the composition stays empty until some unrelated edit or a reload
Repro (fresh cache): first load shows chip `error`. Palette **SOURCES** → paste `ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0` into the `ghcr.io/…/provider-x:vN` box → **Add**. Wait 6 s.
Observed: SOURCES now lists `provider-aws-sqs:v2.7.0 sha256:2cfadf9f941f · 8 kinds`; `curl /api/providers` lists it; but top bar still `error`, chip title still `provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0" is not in the cache; run: cf provider add …`, composition pane still `0 lines · deterministic`. Network after Add: `POST /api/providers, GET /api/providers, GET /api/kinds` — no `POST /api/generate`. Clicking the XQueue node: unchanged. Pressing **Validate**: chip becomes `validation check unavailable`, pane still `0 lines`. Only an inspector edit (`PUT /api/blueprint/parameters/region` → `POST /api/generate`) or a page reload turns it into `preview · 4 files` / `86 lines · deterministic`. Nothing tells the user a regenerate is needed. (The toast's remedy `run: cf provider add …` is a CLI the Docker user has no shell for; the SOURCES box is the only in-UI path and is not pointed to.)

### P1-5 — The top bar and drawer show file names that do not exist on disk
Repro: first load; then load the S3 example; then Generate.
Observed: top bar `blueprints/sqs-queue.cf.yaml v1alpha1` (disk: `/workspace/xqueue.cf.yaml`; there is no `blueprints/` directory), later `blueprints/s3-bucket.cf.yaml`; drawer tab `s3-bucket.cf.yaml`; drawer path label `compositions/s3-bucket.yaml` while the written file is `compositions/xbuckets.storage.sparky.ee.yaml`; artifacts tree `blueprint.cf.yaml`. The real path (`xqueue.cf.yaml`, given on the command line) appears nowhere in the UI.

### P2-6 — Validate's fix tip is not actionable in the published image
Repro: click **Validate**.
Observed chip `validation check unavailable`, title/drawer: `crossplane CLI not found on PATH: exec: "crossplane": executable file not found in $PATH` + `💡 Environment Fix Tip: Install Crossplane CLI: curl -sL https://raw.githubusercontent.com/crossplane/crossplane/master/install.sh | sh`. The image has no `curl` (`which curl` → exit 1), runs as uid 100, and the user is on the host, not in the container; nothing says "not available in the Docker image".

### P2-7 — The blueprint file on disk is rewritten with serializer noise that the editor tab does not show
Repro: load any example (or add a provider from SOURCES).
Observed (host `xqueue.cf.yaml`): 30 lines matching `conventions: null | from: "" | raw: "" | template: "" | templates: null`, e.g.
```
spec:
  conventions: null
  resources:
  - fields:
      objectLockEnabled:
        from: ""
        raw: ""
        template: ""
```
The drawer's `s3-bucket.cf.yaml` → **edit** view shows a clean form (`spec:\n  sources:\n    - provider: …`) that differs from the file, so what the user reads in the UI is not what is in their directory.

### P2-8 — `.cf.lock` in the mount root keeps providers the current blueprint no longer references
Repro: add sqs from SOURCES, load RDS example, load S3 example.
Observed `.cf.lock` refs: `provider-aws-rds:v2.7.0, provider-aws-s3:v2.7.0, provider-aws-sqs:v2.7.0`; blueprint on disk `sources:` = `provider-aws-s3:v2.7.0` only. Written to `$HOME/.cf.lock` for the real user without any UI mention beyond the footer "Pinned by digest in .cf.lock".

### P2-9 — Generate never tells the Docker user where files land
Repro: hover/click **Generate**.
Observed: button title `Write generated manifests to . (overwrites existing files)`; confirm dialog `Generate will write manifests to disk in '.', overwriting existing files.\n\nProceed?`; `/api/version` `"outDir":"."`. "." is `/workspace` inside the container = whatever the user mounted (`$HOME` in the observed setup); neither path is shown.

### P3-10 — Artifacts panel and drawer tabs claim files while generation failed
Repro: first load with the uncached provider.
Observed simultaneously: chip `error`, `POST /api/generate` → 400, composition pane `0 lines · deterministic`, and `ARTIFACTS 6 files` with tabs `composition.yaml definition.yaml functions.yaml sqs-queue.cf.yaml package.yaml rbac` and banner `1 field use a raw template — the canvas can show them but not validate them. missingkey=error still guards them.`

### P3-11 — The server logs nothing after startup, including writes to the mounted directory
Repro: run the whole journey, then `docker logs cf-j1`.
Observed: exactly 2 lines for the entire session (below). Provider pulls from ghcr.io, example loads overwriting the blueprint, `.cf.lock` writes, Generate writes and editor Applies all produce no log line.

## Docker logs (complete, identical in every run)
```
cf: warning: provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 — continuing without it; schemas load on demand
cf serve: listening on http://[::]:8080
```
Nothing in stderr is hidden from the UI: the same sentence is shown as a toast, as the `error` chip title and in the drawer. The console additionally shows `[API ERROR] 400 /api/generate provider "…" is not in the cache; run: cf provider add …`.

## Known items — confirm/refute
- (a) Confirmed: after loading RDS the SOURCES tab lists `provider-aws-rds:v2.7.0 · 44 kinds`; after loading S3 it still lists rds while `/api/providers` returns only `provider-aws-s3:v2.7.0` (50 kinds); a reload shows s3. Same staleness after P0-2 (tab shows 3 providers, API 1).
- (b) Confirmed: stderr warning at startup exactly as quoted above; the generate cycle returns 400 with `provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0` and the UI surfaces it (toast + chip + drawer). Not stderr-only.
- (c) Confirmed: chip `validation check unavailable`, `/api/render` → `{"ok":false,"resources":0,"error":"","unavailable":"crossplane CLI not found on PATH: exec: \"crossplane\": executable file not found in $PATH"}`.

## What worked
- Container starts and serves the canvas in ~2 s; no uncaught page errors (`pageerror`) in any run; the only failed request is the expected first-load 400.
- Provider pulls from ghcr.io work from inside the image with no credentials: sqs (8 kinds), rds (44), s3 (50), iam (46), each 1–2 s. `/home/cf/.cache/compositionfactory/<provider>-<hash>/crds.json` populated.
- Catalogue search returns `provider-aws-iam ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0` with an **Add** button; while fetching the button turns into a spinner `Adding…`; afterwards the row shows `INSTALLED · 46 KINDS` and the KINDS palette gains `IAM.AWS.M.UPBOUND.IO` (Role, RolePolicy, …).
- Example loads render immediately (`74 lines` RDS, `88 lines` S3) with chip `preview · 4 files` and the banner `Preview only · Click Generate to write to compositions · Package: cf package`.
- Validate degrades gracefully (no crash, clear "unavailable" state) and Generate asks for confirmation, writes 4 valid files, and the chip flips to `written · 4 files`.
- The blueprint editor tab (`edit` → **Apply**) goes through the parse gate, updates the inspector (`eu-west-1`) and persists to disk; the change survives a reload.
- Reloading the page always resynchronises the UI with server state (chip, composition, SOURCES).
