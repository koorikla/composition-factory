# Composition Factory Guide

Welcome to Composition Factory — the visual canvas and schema-aware generator for Crossplane v2 Compositions and CompositeResourceDefinitions (XRDs).

---

## 1. The Composition Loop

1. **Discover & Add Sources**: Use the **Sources** rail or **Catalogue** to add provider packages (e.g. AWS RDS, SQS, IAM) or scan live cluster CRDs.
2. **Compose Kinds**: Drag kinds from the **Kinds** palette onto the canvas. Every field validates against the real OpenAPI schema from the provider CRD.
3. **Wire Parameters & Status**: Drag parameter dots directly onto resource cards to bind inputs (`params.<name>`), or drag output status ports onto target fields/annotations (`resources.<name>.status.atProvider.<field>`).
4. **Inspect & Refine**: Select any resource or the XRD composite card to customize fields, envelopes (`providerConfigRef`, `writeConnectionSecretToRef`), pipeline steps, or flow control (`when`, `forEach`).
5. **Live Generation**: The **Generated** drawer continuously generates byte-deterministic `composition.yaml`, `definition.yaml`, and `functions.yaml`.

---

## 2. Wire & Port System

| Wire Type | Color | Description |
| :--- | :--- | :--- |
| **XRD Spec** | Blue (`--wire-xrd`) | Connects an XRD parameter to a resource input field (`from: params.X`). |
| **Shared** | Olive (`--shared`) | Connects a single parameter feeding multiple resource fields across cards. |
| **Status Wire** | Teal (`--wire-status`) | Connects an observed status output from one resource into a dependent field or annotation (`from: resources.A.status.atProvider.X`). |
| **Native Ref** | Rust (`--wire-ref`) | References between native Kubernetes objects. |

---

## 3. Keyboard & Gesture Shortcuts

- **Copy / Duplicate**: `⌘C` / `Ctrl+C` on a selected resource card to copy; `⌘V` / `Ctrl+V` to paste a duplicate with distinct naming and preserved wiring.
- **Delete**: `Delete` / `Backspace` on a selected card removes the resource (prompts confirmation if active wires would drop).
- **Zoom**: Mouse wheel zooms centered at the cursor; `+` and `-` controls in the bottom-right bar.
- **Pan**: `Shift + Scroll` or click-and-drag the empty canvas ground.
- **Reset View**: `⌂` button resets zoom to 100% and centers the active blueprint.
- **Undo / Redo**: `⌘Z` / `Ctrl+Z` to undo; `⇧⌘Z` / `Shift+Ctrl+Z` to redo.

---

## 4. Starter Blueprints

Composition Factory includes curated starter blueprints accessible via the **Examples** topbar button or the **Guide** tab:

- ⚡ **AWS IRSA (IAM Role + EKS ServiceAccount)**: IAM Role with scoped assume-role trust policy template and native K8s ServiceAccount wired to the Role's observed ARN via `eks.amazonaws.com/role-arn` annotation.
- 🗄️ **AWS RDS PostgreSQL**: AWS RDS DB Instance with storage, compute class, engine version, multi-AZ parameters, and credentials connection secret envelope.
- 📦 **K8s Microservice App**: Full-stack application combining native Kubernetes `Deployment` & `Service` with an AWS `Queue` and IAM IRSA `Role`.

---

## 5. Live Render Validation

Click **Validate** in the topbar to execute a real `crossplane composition render` against an XR synthesized from your XRD. The status chip reports the composed resource count or exact Crossplane engine validation errors.
