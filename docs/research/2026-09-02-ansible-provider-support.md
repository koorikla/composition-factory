# Ansible provider support — runbooks and roles as compositions

**Question** (user backlog, 2026-09-02): should cf support Ansible — "ala convert a
runbook / role into a composition"?

## 1. What already works, today, with zero engine changes

`provider-ansible` (crossplane-contrib) is a normal Crossplane provider whose one
managed resource, **`AnsibleRun`** (`ansible.crossplane.io/v1alpha1`), is a
forProvider-shaped kind like any upjet resource — exactly the family the engine
composes. It is **already in our catalogue** (entry 304,
`ghcr.io/crossplane-contrib/provider-ansible:v0.8.0`), and it is maintained:
v0.8.0 released 2026-02-26, commits into mid-2026.

So the whole existing loop applies unmodified:

1. SOURCES → catalogue search "ansible" → add.
2. Drop `AnsibleRun` on the canvas.
3. `spec.forProvider.playbookInline` takes the playbook as opaque YAML — a
   `raw:` field, or better a **`template:` field**, so XR parameters flow into
   the playbook (`{{ .spec.replicas }}` inside the rendered play). Remote
   sources (git roles) are plain forProvider fields.
4. Generate → the AnsibleRun document rides the same guarded go-template as
   every other resource; `crossplane composition render` verifies it.

That is "Ansible provider support" in the sense the engine defines support, and
it needs only a docs/example showing the pattern.

## 2. "Convert a runbook/role into a composition" — two honest levels

**(a) Wrap — recommended.** A converter that takes a playbook or role and
*scaffolds a blueprint around it*:

- role/playbook body → an `AnsibleRun` resource (`playbookInline` via the
  blueprint's template mechanism, or `roles` + remote source for a git role);
- `defaults/main.yml` / play `vars` → **XRD parameters** (typed by YAML value
  shape, defaults carried over — the same lift `cf adopt` performs for
  patch-and-transform compositions);
- each lifted var wired `from: params.<name>` into the rendered play.

This is mechanical, deterministic, testable — the same class of ingestion as
`cf adopt`, and it would naturally live there: `cf adopt --ansible <dir|playbook.yml>`.
The composition's contract stays honest: Crossplane reconciles an AnsibleRun;
Ansible remains the execution engine.

**(b) Transpile — not recommended.** Translating Ansible *tasks*
(`amazon.aws.s3_bucket` → a Bucket MR) into native managed resources is a
per-module semantic mapping with an unbounded surface, no fidelity guarantee,
and drift against both module and provider schemas. Same verdict class as the
Configuration-DOM memo: don't rebuild someone else's semantics.

## 3. Recommendation

Adopt (a) when demand shows up, as an `cf adopt` input format:

- **Milestone A (docs-only, cheap):** an `examples/` blueprint + Guide section
  showing AnsibleRun composition with a templated `playbookInline` and a
  params-lifted var. Proves the loop with what ships today.
- **Milestone B (`cf adopt --ansible`):** vars→parameters lift + AnsibleRun
  scaffold, golden-tested like the other adopters.

## Sources

- https://github.com/crossplane-contrib/provider-ansible — AnsibleRun, inline/remote
  sources; `examples/ansible/playbook/ansibleRun-playbook-inline.yml` (spec shape);
  releases v0.8.0 (2026-02-26), v0.7.0 (2025-09-26)
- Local: `catalogue/providers.json` (entry 304), `internal/emit/composition.go`
  (forProvider family), `cf adopt` (65c5e6e) as the ingestion precedent
