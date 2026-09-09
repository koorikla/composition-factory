# UX run — M1, first contact: an S3 bucket from nothing

**Date** 2026-09-04 · **Build** `v0.9.0-40-g0d3e914` · **Engine** `cf serve` on 127.0.0.1:8090,
blank start, `.testrun-ux/` · **Viewport** 1280×720

**Outcome** `completed with workaround`. Wall-clock to first green Validate: ~14 minutes,
of which ~4 were spent on a failure whose cause the interface never named.

**Driver** the in-app Browser pane (`mcp__Claude_Browser__*`), plus a throwaway Playwright
script for the screenshots in [`2026-09-04-m1-first-contact/`](2026-09-04-m1-first-contact/).
Two steps could not be driven by a real pointer and were synthesised the way
`tests/slice1-core-loop.spec.js` does — see [Driver limits](#driver-limits). Those two steps
carry no hesitation data, so no P2 in this report rests on them.

This run was accompanied by three static audits that did not touch the running engine:
visual design system, accessibility/keyboard, and interface copy. Their findings are folded
in below and attributed.

---

## Narrative

The canvas opens with four things competing for attention: a palette on the left, a card
already on the canvas, an Inspector on the right already showing that card's parameters, and
a drawer along the bottom already showing generated YAML. Sitting over the canvas is a
panel headed **1. DRAG KINDS FROM KINDS 2. ADD CLOUD PROVIDERS IN SOURCES** — a run-on that
merges two numbered steps into one line, and then repeats both of them in full underneath.
I read the long version and ignored the heading.

Step 2 first, since I needed AWS. **SOURCES** was the third of four tabs, rendered as
`PROVID.. FUNCTI.. CLUSTER` — every label clipped. Under it, an input reading
`ghcr.io/.../provi` and a **Providers Catalogue** search. I typed `s3`.

Nothing happened. The panel kept saying *Type to search the catalogue.* I waited, decided I
had mistyped, and was about to retype when three results appeared. The catalogue search is
slow enough to need a spinner and does not have one — and the empty-state copy stays on
screen while it works, which actively tells you nothing was found.

The three results all rendered as `provider-aws-…`. Identical. The full ref is underneath in
small grey type, wrapped across three lines, so I could tell them apart by reading the
version string — but the primary label, the bold one, was the same six characters on all
three. I tried to widen the column by dragging its edge. The drag selected text in the YAML
viewer instead; the palette is not resizable.

**Add** on the first one gave me `Adding…` with a spinner, then `INSTALLED · 50 KINDS` about
five seconds later. That is the best feedback in the app. The onboarding panel, meanwhile,
still told me to add cloud providers in SOURCES — it does not notice that you have.

Over to **KINDS**, searched `bucket`, and got a list where every row was truncated to a
common prefix: `BucketAnaly…`, `BucketCorsC…`, `BucketIntel…`, `BucketInven…`, and two rows
that both read `BucketObject…`. Forty-six matches, nine visible at a time, most of them
distinguishable only by their invisible tails. AWS CRD kind names are long by nature and the
palette is 179 px wide.

I clicked `Bucket`. Nothing. Clicked again. Nothing — no selection, no hint, no error. The
row is drag-only, and the interface never says so. That is a plain three-strike moment: the
first thing a person does with a list of things they want is click one.

Dragging it on worked, and from there the tool is good. The card sized itself to its content,
the Inspector filled with `region` marked required and carrying its CRD description, and the
YAML in the drawer updated. Clicking the **XApp** card switched the Inspector to the XRD
panel — the mental model is clean.

**+ Add parameter** created a parameter named `newParam` and left focus on `<body>`. I
clicked into the name field, typed `region`, and pressed Enter. The field showed `region`.
The card still said `newParam`. So did the document. Enter does not commit; only blur does,
and until you blur, the panel and the canvas disagree with each other with nothing marking
which one is real. I reproduced it a second time with `bucketPrefix` and read the document
back over HTTP to be sure.

Dragging the `region` dot onto the Bucket card bound it in one gesture and drew the wire —
no picker, no ceremony, because the names matched. Genuinely nice.

Then **Validate** went red:

```
line 47: resource "bucket" (Bucket): missing required field "spec.forProvider.region" in Bucket spec.forProvider
```

This is where the four minutes went. The field is not missing. It is wired, the wire is drawn
on the canvas, the Inspector shows `+ params.region`, and the port's required marker is
satisfied. The message names a line number in a file I did not write, and a field I can see
is set.

The actual cause is upstream and invisible: `region` is an *optional* XR parameter, so the
emitter guards it — `{{- if or (hasKey $spec "region") }}` — and the sample XR the render
runs against does not set it, so the guard omits the field and the Bucket comes out invalid.
The fix is one checkbox, `req`, on a different object in a different panel. I found it by
knowing the emitter's guard rule from the project's own research notes. A user without that
knowledge has an error telling them to set a field that is visibly already set.

With `req` ticked, the binding became unguarded and Validate went green: **render ok · 1
resource**. Total build, once past that: about three minutes.

One last thing on the way out. The chip that had just said `render ok · 1 resource` was
back to `ok · 4 files` on the next reload, and `ok · 4 files` is what it says after every
keystroke — for a preview that writes nothing to disk. The banner two hundred pixels below
it says *Preview only · Click Generate to write*, and that banner is hidden entirely when the
drawer is collapsed.

### Where I left the script

I did not run export → round-trip. Two of the three remaining phases depend on the
drag-and-drop path I could not drive with a real pointer, and I judged the run's remaining
value to be higher spent verifying the three static audits' load-bearing claims against the
live engine than completing a mission on synthesised events. M4 (adopt/round-trip) is the
mission that should run next, and it is the one with the most P0-shaped risk — see CF-051.

---

## Findings

Severity is the `canvas-ux-tester` scale: **P0** lost work, impossible, or the interface
states something false · **P1** completable only with knowledge that exists solely in the
source · **P2** completable, wastefully · **P3** polish.

`[V]` marks a claim I verified a second time, independently of the run or audit that first
surfaced it. Attribution in brackets names which audit found it; unattributed findings are
from this run.

### P0

**F1 — Package saves the server's error response as the user's `.xpkg`.** `[V]` [copy audit]
`web-proto/js/main.js:521-533` is the whole handler: it creates an `<a href="/api/package"
download>` and clicks it. There is no `fetch`, no status check, no error path. With
`spec.emit.templateSource: FileSystem`, the endpoint returns **400 with
`Content-Type: application/json` and no `Content-Disposition`** — verified against the live
engine:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
{"error":"cannot package a blueprint with spec.emit.templateSource: FileSystem (…switch to templateSource: Inline or use cf gen)"}
```

The browser saves that JSON to disk under a name derived from the URL. The user has a file
they believe is their package and no indication anything failed. Same shape for the 400 from
`emit.Generate` and the 500 from `xpkg.Build`. → **CF-049**

**F2 — The core loop is pointer-only; a keyboard user cannot place a kind or select a card.**
`[V]` [a11y audit] Palette rows are `<div class="kind" draggable="true">` with no `tabindex`,
no `role`, and no focusable descendant — confirmed live against the running app: **46 rows,
0 tabbable, 0 with a role, 0 focusable children.** The only code that appends a resource is
`onDrop` (`canvas.js:1724`), reachable solely from the native drop event. Canvas cards are
likewise plain `<div>`s, and `proto.css:605-606` `display:none`s their two real buttons until
the card is already selected — so an unselected card contains zero tabbable elements and the
Inspector is permanently pinned to the XRD panel.

This also excludes touch: the only `touchstart`/`touchmove` handlers in the codebase are on
the canvas wrapper (`canvas.js:1753-1796`) for pan and zoom. On a tablet you can pan and zoom
a canvas you can never put anything on. → **CF-050**

**F3 — The record of what adoption dropped self-destructs after 12 seconds.** [copy audit]
`reportAdoptLoss` (`main.js:494-501`) passes the loss list to `notice()`, which ends
`setTimeout(function () { bar.hidden = true; }, 12000)`. This is the only place the user is
ever told that parts of an imported Composition did not survive — e.g. *"adopted with 3
dropped items: resource.db.connectionDetails (connectionDetails are not supported in
blueprint); …"*. It is not persisted, not repeated in the blueprint tab, not recallable.
Alt-tab during the import and you return to a canvas that looks complete and is not. This is
the Round-Trip Rule failing where the user can see it least. → **CF-051**

**F4 — A failed "Load Blueprint" closes the modal, which reads as success.** [copy audit]
`main.js:667-669` calls `closeModal()` in the `.then`, and `store.loadExample` resolves `null`
on failure rather than rejecting (`store.js:326-338`), so the failure path closes the modal
too. The `.catch` at `main.js:670-671` is unreachable dead code. The user clicks Load, the
modal closes, the canvas is unchanged, and the only signal is a toast that expires in six
seconds carrying a raw Go string. → **CF-052**

**F5 — The status chip says `ok · N files` for a preview that wrote nothing.** [copy audit]
`chipOk` (`output.js:528-532`) renders identically for `store.generate(false)` — the debounced
preview that runs on every doc edit and writes nothing — and `store.generate(true)`, which
writes to disk. The only differentiator is the drawer banner (`output.js:431-440`), which
`output.js:919` hides entirely when the drawer is collapsed. Observed live on a document that
had never been generated: chip green, `ok · 4 files`, banner *Preview only*, `.testrun-ux/out`
empty. → **CF-053**

**F6 — Renaming a parameter does not commit on Enter.** `[V, reproduced ×2]` Type a name into
the Inspector's parameter-name field and press Enter: the input shows the new name, the canvas
card and the document keep the old one, `document.activeElement` is still the input, and
nothing marks the edit as uncommitted. Read back over HTTP immediately after Enter:

| | run 1 | run 2 |
|---|---|---|
| Inspector input | `region` | `bucketPrefix` |
| Canvas card | `newParam` | `newParam` |
| `spec.xrd.parameters` | `newParam` | `newParam` |

Blur commits. A user who types a name, presses Enter and clicks **Generate** ships
`newParam`. → **CF-054**

### P1

**F7 — An optional parameter wired into a required provider field renders invalid, and the
error blames the field.** `[V]` Full repro in the narrative above. Observed message:

```
line 47: resource "bucket" (Bucket): missing required field "spec.forProvider.region" in Bucket spec.forProvider
```

Observed state at that moment: wire drawn on canvas, Inspector showing `+ params.region`, the
port's required marker satisfied. Emitted template: `{{- if or (hasKey $spec "region") }}`.
Ticking `req` on the parameter changes the emission to `region: {{ $spec.region | quote }}`
and Validate goes green — `render ok · 1 resource`.

The cause is a property of the *parameter*; the message is addressed to the *resource field*,
by a line number in generated YAML. Nothing in the interface connects the two, and the canvas
positively signals that the requirement is met. The knowledge needed to resolve it — the
`hasKey`-not-`with` guard rule for optionals — exists only in `docs/research/`.

The fix must achieve: an optional parameter bound to a required field is flagged at bind time,
on the object that can be changed, before Validate is ever pressed. → **CF-055**

**F8 — "+ Add parameter" ships a parameter literally named `newParam`.** `[V]` The button
creates the parameter immediately with that name and leaves `document.activeElement` on
`<body>` — verified live. `newParam` is a valid name, so it emits into the XRD unchallenged.
The palette's Shared tab has a proper form for the same action with a `parameterName`
placeholder and explicit Add/Cancel; the two entry points behave differently. → **CF-056**

**F9 — Generate overwrites files on disk and nothing says so beforehand.** [copy audit]
Tooltip (`index.html:33`): *"Regenerate composition.yaml and definition.yaml from the blueprint
now"*. The handler writes every output path in `srv.OutDir` with `os.WriteFile(…, 0o644)`, no
confirmation and no backup, including when the directory already holds another blueprint's
output. The word "write" appears only afterwards, in a banner, and only if the drawer is open.
→ **CF-057**

**F10 — Adding a provider gives no progress and freezes the whole server.** [copy audit] The
manual add path (`palette.js:758-772`) only sets `disabled = true` — no label change, no
spinner, while the catalogue path two hundred lines away does show `Adding…`. Meanwhile
`handleAddProvider` holds `srv.mu` across the entire OCI fetch
(`internal/api/providers.go:137-157`), and generate, blueprint PUT, kinds, package and render
all take the same lock. During a multi-megabyte CRD pull the canvas silently stops accepting
work. Slow is expected here and is not the finding; silent is. → **CF-058**

**F11 — Raw Go errors and HTTP statuses reach the user verbatim.** [copy audit] Three
confirmed sources: `api.js:49` surfaces `network error: Failed to fetch`; `api.js:60` falls
back to a bare `502 Bad Gateway`; `inspector.js:305` shows the literal string `operation
failed`. Server messages arrive addressed in the blueprint's coordinate system rather than the
canvas's — `spec.resources[3] "…": invalid resource name` after the user renamed a card.
The pattern to copy already exists in this codebase: `diagnoseError` (`output.js:368-399`)
keeps the verbatim server text and appends an actionable fix. → **CF-059**

**F12 — The dark theme's `--faint` was never re-derived; 30 AA failures, including the
Generate button at 2.69:1.** `[V — token collision re-checked by hand]` [design audit] Every
ink token inverts between themes except `--faint`, which moves from 45.1% to 45.7% lightness:
`#67727F` → `#6A747F`. It is the colour of every secondary affordance in the app and lands at
**3.64:1 on `--surface`** (needs 4.5), 3.36:1 on `--surface-2`, 3.12:1 on `--shared-soft`,
across sixteen selectors. Separately, `#fff` is hardcoded against themed accents that were
lightened for dark but never revisited — `.btn.pri`, the **Generate** button, is **2.69:1**;
the fan-out count badge `.fan` is **2.16:1** at 9 px.

Measured totals: **17 AA text failures in light, 30 in dark, 10 UI-boundary failures in each**,
computed twice by two independent sRGB implementations. Every value is inherited unchanged
from `docs/design/canvas-prototype.html` — **zero token drift across all 27 tokens** — so the
prototype must be fixed alongside `proto.css` or the drift reopens. → **CF-060**

**F13 — IBM Plex is loaded only from Google's CDN, in an app built to run offline.**
[design audit] `index.html:7-9` is the only source of the typeface: no `@font-face`, no
vendored file anywhere in the repo. `web-proto/embed.go` embeds the whole frontend into the
binary precisely so `cf serve` needs no network, and `docs/mcp.md:58` says everything but
provider fetch "works entirely offline." The failure is not graceful: `--cond` falls back
through normal-width faces, and the four palette tabs it sets live in a fixed column with
`overflow:hidden;text-overflow:ellipsis` (`proto.css:88-89`) — they clip. That this rule
already shrank from the prototype's `10px/.06em` to `9.5px/.02em` is evidence the condensed
metrics are load-bearing for fit. → **CF-061**

**F14 — The guide and the tour name affordances that do not exist.** [copy audit] The output
drawer has four names in four places: `YAML` (button, `index.html:24`), `Editor` (its own
label, `index.html:92`), *the output drawer* (`tour.js:76`), and *the **Generated** drawer*
(`docs/guide.md:13`) — which matches nothing on screen. `docs/guide.md:34` says Reset View
"centers the active blueprint"; `canvas.js:668` sets `x=0; y=0; k=1`, so a user whose cards are
at x=2000 presses ⌂ and gets an empty screen. `tour.js:52` calls the mode buttons `V/W/R`;
they are labelled `Val`, `Wire`, `Raw`. → **CF-062**

### P2

**F15 — At 1280×720 the palette is 179 px and truncates every long kind name to the same
prefix.** `[V]` Measured live: `#region-palette` 180 px wide, its scrollable list
`#lrail` 179×277 — nine rows of forty-six. `bucket` returns `BucketAnaly…`, `BucketCorsC…`,
`BucketIntel…`, `BucketInven…` and two rows both reading `BucketObject…`. Catalogue results
are worse: three matches for `s3`, all rendering as `provider-aws-…`, distinguishable only by
the wrapped grey ref beneath. The column is not resizable — dragging its edge selects text in
the YAML viewer instead. Long CRD kind names are the normal case for upjet providers, not the
exception. → **CF-063**

**F16 — The output drawer takes 250 px of a 720 px viewport to show 142 px of YAML.**
`[V]` Measured live at 1280×720: topbar 46, columns 424 (59%), drawer 250 (35%). Inside the
drawer, `#code` is 142 px — about seven lines of a ~500-line document, 27% of the visible
file — so 43% of the drawer is its own chrome. The canvas, the primary work surface, gets
840×424. An authoring tool is giving more vertical space to a read-only preview than to the
palette that feeds it. → **CF-064**

**F17 — Clicking a palette kind row does nothing, and nothing says drag is required.** `[V]`
The delegated click handler at `palette.js:696` has thirteen `closest()` branches and none
matches `.kind`. Observed live: clicking `Bucket` produces no selection, no hint, no error.
The hint bar underneath says *"Drag a kind onto the canvas"* — but it is below the fold of a
277 px list, so it is not on screen when the rows are. → **CF-065**

**F18 — The button says Validate, every result says "render", and then the generate chip
erases it.** [copy audit] `Validate` (`index.html:32`) → `rendering…` / `render ok · N
resources` / `render error` / `render check unavailable` (`output.js:740-754`). Nothing in the
result repeats the word the user pressed. Worse, the same element is the generate status chip:
a successful `render ok · 1 resource` is overwritten by the debounced preview 300 ms after the
next keystroke, so the validation result cannot be read back. Observed both live. → **CF-066**

**F19 — The light-theme code viewer fails AA on four of five syntax colours.** [design audit]
All on `--sunk` `#D8E0EA`: template `3.55:1`, shared `3.55:1`, comment `3.68:1`, key `4.46:1`;
only strings pass at 4.77. All five pass in dark (6.87–9.74:1) — the light theme is the one
that was never measured. This is the pane the whole tool exists to produce. The same
`--wire-xrd`-on-`--sunk` 4.46:1 pair also breaks the raw-expression editor and the selected
file row in the artifact tree. → **CF-067**

**F20 — Wire hit targets are a 2.25 px stroke and parameter dots are 7×7 px.** `[V]`
Measured live from the running app: the three XR parameter dots are **7×7 CSS px, stacked 21
px apart** — so WCAG 2.2 SC 2.5.8's 24 px spacing exception does not apply either. Wires are
selected by `pointer-events:stroke` on a `stroke-width:2.25` path. Both `onCwClick`
(`canvas.js:798`) and `onContextMenu` (`canvas.js:957`) look for a `.wire-hit` element that
`drawWires` never emits and that appears in no template — whatever fat invisible hit path
those selectors were written for does not exist. → **CF-068**

**F21 — Nothing announces a Validate or Generate result, and the error text is hover-only.**
[a11y audit] A repo-wide grep for `aria-live` returns nothing. The state chip is a `<span>`
mutated with `textContent` (`index.html:21`, `output.js:740-754`); the full render error lives
only in its `title` attribute, which most screen readers do not announce for a non-interactive
span and which a keyboard user cannot summon at all. The chip is also given `cursor:pointer`
and a click handler without being made focusable. → **CF-069**

**F22 — `--shared` and `--warn` are the same hex, so "shared binding" and "warning" are the
same colour.** `[V]` [design audit] Verified by hand in `proto.css`:

```
:10  --shared:#877200;      :12  --warn:#877200;      /* light  */
:26  --shared:#D6AB33;      :28  --warn:#D6AB33;      /* dark   */
```

Contrast between them: 1.00:1. In the generated-YAML viewer, `.code .tm` (a template token)
and `.code .sh` (a shared binding) are two different facts about a composition rendered in the
identical colour — while the canvas legend at `index.html:63` actively teaches the user that
this colour means "shared". Selection state reuses `--warn` as a fourth meaning
(`proto.css:136`). → **CF-070**

### P3

**F23 — Design-token hygiene.** [design audit] `#7c3aed` — hue 262.1°, saturation 83.3° —
ships as the CRON card colour in the Examples modal (`internal/examples/examples.go:105`,
painted inline at `main.js:577`) `[V]`; it is the only violet in the served asset graph, and
it breaches the project's own stated rule. Five `var()` names are referenced but never defined
(`--dim`, `--accent`, `--pri`, `--panel`, `--fg`), which silently discards the intent rather
than erroring — and makes the tour card render near-black in the light theme. There are 14
distinct font sizes across 8–18 px (nine effectively single-use) and 23 distinct spacing values
(17 off a 4 px grid) with **no type or spacing tokens at all**; 288 inline `style=` attributes
in JS carry most of them, which is why none of this is visible to a review of `proto.css`.
→ **CF-071**

**F24 — Interface copy.** [copy audit] Implementation words in the interface: *envelope* (the
user is setting a ProviderConfig, a connection secret or a deletion policy), *emission engine*,
*fan-out*, *the doc*, *the chip*, *the rail*, *from the cache*. The starter blueprints have
four names — `Examples`, `Starter Blueprints`, `Choose an Example Blueprint`, `Load Blueprint`
— and pressing `Import` produces results that say *adopt failed* / *adopted with N dropped
items*. The canvas empty state claims **14 native Kubernetes kinds** `[V]`; `nativeKinds` has
**16** entries, and the palette on the same screen reads `16 kinds`. `No kinds match.` is shown
when nothing has been searched. The Providers Catalogue returns functions because the type
filter is not passed. Two Inspector empty states are dead ends — *"No schema found for kind
X."* offers nothing, while the server-side equivalent already says `run: cf provider add %s`.
→ **CF-072**

---

## What worked

Worth naming, so it does not get regressed:

- **Catalogue add feedback.** `Adding…` with a spinner, then `INSTALLED · 50 KINDS`. It is
  the clearest moment in the app and it covers the slowest operation.
- **Name-matched auto-bind.** Dragging the `region` dot onto the Bucket card bound it to
  `spec.forProvider.region` in one gesture, no picker. Cards also size to their content,
  so real CRD field names are readable on the canvas even though they are not in the palette.
- **The XR card as the XRD's Inspector.** Clicking the XApp card switches the right panel to
  the definition. One mental model for two very different objects, and it holds.
- **`diagnoseError`** (`output.js:368-399`) keeps the verbatim render failure and appends
  *"Install Crossplane CLI: …"* or *"Make sure Docker Desktop or dockerd is running"*. This is
  exactly the pattern F11 asks for everywhere else.
- **Destructive confirms that name what is lost.** *`Remove "bucket"? Wired fields will be
  dropped: …`* and *`Parameter "x" is wired into 6 fields. Delete it?`* — specific, not
  "Are you sure?". (The Shared tab's parameter delete is the one that skips the fan-out check.)
- **Wires are focusable and named.** `tabindex="0" role="button" aria-label` on every wire
  path (`canvas.js:558`) is the app's one strong non-visual affordance.
- **The Examples modal and the Tour** both record the previously focused element, trap Tab in
  both directions, handle Escape, and restore focus. The pattern is already in the codebase;
  the wire picker and context menu just do not use it.

---

## Driver limits

Two steps could not be driven with a real pointer and were synthesised:

1. **Palette → canvas drop.** HTML5 drag-and-drop does not fire from synthetic mouse events.
   Dispatched `DragEvent('dragstart')` + `dragover` + `drop` with a shared `DataTransfer`, the
   same way `tests/slice1-core-loop.spec.js:74` does.
2. **Screenshots.** Captured by a throwaway Playwright script against the already-running
   engine on 8090. It binds no port of its own and does not touch the e2e range.

Everything else — provider search and add, kind search, card selection, parameter add and
rename, drag-to-wire, Validate — was driven as a pointer and keyboard.

**Not filed, for the record:** I believed for a while that filtering the kind list left it
scrolled past its own first results, hiding the exact match. One observation supported it;
two attempts to reproduce it did not, and the JS probe I used to measure it was reading a
stale `scrollHeight` and the wrong input. Dropped. It may still be real under some sequence I
did not find.

## Not yet done

`canvas-ux-tester` §5 requires a failing spec for every P0 and P1, and for any P2 with a
deterministic repro. **None are written** — this run was scoped to the report and the backlog.
The findings that most need one first, with their anchors:

| Finding | Anchor |
|---|---|
| CF-054 rename does not commit on Enter | `#insp input.tin.bold`, then `GET /api/blueprint` |
| CF-055 optional param → required field | `#validateBtn` / `#valid`, assert green after wiring alone |
| CF-053 preview chip claims files written | `#valid` text vs `--out` directory contents |
| CF-049 package error saved as a file | `/api/package` status vs what the click produces |
| CF-050 keyboard cannot add a kind | tab order over `#lrail .kind`, then Enter |
