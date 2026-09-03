# Blueprint DSL Reference

Composition Factory uses a single declarative Blueprint format (`*.cf.yaml`) that unifies the CompositeResourceDefinition (XRD) schema, Crossplane pipeline definition, and composed resources into one cohesive source of truth.

---

## Blueprint Structure

apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: <blueprint-name>
spec:
  sources:
    - provider: <oci-image-ref>
    - crds: <manifest-path>
  xrd:
    group: <api-group>
    kind: <composite-kind>
    plural: <composite-plural>
    version: <api-version>
    scope: Namespaced # Note: "Cluster" is refused in M1 ("use Namespaced")
    parameters:
      providerName: # Mandatory in Namespaced XRDs containing managed resources
        type: string
        required: true
        description: <doc-string>
      <param-name>:
        type: string | integer | number | boolean | object
        required: true | false
        default: <default-val>
        enum: [<val1>, <val2>]
        description: <doc-string>
        properties: # nested definitions for type: object
          <member-name>:
            type: string | integer | number | boolean
            required: true | false
            default: <default-val>
            enum: [<val1>, <val2>]
            description: <doc-string>
  environment: # optional Crossplane v2 EnvironmentConfigs declaration
    <env-key>:
      type: string | integer | number | boolean
      default: <default-val>
      description: <doc-string>
  pipeline: # optional custom composition pipeline steps (bare list)
    - name: auto-ready
      functionRef: function-auto-ready
      package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
      position: after # "before" or "after" templating step (default: "after")
    - name: custom-step
      functionRef: function-custom
      package: xpkg.example.org/custom-fn:v1.0.0
      input: | # verbatim typed input CRD YAML
        apiVersion: custom.fn.crossplane.io/v1alpha1
        kind: Input
        spec:
          key: value
  templates:
    <template-name>: |
      <go-template-body>
  conventions:
    - match: <field-suffix> # Suffix match on field name (refused on native kinds)
      template: <template-name>
  resources:
    - name: <resource-name>
      kind: <crd-kind>
      provider: <provider-ref | k8s>
      fields:
        <path>: {value | from | raw | template}
        <mapField>[<key>]: {value | from | raw} # Bracket grammar for map entries
      envelope:
        <envelope-path>: {value | from | raw} # Note: status wires and template: are refused in envelope
      annotations:
        <annotation-key>: {value | from | raw | template}
      when: params.<boolParam> | params.<param> == "<lit>" | env.<boolKey> | env.<key> == "<lit>"
      forEach: params.<intParam> | env.<intKey> | resources.<name>.status.atProvider.<field>
```

---

## Parameter Types

Parameters in `spec.xrd.parameters` define the OpenAPI schema for the XRD and claim:

- **`string`**: String value, with optional `enum` list.
- **`integer`**: 64-bit integer value.
- **`number`**: Floating point / decimal number value.
- **`boolean`**: Boolean flag (`true` / `false`).
- **`object`**: Structured object. Supports arbitrary nesting via recursive `properties` definitions. Members are wired in fields via `params.<object>.<member>`.

*(Note: `type: "array"` is explicitly refused in M1).*

### Mandatory `providerName` Parameter
When `spec.xrd.scope` is `Namespaced` (the required scope in M1) and the blueprint contains managed provider resources (`provider != k8s`), `spec.xrd.parameters.providerName` is **strictly required**:

```yaml
spec:
  xrd:
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
        description: "ProviderConfig name used for managed resources"
```

**Why it is required**: Crossplane managed resources default `providerConfigRef` to `{kind: ClusterProviderConfig, name: {{ $spec.providerName }}}`. Without `providerName: {type: string, required: true}`, the Composition's unguarded dereference would fail rendering under `missingkey=error` or leave managed resources unbound.

*Note: If a blueprint composes exclusively native Kubernetes resources (`provider: k8s`), `providerName` is optional and can be omitted.*

---

## Environment Configuration (`spec.environment`)

Crossplane v2 supports environment configuration where environment data is fetched into pipeline context (`index .context "apiextensions.crossplane.io/environment"`). The Blueprint DSL allows declaring environment keys and types directly under `spec.environment`:

```yaml
spec:
  environment:
    vpcId:
      type: string
      description: "Default VPC ID from EnvironmentConfig"
    subnetCount:
      type: integer
      default: 3
      description: "Number of subnets"
    isProduction:
      type: boolean
      default: false
      description: "Production mode flag"
```

### Supported Types:
- **`string`**: String environment value.
- **`integer`**: Integer environment value (e.g. for `forEach` loops).
- **`number`**: Floating point / decimal number.
- **`boolean`**: Boolean flag (`true` / `false`).

### Automatic Pipeline Injection:
When `spec.environment` is declared and non-empty, the generator automatically:
1. Injects the `function-environment-configs` pipeline step ahead of the templating step (pinned to `xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0` in `functions.yaml`).
2. Populates `$env` in the Go template context with `hasKey` guards and safe defaults.
3. Records the `factory.crossplane.io/environment-keys` annotation on the generated Composition for lossless round-trip fidelity during `cf adopt`.

### Environment Wires (`from: env.<key>`):
Environment variables can be wired directly using `from: env.<key>` across:
- **`fields`**: `{from: env.vpcId}`
- **`annotations`**: `custom.org/vpc: {from: env.vpcId}`
- **`envelope`**: `writeConnectionSecretToRef.namespace: {from: env.secretNamespace}`
- **`when` conditionals**:
  - `when: env.isProduction` (boolean truthiness test)
  - `when: env.stage == "prod"` (equality check against literal string)
  - `when: env.stage != "dev"` (inequality check against literal string)
- **`forEach` bounds**: `forEach: env.subnetCount` (requires `type: integer`)

*Engine Notes: In Python compositions, environment data is extracted from `req.context["apiextensions.crossplane.io/environment"]`. In KCL compositions, `spec.environment` is currently refused as KCL uses standalone schemas.*

---

## Field Modes

Every field in `resources[*].fields` supports exactly one of four authoring forms:

### 1. `value`
Assigns a literal scalar or primitive value.
```yaml
fields:
  region: {value: "eu-north-1"}
  engine: {value: postgres}
```

### 2. `from`
Wires an XRD parameter, environment key, upstream resource status output, or upstream resource metadata name:
- **XRD Parameter binding**: `params.<name>` or nested member `params.<obj>.<member>`
  - Required parameters dereference directly: `{{ .spec.<name> }}`
  - Optional parameters are wrapped in Go-template `hasKey` guards to prevent missing key errors.
- **Environment Key binding**: `env.<key>`
  - Dereferences from the Crossplane environment context: `index $env "<key>"`
  - Guarded with `hasKey $env "<key>"` so absent optional keys omit cleanly.
- **Cross-Resource Status reference**: `resources.<name>.status.atProvider.<field>`
  - Generates guarded status dereferencing so unobserved upstream resources omit the field cleanly on early reconcile passes.
- **Cross-Resource Metadata Name reference**: `resources.<name>.metadata.name`
  - Wires another composed resource's formatted name directly.
```yaml
fields:
  allocatedStorage: {from: params.storageGB}
  vpcId:            {from: env.vpcId}
  subnetId:         {from: resources.vpc.status.atProvider.defaultSubnetId}
```

### 3. `raw`
Emits verbatim YAML or Go template expressions. This is the primary escape hatch for custom expressions, helper functions, and index-based math.

```yaml
fields:
  spec.selector.matchLabels: {raw: "{app: web}"}
  cidrBlock: {raw: '{{ printf "10.0.%d.0/24" $i }}'}
  tags[Name]: {raw: '{{ printf "%s-subnet-%d" $xr $i }}'}
```

#### Runtime Variables Available in `raw`:
When using the default `go-templating` engine, `raw:` expressions have direct access to the following scoped template variables:
- **`$spec`**: The composite resource's parameter map (`.observed.composite.resource.spec`). E.g., `$spec.region`, `$spec.dbName`.
- **`$xr`**: The composite resource's metadata name string (`.observed.composite.resource.metadata.name`), useful for deterministic resource naming and tagging.
- **`$xrMeta`**: The composite resource's entire `metadata` map (including `.labels`, `.annotations`, `.namespace`, and `.uid`).
- **`$observed`**: The map of all observed composed resources (`.observed.resources`). Each resource is accessed via `(index $observed "<resource-name>").resource`.
- **`$env`**: The map of all resolved environment configuration keys (`index .context "apiextensions.crossplane.io/environment" | default dict`).
- **`$i`**: The current iteration index (`0, 1, 2, ...`) available inside looped resources (`forEach`).
- **`$resource`**: The composed resource's name string.

*Note: In `python` and `kcl` engines, `{{ ... }}` Go-template syntax inside `raw:` is rejected with a validation error.*

### 4. `template`
Executes a reusable named template from `spec.templates`.
```yaml
templates:
  trust-policy: >-
    '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Federated":"{{ .spec.oidcProviderArn }}"},"Action":"sts:AssumeRoleWithWebIdentity"}]}'
resources:
  - name: role
    kind: Role
    provider: ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0
    fields:
      assumeRolePolicy: {template: trust-policy}
```

#### Template Constraints:
- `template:` references are supported only in the default `go-templating` engine. When using `kcl` or `python` engines, `template:` fields, `spec.conventions`, and `{{ ... }}` Go-template syntax in `raw:` fields are strictly refused.
- `template:` is refused on native Kubernetes resource fields like `Deployment.spec` (because a template call's output re-indents to the fixed `forProvider` column), but is fully supported in `annotations:`.

### Map-Entry Bracket Grammar
Map fields support direct key subscripting using bracket grammar:
```yaml
fields:
  labels[environment]: {value: "production"}
  selector.matchLabels[app]: {from: params.appName}
```

---

## Annotations Authoring

The `annotations` block maps directly to `metadata.annotations` on the emitted resource (supporting managed and native Kubernetes kinds):

```yaml
resources:
  - name: sa
    kind: ServiceAccount
    provider: k8s
    annotations:
      eks.amazonaws.com/role-arn: {from: resources.role.status.atProvider.arn}
      custom.io/vpc:             {from: env.vpcId}
      custom.io/policy:          {template: trust-policy}
```

If the referenced status value is not yet available, the generated Composition skips rendering the annotation key cleanly rather than rendering empty or invalid strings.

---

## Envelope Configuration

Crossplane-native envelope fields (`providerConfigRef`, `writeConnectionSecretToRef`, `managementPolicies`, etc.) are authored per-resource in `envelope`:

```yaml
resources:
  - name: db-instance
    kind: Instance
    provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
    envelope:
      writeConnectionSecretToRef.name:      {from: params.dbName}
      writeConnectionSecretToRef.namespace: {from: env.secretNamespace}
      managementPolicies:                  {value: "Observe, Create, Update, Delete, LateInitialize"}
```

#### Envelope Rules:
- Supports `value`, `from` (with `params.<name>` or `env.<key>`), and `raw`.
- Status wires (`resources.<name>.status...`) and `template:` are refused in `envelope`.
- Array-typed envelope leaves (such as `managementPolicies`) accept comma-separated strings in `value` (e.g. `value: "Observe, Create, Update"`) or literal lists in `raw`.

---

## Flow Control: Loops and Conditionals

### Conditional Inclusion (`when:`)
Conditionally renders a resource based on a boolean parameter/environment key or literal string comparison:
```yaml
resources:
  - name: backup-vault
    kind: BackupVault
    provider: ghcr.io/crossplane-contrib/provider-aws-backup:v2.7.0
    when: params.enableBackups

  - name: prod-cache
    kind: ReplicationGroup
    provider: ghcr.io/crossplane-contrib/provider-aws-elasticache:v2.7.0
    when: env.isProduction

  - name: regional-config
    kind: ConfigMap
    provider: k8s
    when: params.environment == "prod"
```

Grammar supported:
- `params.<boolParam>`: evaluated as truthy boolean XRD parameter.
- `params.<param> == "<literal>"`: equality comparison against a literal string.
- `params.<param> != "<literal>"`: inequality comparison against a literal string.
- `env.<boolKey>`: evaluated as truthy boolean environment key (`{{- if and (hasKey $env "<key>") $env.<key> }}`).
- `env.<key> == "<literal>"`: equality comparison of string environment key.
- `env.<key> != "<literal>"`: inequality comparison of string environment key.

### Resource Loops (`forEach:`)
Replicates a resource N times driven by an integer parameter, integer environment key, or upstream observed status:
```yaml
resources:
  - name: cluster-node
    kind: ClusterInstance
    provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
    forEach: params.nodeCount

  - name: subnet
    kind: Subnet
    provider: ghcr.io/crossplane-contrib/provider-aws-ec2:v2.7.0
    forEach: env.subnetCount
```
Emits indexed resource names and `setResourceNameAnnotation` bindings for each replica in the loop.

---

## Conventions (`spec.conventions`)

Conventions apply a named template to any field across resources that matches a given suffix:

```yaml
conventions:
  - match: tags
    template: standard-tags
```

- `match` performs a case-sensitive suffix match against field paths (e.g., `tags` matches `tags` and `spec.tags`).
- Conventions are refused on native Kubernetes kinds (`provider: k8s`).
- Conventions are supported only with the `go-templating` engine.

---

## Composition Pipeline (`spec.pipeline`)

The composition pipeline executes functions sequentially. By default (when `spec.pipeline` is omitted), the generator automatically emits:
1. `function-environment-configs` (if `spec.environment` is non-empty).
2. The templating function (`function-go-templating`, `function-kcl`, or `function-python`).
3. `function-auto-ready` (`xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1`).

When `spec.pipeline` is explicitly declared as a list, it completely controls the pipeline steps around the templating step:

```yaml
spec:
  pipeline:
    - name: auto-ready
      functionRef: function-auto-ready
      package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
      position: after # "before" or "after" (default: "after")

    - name: custom-step
      functionRef: function-custom-validator
      package: xpkg.example.org/validator:v1.0.0
      position: before
      input: |
        apiVersion: validator.fn.crossplane.io/v1alpha1
        kind: Input
        spec:
          strictMode: true
```

### Pipeline Step Properties:
- **`name`**: Step name string (required, unique DNS label; cannot be `render-resources`).
- **`functionRef`**: Function name string (required, DNS label, e.g. `function-auto-ready`).
- **`package`**: OCI package reference (required, e.g. `xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1`).
- **`input`**: Raw YAML mapping string for the function's typed Input CRD (must contain non-empty `apiVersion` and `kind`).
- **`position`**: Relative position to the templating step: `"before"` or `"after"` (default: `"after"`).

---

## Emission Options (`spec.emit`)

The blueprint controls the generator output engine and structure via `spec.emit`:

```yaml
spec:
  emit:
    engine: go-templating    # "go-templating" (default), "kcl" (function-kcl), or "python" (function-python)
    templateSource: Inline   # "Inline" (default) or "FileSystem"
```

- **`engine: go-templating`**: Generates `function-go-templating` pipeline step with Go template expressions and `missingkey=error`.
- **`engine: kcl`**: Generates `function-kcl` (`krm.kcl.dev/v1alpha1` `KCLInput`) pipeline step with typed KCL expressions (`oxr`, `_spec`, `ocds`, `items = _items`), automatically configured in `functions.yaml` and `package.yaml`.
- **`engine: python`**: Generates `function-python` (`python.fn.crossplane.io/v1beta1` `Script`) pipeline step with native Python composition logic (`req: fnv1.RunFunctionRequest, rsp: fnv1.RunFunctionResponse`), desired resource mappings, and readiness signals, automatically configured in `functions.yaml` and `package.yaml`.
- **`templateSource: FileSystem`**: Emits one template file per object in a `templates/<plural>.<group>/` folder packed into ConfigMaps and mounted via a `DeploymentRuntimeConfig` (`runtime/<plural>.<group>.yaml`).

---

## Common Errors & Diagnostics

The `cf` generator validates blueprints strictly and returns actionable error messages:

| Error Message | Cause & Remedy |
|---|---|
| `spec.xrd.scope: Cluster is not supported in M1 -- use Namespaced.` | M1 requires Namespaced composite resource definitions. Change `scope: Namespaced`. |
| `spec.xrd.parameters.providerName is required for a Namespaced XRD...` | Managed resources require `providerName: {type: string, required: true}` in `spec.xrd.parameters` to configure `providerConfigRef`. |
| `spec.xrd.parameters.<param>: type "array" is not supported in M1.` | Array parameter types are not supported. Use scalar types (`string`, `integer`, `number`, `boolean`) or `object` with nested properties. |
| `spec.environment.<key>: type is required (must be string, integer, number, or boolean)` | Environment key must declare a scalar type (`string`, `integer`, `number`, `boolean`). |
| `spec.pipeline[<i>].name: "render-resources" collides with the built-in templating step's name` | Step name `render-resources` is reserved for the built-in templating step. Choose another name. |
| `spec.pipeline[<i>].position: "<pos>" is not valid (must be "before" or "after")` | Position must be either `before` or `after` the templating step. |
| `resource "<name>" field "<path>": unknown path -- did you mean "<suggestion>"?` | Field path does not exist in CRD schema. Check typo or update to suggested schema path. |
| `resource "<name>" field "<path>": template: fields are not supported on a native Kubernetes kind...` | `template:` references are only supported on managed provider fields or in `annotations:`. Use `value:`, `from:`, or `raw:` on native fields. |
| `resource "<name>" field "<path>": engine "<engine>" does not support template: fields` | `template:` and `spec.conventions` are only supported with the `go-templating` engine. Use `value:`, `from:`, or engine-native `raw:`. |
| `resource "<name>" envelope "<path>": status wires (...) are not supported in envelope` | `from: resources.<name>.status...` is not permitted in `envelope`. Status references can only be wired into `fields:` or `annotations:`. |
| `resource "<name>": conventions cannot match native Kubernetes kind` | `conventions` can only target managed provider resources, not native Kubernetes resources. |

