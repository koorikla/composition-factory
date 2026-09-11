# CF-208 — Environment keys added in the inspector are permanently named key1 with no rename affordance

## Severity & Scope
- **Severity**: P2
- **Scale**: ux
- **Touch set**: `web-proto/js/regions/inspector.js`, `web-proto/js/regions/palette.js` (if needed), `tests/cf208-env-key-rename.spec.js`

## Background & Problem
In the EnvironmentConfig inspector, clicking `+ Add key` (`#envAddKeyBtn`) immediately creates an entry named `key1` (or `key2`, etc.) via `addEnvKey("key1", { type: "string" })`.
The key name input is rendered as:
`<input class="tin bold" data-env-name="key1" value="key1" readonly style="flex:1;min-width:70px" aria-label="Key name">`

Because the input has the `readonly` attribute, typing into it does nothing. There is no rename button or edit affordance.
As a result, any environment key added through the Inspector is permanently stuck with the auto-generated placeholder name `key1` in the GUI.

## Contract & Expected Behavior
1. In `web-proto/js/regions/inspector.js`:
   - Remove `readonly` from `data-env-name` input so the key name can be edited directly.
   - Listen for `change` / `blur` / `keydown` (Enter) on `data-env-name` inputs to commit rename.
   - Validate new key name:
     - Must not be empty.
     - Must be a valid identifier (no spaces, valid characters).
     - Must not collide with an existing environment key name.
     - If invalid or identical to old name, revert input to old name (and show error toast/warn if invalid).
   - If valid, invoke `renameEnvKey(oldKey, newKey)`:
     - Update all references in `doc.spec.resources`:
       - `res.fields`: any field with `from: "$env.<oldKey>"` or `from: "env.<oldKey>"` updated to `...<newKey>`.
       - `res.annotations`: any annotation with reference to `$env.<oldKey>`.
       - `res.envelope`: any envelope field with reference to `$env.<oldKey>`.
     - Update `doc.spec.environment`: replace `oldKey` with `newKey` while preserving its attributes (`type`, `required`, `value`, `default`, etc.).
2. When clicking `+ Add key` (`#envAddKeyBtn`), focus and select the name input of the newly added key so the user can immediately type its name.
3. If wires were attached to the renamed key, they must remain intact and point to `$env.<newKey>`.

## Acceptance Test
Create `tests/cf208-env-key-rename.spec.js` using Playwright:
1. Open canvas with EnvironmentConfig card present (or create one).
2. Inspect the EnvironmentConfig card.
3. Click `+ Add key` (`#envAddKeyBtn`).
4. Verify the newly created row has an editable (not readonly) key name input.
5. Change the key name from `key1` to `clusterName` and trigger change/blur.
6. Verify `clusterName` is reflected in the blueprint doc (`doc.spec.environment.clusterName`).
7. Wire a resource field to `$env.clusterName`.
8. Rename `clusterName` to `productionCluster`.
9. Verify the wire updates to `$env.productionCluster` and canvas remains valid.
