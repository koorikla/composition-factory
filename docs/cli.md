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

### `cf init` — Scaffold Minimal Blueprint

Scaffolds a minimal valid blueprint document (`blueprint.cf.yaml` by default) containing a Namespaced XRD identity and the required `providerName` parameter:

```sh
cf init [path]
```

#### Arguments:
- `[path]`: Optional path for the created blueprint file (defaults to `blueprint.cf.yaml`). Refuses to overwrite existing files.

Example:
```sh
cf init my-service.cf.yaml
```

---

### `cf kinds` — Discover Available Kinds

Lists available CRD kinds from cached providers, blueprint sources, and native Kubernetes kinds:

```sh
cf kinds [query] [flags]
```

#### Flags:
- `[query]`: Optional substring/fuzzy filter matching kind name, API group, apiVersion, or provider.
- `--blueprint <path>`: Path to blueprint file to include declared sources (defaults to `doc.cf.yaml`).
- `--cache-dir <dir>`: Provider schema cache directory.

Example:
```sh
cf kinds bucket
# Output:
# KIND    APIVERSION                       SCOPE       REQUIRED/FIELDS  PROVIDER
# Bucket  s3.aws.m.upbound.io/v1beta1     Namespaced  0/8              ghcr.io/.../provider-aws-s3:v2.7.0
```

---

### `cf fields` — Inspect Kind Schema Tree

Displays the settable spec fields or observed status fields for a CRD kind:

```sh
cf fields <kind> [flags]
```

#### Flags:
- `<kind>`: Kind name (e.g. `Queue`, `Instance`, `Deployment`) or fully qualified `apiVersion/Kind`.
- `--required`: Print only required fields and required branch objects.
- `--status`: Print status output fields instead of spec fields (for `resources.<name>.status` wiring).
- `--blueprint <path>`: Path to blueprint file to include declared sources.
- `--cache-dir <dir>`: Provider schema cache directory.

Example:
```sh
# View required fields only
cf fields Instance --required

# View available status fields for cross-resource wiring
cf fields Bucket --status
```

---

### `cf catalogue` — Search Provider & Function Packages

Searches the Upbound / Crossplane package catalogue:

```sh
cf catalogue [query] [flags]
```

#### Flags:
- `[query]`: Optional search query matching package name, description, or served kinds.
- `--type <type>`: Filter by package type: `provider` or `function`.
- `--kind <kind>`: Filter packages that serve a specific CRD kind (e.g. `Bucket`, `DatabaseInstance`).

Example:
```sh
# Find packages that serve SQS queues
cf catalogue --kind Queue

# Search for available function packages
cf catalogue --type function
```

---

### `cf gen` — Generate Manifests

Generates production-ready, byte-deterministic YAML from a blueprint file:

```sh
cf gen <blueprint.cf.yaml> -o <output-dir> [flags]
```

#### Flags:
- `-o`, `--out <dir>`: Output directory (defaults to `.`).
- `--engine <engine>`: Composition rendering engine: `go-templating` (default), `kcl`, or `python` (overrides blueprint setting). Note that `template:` references, `spec.conventions`, and Go template expressions `{{ ... }}` in `raw:` are supported only in `go-templating` and are refused under `kcl` and `python`.
- `--template-source <source>`: Where the Composition's go-template body lives: `inline` (default) or `filesystem` (`templates/` folder + ConfigMaps + DeploymentRuntimeConfig).
- `--cache-dir <dir>`: Provider schema cache directory (defaults to `~/.cache/compositionfactory` on Linux or `~/Library/Caches/compositionfactory` on macOS).
- `--check`: Validate that generated output matches blueprint without writing to disk (exits `0` if in sync, `2` if output has drifted or is missing).
- `--validate`: Validate rendered output against CRD schemas by running `crossplane composition render` against a synthesized sample XR.
- `--group-suffix <suffix>`: Appends a workspace isolation suffix to the XRD group (e.g. `.w1a2b3c.cf-test` to prevent cluster-scoped CRD collisions in a shared cluster).

Output directory structure (Inline mode):
```
out/
├── compositions/
│   └── <plural>.<group>.yaml
├── xrds/
│   └── <plural>.<group>.yaml
├── functions.yaml
├── providerconfigs/
│   └── <provider-family>.yaml
└── rbac.yaml (emitted when native Kubernetes kinds are composed)
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
│   └── <plural>.<group>/
│       ├── 000-context.yaml
│       └── <resource>.yaml
├── runtime/
│   └── <plural>.<group>.yaml
└── rbac.yaml (emitted when native Kubernetes kinds are composed)
```

*(Note: When native Kubernetes kinds like Deployment or Service are composed, `rbac.yaml` contains the aggregated ClusterRole required by Crossplane to manage them).*

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

### `cf function add` — Cache Function Input Schemas

Fetches a Crossplane function package, extracts its Input CRDs, caches them locally, and pins the digest in `.cf.lock`:

```sh
cf function add <function-package-ref> [flags]
```

#### Flags:
- `--cache-dir <dir>`: Schema cache directory.
- `--lock <path>`: Path to lockfile (defaults to `.cf.lock`).

Example:
```sh
cf function add xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
```

- Schema validation checks `spec.pipeline[].input` against the cached Input CRD.
- Warns explicitly if a pipeline step references an uncached function package.

---

### `cf mcp` — Model Context Protocol Server

Starts an MCP server over stdio for LLMs and AI coding assistants (Claude Code, Antigravity, Cursor):

```sh
cf mcp --blueprint <blueprint.cf.yaml> --out <output-dir>
```

See the [MCP Server Guide](mcp.md) for tool documentation and setup instructions.

---

## Important: The `providerName` Parameter Requirement

In Crossplane Compositions, managed resources require a reference to a `ProviderConfig` (via `providerConfigRef`) to authenticate with cloud providers. Composition Factory generates this binding dynamically:

```yaml
providerConfigRef:
  kind: ClusterProviderConfig
  name: {{ $spec.providerName }}
```

Because `scope: Cluster` is refused in M1 in favor of `scope: Namespaced`, **any blueprint containing managed resources (`provider != k8s`) must declare `providerName` as a required string parameter**:

```yaml
spec:
  xrd:
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
        description: "Name of the ProviderConfig to use for managed resources"
```

If `providerName` is missing or optional, `cf gen` will fail with:
```
spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}
```

*Exception*: If a blueprint is composed purely of native Kubernetes resources (`provider: k8s`), `providerConfigRef` is not generated and `providerName` is not required.

