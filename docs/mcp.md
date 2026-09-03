# cf mcp — the MCP server

`cf mcp` serves compositionfactory's full authoring surface as MCP tools over
stdio: schema browsing, blueprint editing, provider management, generation and
the render check. Every tool is a thin bridge over the exact same handler
`cf serve` exposes on HTTP, so the two front doors validate identically and
report identical error messages — see `internal/mcp`'s package comment for the
architecture.

Writes are confined to the declared workspace: the `--blueprint` file and the
`--out` directory are the only paths the tools can write. `generate` checks
every output path (absolute, cleaned, prefix) against `--out` before writing
anything; a path outside it is refused with no files touched. `add_provider`
additionally maintains the schema cache (`--cache-dir`) and lockfile
(`--lock`) at the fixed paths chosen at launch — server infrastructure no tool
input can redirect.

## Registering the server

With [Claude Code](https://docs.anthropic.com/en/docs/claude-code):

```sh
claude mcp add compositionfactory -- \
  cf mcp --blueprint blueprints/xqueue.cf.yaml --out ./platform
```

Or in any MCP client that takes a JSON server definition:

```json
{
  "mcpServers": {
    "compositionfactory": {
      "command": "cf",
      "args": [
        "mcp",
        "--blueprint", "blueprints/xqueue.cf.yaml",
        "--out", "./platform"
      ]
    }
  }
}
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `--blueprint` | (required) | The blueprint file the tools read and edit. |
| `--out`, `-o` | `.` | Workspace directory `generate {"write":true}` writes into — with the blueprint file, the only path the tools can write. |
| `--cache-dir` | OS cache dir | Provider schema cache (shared with `cf serve`, `cf gen`, `cf provider add`). |
| `--lock` | `.cf.lock` | Lockfile `add_provider` pins newly added providers into. |

Relative `--blueprint`/`--out` paths resolve against the working directory the
MCP client launches the server in; prefer absolute paths in client
configuration, where that directory is rarely visible.

Providers must already be cached (`cf provider add <ref>`) or added at runtime
with the `add_provider` tool — everything else works entirely offline.

## Tools

Same operations, same validation, same error text as the HTTP API; the HTTP
column names the route each tool bridges to.

| Tool | HTTP equivalent | What it does |
|---|---|---|
| `list_kinds` | `GET /api/kinds` | Search the cached providers' managed-resource kinds (`search`, `limit`). |
| `get_kind_fields` | `GET /api/kinds/{apiVersion}/{kind}/fields` | One kind's settable `forProvider` fields, filterable by `prefix`, `max_depth`, `search`, `required_only` (effectively required: `requiredChain`, the whole ancestor chain, not the raw per-object flag), `limit`; `total` counts the pre-`limit` set, and `requiredBranches` lists required subtrees with no effectively-required leaf (a Deployment's `spec.selector`/`spec.template`). |
| `get_blueprint` | `GET /api/blueprint` | The whole blueprint document as JSON. |
| `replace_blueprint` | `PUT /api/blueprint` | Replace the whole document (full replace, not a merge; unknown keys rejected). |
| `add_parameter` | `POST /api/blueprint/parameters` | Declare a new XRD parameter; duplicates refused. |
| `update_parameter` | `PUT /api/blueprint/parameters/{name}` | Replace a parameter's declaration in full; omitting a key that currently holds a value is refused rather than silently discarding it. |
| `rename_parameter` | `POST /api/blueprint/parameters/{name}/rename` | Rename a parameter and rewrite every `from: params.<name>` reference. |
| `delete_parameter` | `DELETE /api/blueprint/parameters/{name}` | Delete a parameter; refused while resource fields still reference it. |
| `add_provider` | `POST /api/providers` | Fetch a provider package (network), cache its schemas, pin its digest, index its kinds. |
| `list_providers` | `GET /api/providers` | The providers being served, with digest and kind count. |
| `add_function` | `POST /api/functions` | Fetch a function package (network), cache its input schemas, pin its digest. |
| `generate` | `POST /api/generate` | Render XRD, Composition and functions.yaml through the same engine `cf gen` uses; `write:false` previews, `write:true` writes into `--out` only. |
| `render_check` | `POST /api/render` | Run a real `crossplane composition render` against a sample XR; the outcome (`ok`/`error`/`unavailable`) is the payload. |
| `adopt_composition` | `POST /api/blueprint/adopt` | Import an existing Crossplane Composition (and optional XRD) YAML manifest into a structured Blueprint; `persist: true` saves to the workspace blueprint file. |
| `preview_expression` | `POST /api/preview-expression` | Evaluate a Go template expression against synthetic context (`$spec`, `$xr`, `$xrMeta`, `$observed`, `$env`, `$i`, `$resource`). |

The 15 MCP tools above bridge the core Crossplane authoring, schema query, expression preview, generation, validation, provider and function ingestion, and adoption workflows.

### Complete HTTP API Route Inventory (`cf serve`)

The full HTTP API route table exposed by `cf serve`:

| Method | Route | Description |
|---|---|---|
| `GET` | `/healthz` | Plaintext liveness probe. |
| `GET` | `/api/version` | Version metadata and supported rendering engines (`go-templating`, `kcl`, `python`). |
| `GET` | `/api/kinds` | Search and list available CRD kinds. |
| `GET` | `/api/kinds/{apiVersion}/{kind}` | Detailed kind envelope and metadata. |
| `GET` | `/api/kinds/{apiVersion}/{kind}/fields` | Inspect spec/status fields for a CRD kind. |
| `GET` | `/api/blueprint` | Read the active blueprint document. |
| `PUT` | `/api/blueprint` | Replace and validate the active blueprint document. |
| `POST` | `/api/blueprint/import` | Import an existing composition directory or file. |
| `POST` | `/api/blueprint/adopt` | Import/adopt raw Crossplane YAML manifest. |
| `POST` | `/api/blueprint/parameters` | Add an XRD parameter. |
| `PUT` | `/api/blueprint/parameters/{name}` | Update an XRD parameter. |
| `POST` | `/api/blueprint/parameters/{name}/rename` | Rename an XRD parameter. |
| `DELETE` | `/api/blueprint/parameters/{name}` | Delete an XRD parameter. |
| `POST` | `/api/blueprint/resources` | Add a composed resource. |
| `PUT` | `/api/blueprint/resources/{name}` | Update a composed resource. |
| `POST` | `/api/blueprint/resources/{name}/rename` | Rename a composed resource. |
| `DELETE` | `/api/blueprint/resources/{name}` | Delete a composed resource. |
| `GET` | `/api/providers` | List active providers and schema counts. |
| `POST` | `/api/providers` | Ingest and cache a new provider package. |
| `DELETE` | `/api/providers/{ref}` | Evict a provider package from cache and blueprint sources. |
| `GET` | `/api/functions` | List active function packages in lockfile. |
| `POST` | `/api/functions` | Ingest and cache a new function package. |
| `GET` | `/api/rbac` | Generate aggregated ClusterRole RBAC manifests for composed native kinds. |
| `GET` | `/api/package` | Build or inspect `.xpkg` Crossplane configuration package. |
| `POST` | `/api/sources/crds` | Ingest CRD YAML manifest uploads. |
| `GET` | `/api/catalogue` | Search the Upbound/Crossplane catalogue. |
| `GET` | `/api/examples` | List starter templates and cache readiness. |
| `GET` | `/api/examples/{id}` | Retrieve starter template blueprint. |
| `POST` | `/api/examples/{id}/load` | Load starter template into active blueprint. |
| `GET` | `/api/cluster` | Discovered cluster CRD schemas. |
| `POST` | `/api/cluster/sync` | Sync cluster CRDs into local cache. |
| `POST` | `/api/cluster/connect` | Connect to a remote Kubernetes cluster context. |
| `POST` | `/api/generate` | Generate XRD, Composition, and functions.yaml. |
| `POST` | `/api/render` | Execute real Crossplane composition render check. |
| `POST` | `/api/preview-expression` | Evaluate Go template expression in-process. |

