# web-proto — compositionfactory canvas (live)

The approved canvas prototype, made real: plain ES modules + CSS, no
frameworks, no build step. Talks to the live engine (`cf serve`, port 8080)
directly so every fetch is a same-origin relative `/api` path.

## Run

```sh
cf serve --blueprint blueprints/xqueue.cf.yaml --out ./out
```

then open <http://127.0.0.1:8080>.

## Layout

| file | role |
|---|---|
| `index.html` | the approved prototype markup, split into marked regions with stable root ids (`#region-topbar`, `#region-palette`, `#cw`, `#region-inspector`, `#region-output`) |
| `css/proto.css` | the prototype's CSS, extracted unchanged |
| `js/dom.js` | shared DOM helpers and HTML escaping (`esc`) |
| `js/api.js` | fetch wrappers for every endpoint; throws `{status, message}` with the server's verbatim error text — **frozen contract, do not edit** |
| `js/store.js` | single state container `{doc, selectedResource, positions, lastGenerate}` + pub/sub (topics: `doc`, `selection`, `generate`, `error`) — **frozen contract, do not edit** |
| `js/wires.js` | pure doc helpers: `listWires(doc)`, `fanOut(doc, param)` — **frozen contract, do not edit** |
| `js/main.js` | boot: imports the region modules, then `store.loadDoc()` |
| `js/regions/*.js` | one module per region, each owned by its region agent |

## Engine truths

- Wires live **in** the doc: a resource field `{from: "params.X"}` is a wire.
- Blueprint field forms are exactly-one-of `{value | from | raw}`.
- Unknown field paths are rejected with 400; the message is shown verbatim.
  Verified against the live engine: `PUT /api/blueprint` ACCEPTS an unknown
  path (200) — the 400 arrives from `POST /api/generate` (message names the
  resource and field). Surface generate errors, not just PUT errors.
