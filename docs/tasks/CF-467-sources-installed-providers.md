# CF-467 — SOURCES tab lists cached providers as installed, hiding Add button and search for RDS, IAM

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 |
| **Closes** | `CF-467 — SOURCES tab lists cached providers as installed, hiding Add button and search for RDS, IAM` (#364) |
| **Worktree** | `.worktrees/CF-467-sources` on branch `CF-467-sources-installed-providers`, branched from `main` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/cf467-sources-installed-providers.spec.js` |

## Symptom

When starting `cf serve` on an empty or untitled blueprint (`spec.sources: []`) with cached providers present in the schema cache:
1. The SOURCES tab -> Providers lists every cached package under "Installed Providers", even though `doc.spec.sources` is empty.
2. In the Providers Catalogue (`#cat-search`), searching for `RDS` or `iam` shows the packages (`provider-aws-rds`, `provider-aws-iam`) marked as `Installed · 44 kinds`, hiding the `Add` button.
3. The only text box next to an `Add` button is `#src-add-ref` (styled with `class="search"`), where entering service names like `RDS` or `iam` errors with:
   `parse reference "RDS": could not parse reference: RDS`
4. As a result, a user cannot discover or add required providers (like RDS or IAM) to their blueprint through the UI.

## Root Cause

In `web-proto/js/regions/palette.js:616-620`:
```javascript
  let sources = providers !== null
    ? providers.map(function (p) { return { provider: p.ref, digest: p.digest, kinds: p.kinds, error: p.error || "" }; })
    : (doc.spec && doc.spec.sources || []).slice();
```
`sources` is overridden with all providers returned by `GET /api/providers` (which lists all packages the server holds in memory/cache).
Furthermore, in the catalogue search (`palette.js:707-712`):
```javascript
  var isInstalled = (sources || []).some(function (s) {
    if (s.error) return false;
    var sp = (s.provider || "").split(":")[0];
    var cr = (c.ref || "").split(":")[0];
    return s.provider === c.ref || (cr && sp && sp === cr);
  });
```
`isInstalled` checks against `sources`, treating every cached provider as already installed in the active blueprint document.

## Acceptance Contract

1. The SOURCES "Installed Providers" list must reflect only the sources declared in the active blueprint document (`doc.spec.sources`), plus the native `k8s` provider (if native kinds exist).
   - Each declared source can be enriched with status/digest/kinds/error from `providers` (from `GET /api/providers`).
   - If `doc.spec.sources` is empty, "Installed Providers" should show `0` (or `1` if `k8s` native provider is present), and display "No sources declared." if no non-native sources are declared.
2. Providers that are present in the local cache or server index but not declared in `doc.spec.sources` must remain addable via the Providers Catalogue with a visible `Add` button (`isInstalled` evaluates to false).
3. Searching for `RDS`, `iam`, or other keywords in `#cat-search` displays matching catalogue providers with an enabled `Add` button when they are not in `doc.spec.sources`.
4. Clicking `Add` on a catalogue provider calls `api.addProvider(ref)`, adds the provider to `doc.spec.sources`, and updates the UI so it becomes installed.
5. Guarded by Playwright test `tests/cf467-sources-installed-providers.spec.js`.

## Verification

```sh
npm run lint:js
make lint
make lint-strict
make test-race
npx playwright test tests/cf467-sources-installed-providers.spec.js
```
