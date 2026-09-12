# CF-466 — Dropping a Deployment scaffolds no container and writes labels as raw JSON; Generate emits an invalid Deployment and exits 0

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `#366` — `CF-466 — Dropping a Deployment scaffolds no container and writes labels as raw JSON; starters should be valid, good-practice minimums in map-entry grammar` |
| **Worktree** | `.worktrees/prefill` on branch `prefill-fields` (slice 1 of the design in `docs/superpowers/specs/2026-09-12-prefilled-fields-design.md`) |
| **May write** | `web-proto/js/profiles.js` (new), `web-proto/js/regions/canvas.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/inspector/events.js`, `internal/examples/k8s-app.cf.yaml`, `tests/cf466-starter-deployment.spec.js` (new), `tests/cf439-auto-scaffold-drop.spec.js` |
| **Merges after** | nothing |

## Symptom

Drag a Deployment from KINDS onto the canvas, click Generate. The composition contains a
Deployment with `selector` and `template.metadata.labels` and nothing else: no
`containers`. Generate exits 0 and Validate (crossplane render) reports ok, so the user
learns the object is invalid only from the API server. The two label fields are written
as raw JSON, so the output pane also warns that they bypass schema validation.

## Evidence

Reproduced twice on 1335360 against `cf serve` on 8090 (2026-09-12). Doc after a drop:

```
spec.selector.matchLabels: {raw: "{\"app\":\"web\"}"}
spec.template.metadata.labels: {raw: "{\"app\":\"web\"}"}
```

`POST /api/generate {"write":false}` → composition body:

```
          spec:
            selector:
              matchLabels:
                app: 'web'
            template:
              metadata:
                labels:
                  app: 'web'
```

`POST /api/render` → `{'ok': True, 'resources': 1, 'error': '', 'unavailable': ''}`.

Inspector output pane: "4 fields use raw templates (web.spec.selector.matchLabels, …) — raw
values bypass CRD schema validation".

The bracket form generates correctly and validates (same session):
`spec.selector.matchLabels[app]: {value: web}` + a container block with
`ports[0].containerPort: {value: "80"}` and `resources.requests[cpu]: {value: 100m}` →
`containerPort: 80`, `cpu: '100m'`, render ok.

## Location

`web-proto/js/regions/canvas.js:1609-1640` — `scaffoldResourceFields` writes the Deployment
container block only when `isExplicit` is true; `onDrop` (line ~1785) calls it without the
flag. Labels are written as `{ raw: JSON.stringify({app: name}) }`.
`web-proto/js/regions/inspector/events.js:786-803` — the Sync button writes the same raw JSON.
`internal/examples/k8s-app.cf.yaml:102-103,116` — the example uses `raw: "{app: worker}"`.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any production
code: `tests/cf466-starter-deployment.spec.js` exactly as given in
`docs/superpowers/plans/2026-09-12-prefilled-fields.md` Task 1.

**Fails today with** (first run on `prefill-fields`, 2026-09-12, before any production change):

```
Running 4 tests using 1 worker

  ✘  1 tests/cf466-starter-deployment.spec.js:14:3 › … › dropping a Deployment writes the full starter with no raw JSON (5.5s)
  ✘  2 tests/cf466-starter-deployment.spec.js:43:3 › … › the starter generates a container and passes render validation (416ms)
  ✘  3 tests/cf466-starter-deployment.spec.js:59:3 › … › Service starter targets app label with bracket grammar and ClusterIP (5.3s)
  ✘  4 tests/cf466-starter-deployment.spec.js:77:3 › … › Sync on the workload card writes bracket entries, not raw JSON (5.6s)

  1) … › dropping a Deployment writes the full starter with no raw JSON

    Error: expect(received).toEqual(expected) // deep equality

    - Expected  - 11
    + Received  + 11

      Object {
    -   "anyRaw": false,
    -   "cName": "deployment",
    -   "cpu": "100m",
    -   "image": "nginx:1.27",
    -   "labels": "deployment",
    -   "mem": "128Mi",
    -   "memLimit": "256Mi",
    -   "oldRawSelector": false,
    -   "port": "80",
    -   "replicas": "2",
    -   "selector": "deployment",
    +   "anyRaw": true,
    +   "cName": undefined,
    +   "cpu": undefined,
    +   "image": undefined,
    +   "labels": undefined,
    +   "mem": undefined,
    +   "memLimit": undefined,
    +   "oldRawSelector": true,
    +   "port": undefined,
    +   "replicas": undefined,
    +   "selector": undefined,
      }
```

## Contract

- A dropped Deployment, StatefulSet, DaemonSet, Job, CronJob or Service lands with the
  starter listed in the design doc §1, every value a `{value}` literal, labels and resource
  quantities as `[key]` map entries, no `raw` anywhere.
- Drop and the inspector's Scaffold button write the same fields (one profile).
- Generated composition for a dropped Deployment contains a container with image
  `nginx:1.27`, `containerPort: 80`, requests and a memory limit; `POST /api/render` ok.
- Sync and the Service quick-match write `[app]` entries; readers still accept the old raw
  and dotted forms for blueprints on disk.
- `internal/examples/k8s-app.cf.yaml` migrated; its generated output stays byte-identical
  except quoting inside the two label values (record the diff in the commit body).
- Provider kinds keep today's schema-driven scaffold.

## Verification

```sh
npx playwright test tests/cf466-starter-deployment.spec.js tests/cf439-auto-scaffold-drop.spec.js tests/slice63-selectors-functions.spec.js tests/slice65-authoring-ux-enhancements.spec.js
go test ./internal/examples/ ./internal/emit/ -count=1
make lint
```

## Out of scope

Essentials form (CF-467), manifest editor (CF-468), probes/securityContext tier.

## Handover

Commits on `prefill-fields`; the failing and passing runs of the acceptance test pasted in
the handover on the issue.
