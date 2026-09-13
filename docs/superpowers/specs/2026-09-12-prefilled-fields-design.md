# Pre-filled fields, essentials form and manifest editor — design

Date: 2026-09-12. Status: implemented on branch prefill-fields.

## Problem

Dropping a Deployment onto the canvas scaffolds only `spec.selector.matchLabels` and
`spec.template.metadata.labels` (CF-439, landed 61c2b4d). The container block is gated
behind the inspector's "Scaffold Required Fields" button, so a freshly dropped Deployment
generates a manifest with no containers. StatefulSet, DaemonSet, Job and CronJob do get a
container on drop; Deployment is the odd one out.

The scaffold writes maps as raw JSON (`{raw: "{\"app\":\"web\"}"}`), so Generate warns that
those fields bypass schema validation even though the DSL has a validated map-entry form
(`spec.selector.matchLabels[app]: {value: web}`). The image placeholder is `nginx:latest`,
which both Kubernetes references consulted (the installed `k8s-manifest-generator` skill and
LukasNiessen/kubernetes-skill) list as a hard don't.

Setting inputs in the inspector is inconvenient. The "All" view of a Deployment lists 848
leaf rows; in the pane's width their paths truncate to `s…` and `spec…`. The Required view
tells the user to find members "via All / search". The existing "Workload Selectors & Pod
Spec" card is a good idea limited to four kinds and four fields.

## Decisions (with Kaur, 2026-09-12)

1. Starter depth: good-practice minimum. Selector, labels, one container with a pinned
   image tag, containerPort, resource requests and a memory limit, replicas. No probes or
   securityContext on drop.
2. Values are literals. Each essentials row carries a one-click "expose as parameter"
   action that creates an XRD parameter with the literal as its default and wires the field.
3. Inspector direction: an essentials form per kind on top; the field list demoted.
4. The demoted bottom is a manifest-style YAML editor, not the 848-row list.
5. Approach: kind profiles in the frontend, a Go endpoint for the manifest round trip with
   schema validation, and the old field list kept behind a Manifest / Fields toggle. 28 of
   176 Playwright specs drive the old list; deleting it buys nothing for the user.
6. Kaur implements via this session in a worktree, three landable slices.

## 1. Kind profiles and starters

`web-proto/js/profiles.js` exports one profile per native kind. A profile is
`{ starter(name) -> fields, essentials: [...] }`. Drop (`canvas.js onDrop`), the Scaffold
button (`inspector.js scaffoldResource`) and the essentials form read the same profile, so
the three can never disagree. `scaffoldResourceFields` keeps its generic schema-driven branch
for provider kinds and loses the hand-written native blocks and the `isExplicit` gate.

Deployment starter (StatefulSet and DaemonSet share it; StatefulSet adds `spec.serviceName`):

```yaml
spec.replicas:                                                   {value: "2"}
spec.selector.matchLabels[app]:                                  {value: <name>}
spec.template.metadata.labels[app]:                              {value: <name>}
spec.template.spec.containers[0].name:                           {value: <name>}
spec.template.spec.containers[0].image:                          {value: nginx:1.27}
spec.template.spec.containers[0].ports[0].containerPort:         {value: "80"}
spec.template.spec.containers[0].resources.requests[cpu]:        {value: 100m}
spec.template.spec.containers[0].resources.requests[memory]:     {value: 128Mi}
spec.template.spec.containers[0].resources.limits[memory]:       {value: 256Mi}
```

Port 80 matches the nginx image so the example runs if applied. Memory limit only: the
linked skill's rule is to leave CPU limits unset. Replicas 2 per the same skill's "never a
single replica". Verified on 2026-09-12 against the live engine: this shape generates a clean
Deployment, integers emit unquoted, and `POST /api/render` reports ok with no raw-template
warning.

Job and CronJob: same container block, `restartPolicy: Never`, CronJob adds
`spec.schedule: "*/5 * * * *"`. Service: `spec.selector[app]`, `spec.ports[0].port: 80`,
`spec.ports[0].targetPort: 80`, `spec.type: ClusterIP`. Every other native kind gets only
what the schema requires.

Migration to the map-entry grammar: the Sync button (`data-wl-app` handler), the Service
quick-match, `checkMissingRequired`, and `internal/examples/k8s-app.cf.yaml` move from raw
`{app: x}` to `[app]` entries. Readers keep accepting the old raw form for blueprints on disk.

## 2. Essentials form

Replaces the workload and service cards. For each essentials entry the row renders a label,
a typed input driven by the schema field (enum as select, integer as number input, boolean
as the existing three-state select, map entries as key/value rows), and two actions:

- Expose as parameter: creates an XRD parameter named after the leaf (`image`, `replicas`,
  `containerPort`), typed from the schema, default = current literal, then wires the field
  through the existing `addParameter` + `setField` path used by "+ new XRD parameter…".
- Wire: the existing wire select.

A wired row shows the existing `.bound` chip with unwire. Deployment rows: app label,
replicas, image, container name, port, cpu request, memory request, memory limit.

Kinds without a profile (every provider resource) derive essentials from the schema:
chain-required leaves, `region` when present, plus every field already set. A Queue shows
region and what the user set, not 57 rows.

### Environment variables (added 2026-09-12 after Kaur's review)

The schema lists arrays only as `[0]`, so today a second `env` entry cannot be added from
the GUI at all. Workload essentials therefore end with an "Environment variables" repeater:
one row per `containers[0].env[i]` with a NAME input, a value control that takes a literal
or a wire (status outputs and parameters, the k8s-app example's `QUEUE_URL ←
resources.queue.status.atProvider.url` pattern), a delete that renumbers the following
indices, and "+ add variable". Every other array (ports beyond the first, volumes, probes)
is authored in the manifest editor by adding a list item; a generic "+ element" for the
Fields view is a follow-up.

### Field search (added 2026-09-12 after Kaur's review)

The inspector header gains a search box next to the view toggle. In Fields view it filters
the loaded leaves on path and description (the fields API's `q` semantics, applied on top of
Required / Set / All). In Manifest view it lists matching schema leaves under the editor,
path with type and description, and clicking one switches to Fields view with the search
kept so the row's Val / Wire / Raw controls are at hand. The branch row's "expand via All /
search" text finally points at something that exists.

## 3. Manifest editor

### View

Below essentials, a "Manifest" block renders the resource's set fields as nested YAML in
manifest shape, highlighted with the output pane's `highlight()`. Managed resources show their
forProvider fields under a comment header. Leaf wrappers are the blueprint's own:

```yaml
spec:
  replicas: {from: params.replicas}
  selector:
    matchLabels:
      app: web
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.27
          resources:
            requests: {cpu: 100m, memory: 128Mi}
```

Rules: a plain scalar is a literal. A mapping whose only key is one of `value`, `from`,
`raw`, `template` is a wrapper. Anything else recurses. Whole-object raw values are always
written explicitly as `{raw: "..."}`; a bare string at an object or map position is an
error, never inferred. A genuine literal map that is exactly `{from: x}` is written
`{from: {value: x}}` and documented in `docs/dsl.md`.

### Editing

Same flow as the blueprint Edit tab in `output.js`: an edit button swaps the view for a
textarea; Apply and Cancel; Cmd/Ctrl+Enter applies; Escape cancels; Tab inserts two spaces. A
snippet dropdown built from `buildSnippets` inserts `{from: params.x}` or a status reference
at the cursor. A rejected apply keeps the editor open, shows the server message, and
highlights the offending line. Doc changes from elsewhere do not overwrite an open editor.

The pane header's Required / Set / All segment is replaced by a Manifest / Fields toggle;
Fields view restores the old list and its segment unchanged. The choice persists in
localStorage (`cf-insp-view`). The tour text at `tour.js:52` is updated.

### Server

Two routes, MCP tools bridging into the same handlers (parity by construction):

- `GET /api/blueprint/resources/{name}/manifest` → `{yaml}`. Field order follows the emit's
  path-sorted plan so the view matches the generated output.
- `PUT /api/blueprint/resources/{name}/manifest` ← `{yaml}`. Parses with yaml.v3, walks the
  document against the kind's `FieldTree()`, flattens to `map[string]blueprint.Field`. The
  schema decides the grammar: a map node's children become `[key]` entries, an array's
  become `[i]`, an object's use dots. Unknown key, scalar at a non-leaf, and scalar type
  mismatch return 400 `{error, path, line}` from the yaml.v3 node. On success the resource's
  fields are replaced in full inside `srv.mutate`, followed by the same
  `validateBlueprintAgainstCRDs` as `PUT /api/blueprint/resources/{name}`.
- MCP: `get_resource_manifest`, `set_resource_manifest`.

Adopt's name-based `isMapFieldPrefix` heuristic is not reused; the schema is authoritative.

## 4. Testing

Playwright (spec first, then code):

- Drop a Deployment: doc holds the starter fields in map-entry grammar; Generate's
  composition contains a container with `nginx:1.27`; Validate reports ok and the raw
  warning is absent.
- Essentials: rows render for a Deployment; editing image commits a value; expose on image
  creates parameter `image` with default `nginx:1.27`, wires the field, XRD card shows it.
  A Queue shows region plus set fields only.
- Essentials env repeater: "+ add variable" writes `env[1].name`; delete of `env[0]`
  renumbers `env[1]` to `env[0]`.
- Manifest: read-only view nests `containers:`; edit replicas, Apply, doc updated; unknown
  key → 400 shown, line highlighted, editor stays open; adding a second `ports` item lands
  as `ports[1].containerPort`; Fields toggle restores the old list.
- Search: typing `image` in Fields view leaves only rows whose path or description
  matches; in Manifest view it lists the leaf and clicking it opens Fields view filtered.
- The Playwright config seeds `cf-insp-view=fields` through `storageState`, so the 28
  existing list specs run unchanged; the manifest spec opts out with an empty storageState.
- `tests/cf439-auto-scaffold-drop.spec.js` moves to bracket keys. slice65's Sync test with
  `web:prod,v1` must still pass.

Go:

- Round trips for flatten and unflatten: Deployment, Queue with tags, arrays, wrapper
  detection, unknown key with line, scalar at object position, explicit `{raw}` at a map.
- Contract fixture for the manifest envelope (`testdata/contract`).
- MCP parity test for both tools (server_test pattern).
- k8s-app example in bracket grammar with byte-identical generated output.

Gates: `make lint`, `make test-race`, `make test-e2e` from the worktree (own port), e2e flake
rule from AGENTS.md.

## 5. Rollout

Three landable slices, each a commit with its guarding tests:

1. Starters and the raw-to-bracket migration (JS + example YAML).
2. Essentials form with expose and wire (JS).
3. Manifest endpoint and MCP tools (Go), then editor and toggle (JS).

No AI attribution trailers (AGENTS.md §4). Worktree `.worktrees/prefill`, branch
`prefill-fields` from `origin/main` at ea7e353.

## Out of scope (follow-ups)

- Clicking a schema search result to insert its path into the manifest at the right nesting.
- Probes, securityContext and PodDisruptionBudget as an optional "production" starter tier.
- Installing LukasNiessen/kubernetes-skill as a project skill.
