# CF-248 — README Container badge links to a 404 GitHub package page after the repo rename

## Severity & Scope
- **Severity**: P3
- **Scale**: engine
- **Touch set**: `README.md`

## Background & Problem
In `README.md:6`:
`[![Container](https://img.shields.io/badge/ghcr.io-compositionfactory-blue?logo=docker)](https://github.com/koorikla/compositionfactory/pkgs/container/compositionfactory)`
When clicked or fetched via `curl -sI -L`, the link target returns HTTP 404 because the GitHub repository was renamed from `koorikla/compositionfactory` to `koorikla/composition-factory`. GitHub's rename redirect does not cover `/pkgs/container/...`.

## Expected Behavior & Contract
The Container badge in `README.md` should link to a valid page (e.g. `https://github.com/koorikla/composition-factory/packages`) which returns HTTP 200, rather than a 404 page.

## Acceptance Test
- `curl -sI -L <badge_target_url>` returns HTTP 200 OK.
