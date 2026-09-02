# CLI & GitOps Guide

`cf` is a single binary that provides generation, visual editing, packaging, adoption, and AI agent integration for Crossplane Compositions.

---

## Command Reference

### `cf version` — Print Version

Displays the current version of the `cf` binary:

```sh
cf version
```

---

### `cf gen` — Generate Manifests

Generates production-ready, byte-deterministic YAML from a blueprint file:

```sh
cf gen <blueprint.cf.yaml> -o <output-dir> [flags]
```

#### Flags:
- `-o`, `--out <dir>`: Output directory (defaults to `.`).
- `--engine <engine>`: Composition rendering engine: `go-templating`, `kcl`, or `python` (overrides blueprint setting).
- `--template-source <source>`: Where the Composition's go-template body lives: `inline` (default) or `filesystem` (`templates/` folder + ConfigMaps + DeploymentRuntimeConfig).
- `--cache-dir <dir>`: Provider schema cache directory (defaults to `~/.cache/compositionfactory` on Linux or `~/Library/Caches/compositionfactory` on macOS).
- `--check`: Validate that generated output matches blueprint without writing to disk (exits `0` if in sync, `2` if output has drifted or is missing).

Output directory structure (Inline mode):
```
out/
├── compositions/
│   └── <plural>.<group>.yaml
├── xrds/
│   └── <plural>.<group>.yaml
├── functions.yaml
└── providerconfigs/
    └── <provider-family>.yaml
```

Output directory structure (FileSystem mode with `templateSource: FileSystem`):
```
out/
├── compositions/
│   └── <plural>.<group>.yaml
├── xrds/
│   └── <plural>.<group>.yaml
├── functions.yaml
├── providerconfigs/
│   └── <provider-family>.yaml
├── templates/
│   ├── 000-context.yaml
│   └── <resource>.yaml
└── runtime/
    └── deploymentruntimeconfig.yaml
```

*(Note: RBAC rules are not written to disk; they are dynamically queried via `GET /api/rbac` during live canvas sessions or CI automation).*

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
cf serve --blueprint <blueprint.cf.yaml> --out <output-dir> [flags]
```

#### Flags:
- `--blueprint <path>`: Path to blueprint file (defaults to `doc.cf.yaml`; scaffolds a blank blueprint if file does not exist).
- `-o`, `--out <dir>`: Output directory for `POST /api/generate` writes (defaults to `.`).
- `--addr <ip:port>`: Bind address (defaults to `127.0.0.1:8080`). Must be loopback unless `--i-know-this-is-unauthenticated` is explicitly passed.
- `--i-know-this-is-unauthenticated`: Allow binding a non-loopback address (required when running inside Docker containers or public networks).
- `--no-ui`: Serve API endpoints only (disables embedded canvas at `/`).
- `--lock <path>`: Path to dependency lockfile (defaults to `.cf.lock`).
- `--cache-dir <dir>`: Schema cache directory.
- `--cluster`: Connect to active Kubernetes context to discover cluster CRDs on startup.
- `--kubeconfig <path>`: Path to explicit kubeconfig file.
- `--context <name>`: Kubernetes context name to use.

---

### `cf adopt` — Ingest Existing Compositions

Adopts an existing Crossplane Composition (supporting both `function-go-templating` and classic patch-and-transform, with optional embedded or sibling XRDs) into a structured Blueprint:

```sh
cf adopt <composition.yaml> -o <blueprint.cf.yaml> [--provider <ref>]
```

#### Flags:
- `-o`, `--out <path>`: Output file path (defaults to stdout).
- `--provider <ref>`: Default provider package reference when not inferrable from CRDs.

---

### `cf package` — Build Crossplane Configuration Packages

Packages the blueprint, synthesized `crossplane.yaml`, XRDs, and Compositions into a standard `.xpkg` OCI artifact or YAML stream:

```sh
cf package <blueprint.cf.yaml> -o out/package.xpkg [flags]
```

#### Flags:
- `-o`, `--out <path>`: Output path (defaults to `<blueprint-name>.xpkg` or `<blueprint-name>.package.yaml` with `--yaml`).
- `--yaml`: Write the `package.yaml` multi-document stream instead of an `.xpkg` image (importable back via the GUI or `POST /api/blueprint/import`).
- `--cache-dir <dir>`: Schema cache directory.

Features:
- Synthesizes `crossplane.yaml` with exact dependency pins derived from blueprint sources and pipeline functions.
- Embeds the blueprint source inside package metadata annotations for lossless recovery.
- Compatible with `crossplane xpkg extract` and `crossplane xpkg push`.

---

### `cf push` — Push Package to OCI Registry

Pushes a built `.xpkg` configuration artifact to an OCI registry using standard keychain credentials:

```sh
cf push <image-ref> <package.xpkg>
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
- Saves schemas to `~/.cache/compositionfactory/` (Linux) or `~/Library/Caches/compositionfactory/` (macOS).

---

### `cf mcp` — Model Context Protocol Server

Starts an MCP server over stdio for LLMs and AI coding assistants (Claude Code, Antigravity, Cursor):

```sh
cf mcp --blueprint <blueprint.cf.yaml> --out <output-dir>
```

See the [MCP Server Guide](mcp.md) for tool documentation and setup instructions.
