# Agent Guidelines for composition-factory

This document records the foundational architecture rules, testing loops, and code hygiene principles for all AI coding agents working on this repository.

---

## 1. Engine Truths

- **One Engine (`internal/emit`)**:
  All emission of Crossplane artifacts (XRD, Composition, functions.yaml, RBAC) is implemented strictly in `internal/emit`.
  The CLI (`cf gen`), HTTP API (`cf serve`), and MCP server (`cf mcp`) are thin bridges calling `internal/emit`. Never create duplicate or parallel emission logic. Every interface must generate 100% byte-identical, deterministic YAML.

- **Blueprint as Single Source of Truth**:
  The `factory.crossplane.io/v1alpha1` `Blueprint` document is the canonical intermediate representation (IR). Edits, parameter definitions, and resource wiring manipulate this document directly.

- **Strict CRD Schema Validation**:
  Provider CRD OpenAPI schemas (`spec.forProvider`) are authoritative. Field paths and types must match the schema. Invalid or unknown field paths must fail loudly with nearest-match suggestions rather than silently dropping fields at deploy time.

- **Reproducibility**:
  Given the same blueprint and provider versions (or `.cf.lock`), generation must always produce the exact same byte-for-byte outputs.

---

## 2. BDD Loop & Testing Discipline

- **Behavior-Driven Verification**:
  Every feature, fix, or refactor must be backed by automated tests:
  - Unit tests within relevant packages (`internal/emit`, `internal/blueprint`, `internal/api`, `internal/mcp`, etc.).
  - Cross-language API contract tests (`internal/api/contract_fixtures_test.go` asserting shapes against `internal/api/testdata/contract/*.json`).
  - End-to-end acceptance tests (`acceptance_test.go`).

- **Verify Before Completion**:
  Always run `go test ./...` across the entire workspace before concluding tasks or submitting changes.

---

## 3. Git & Code Hygiene

- **Never Use Blanket Staging**:
  Never execute `git add -A` or `git add .` blindly. Explicitly review and stage only the specific files modified or created for the intended task.

- **Format Code Before Commit**:
  Always run `gofmt -w` (or `go fmt ./...`) on all modified Go files before committing. Keep formatting clean and idiomatic.

- **Preserve Documentation Integrity**:
  Maintain docstrings, architectural commentary, and inline explanation blocks. Do not strip unrelated comments or explanations when refactoring.
