# CF-139 — A drawn status wire's binding is not readable untruncated anywhere

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: discoverability / ergonomics) |
| **Closes** | `CF-139 — A drawn status wire's binding is not readable untruncated anywhere` |
| **Worktree** | `.worktrees/CF-139` on branch `CF-139-status-wire-untruncated-binding` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/inspector.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom

After wiring a long status path (such as `resources.role.status.atProvider.arn` into a ServiceAccount annotation), the card row truncates (`…amazonaws.com/role-arn`) and the inspector shows `EKS.AMAZONAWS.COM/R… ARN ← resources.role.status….`. The complete source → target pair cannot be read untruncated anywhere in the interface.

## Evidence

In Journey J2 (`docs/ux-runs/2026-09-11-m2-irsa-status-wire.md` finding F6):
- Status wires with deep JSON paths or reverse-DNS annotation keys truncate at both endpoints.
- Hovering or clicking provides no expanded view of the full binding expression.

## Contract

1. When hovering over a status wire or its endpoints on a card, a tooltip or title attribute displays the complete, untruncated binding: `<sourceResource>.status.<sourcePath> → <targetResource>.<targetPath>`.
2. In the inspector's Wire list and field rows, hovering or clicking the binding exposes the full untruncated expression without horizontal layout breakage.
3. Wire selection displays the full binding in the canvas status/hint bar or wire detail badge.

## Acceptance Test

Write Playwright test `tests/cf139-untruncated-status-wire.spec.js`:
1. Load a blueprint with a long status wire binding (`role.status.atProvider.arn` to `eks.amazonaws.com/role-arn`).
2. Verify that hovering or selecting the wire or inspector row provides accessible text containing the full, untruncated source and target paths.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-139-status-wire-untruncated-binding`, committed, not pushed, not merged. Final report includes failing and passing runs of the acceptance test.
