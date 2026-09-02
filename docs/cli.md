# CLI & GitOps Guide

`cf` is a single binary that provides generation, visual editing, packaging, and AI agent integration for Crossplane Compositions.

---

## Command Reference

### `cf gen` — Generate Manifests

Generates production-ready, byte-deterministic YAML from a blueprint file:

```sh
cf gen <blueprint.cf.yaml> -o <output-dir>
```

Output directory structure:
```
out/
├── compositions/
│   └── <plural>.<group>.yaml
├── xrds/
│   └── <plural>.<group>.yaml
├── functions.yaml
├── providerconfigs/
│   └── <provider-family>.yaml
└── rbac/
    └── clusterrole.yaml
```

#### Drift Detection & CI Check (`--check`)
Validates that generated files in the output directory match the blueprint without modifying disk:
```sh
cf gen --check <blueprint.cf.yaml> -o <output-dir>
```
- Exits `0` if in sync.
- Exits `2` if output files have drifted or are missing.

---

### `cf serve` — Visual Canvas Server

Starts the embedded interactive visual canvas and HTTP API:

```sh
cf serve --blueprint <blueprint.cf.yaml> --out <output-dir>
```

#### Options:
- `--addr <ip:port>`: Bind address (defaults to `127.0.0.1:8080`).
- `--no-ui`: Serve API endpoints only (disables embedded canvas at `/`).
- `--lock <path>`: Path to dependency lockfile (defaults to `.cf.lock`).
- `--cluster`: Connect to active Kubernetes context to discover cluster CRDs.
- `--kubeconfig <path>`: Path to explicit kubeconfig file.

---

### `cf package` — Build Crossplane Configuration Packages

Packages the blueprint, synthesized `crossplane.yaml`, XRDs, and Compositions into a standard `.xpkg` OCI artifact:

```sh
cf package <blueprint.cf.yaml> -o out/package.xpkg
```

- Synthesizes `crossplane.yaml` with exact dependency pins derived from blueprint sources and pipeline functions.
- Embeds the blueprint source inside package metadata annotations for lossless recovery.
- Compatible with `crossplane xpkg extract` and `crossplane xpkg push`.

---

### `cf push` — Push Package to OCI Registry

Pushes a built `.xpkg` configuration artifact to an OCI registry using standard credentials:

```sh
cf push <image-ref> out/package.xpkg
```

Example:
```sh
cf push ghcr.io/org/my-configuration:v1.0.0 out/my-configuration.xpkg
```

---

### `cf provider add` — Cache Provider CRD Schemas

Fetches only CRD layer metadata from an OCI package and caches it locally:

```sh
cf provider add ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
```

- Appends the resolved digest to `.cf.lock`.
- Saves schemas to `~/.cache/compositionfactory/`.

---

### `cf mcp` — Model Context Protocol Server

Starts an MCP server over stdio for LLMs and AI coding assistants (Claude Code, Antigravity, Cursor):

```sh
cf mcp --blueprint <blueprint.cf.yaml>
```

See the [MCP Server Guide](mcp.md) for tool documentation and setup instructions.
