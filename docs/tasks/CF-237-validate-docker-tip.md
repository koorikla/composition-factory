# CF-237 — Validate appends the Docker fix tip to a template render error although Docker is running

## Severity & Scope
- **Severity**: P2
- **Scale**: ux
- **Touch set**: `web-proto/js/regions/output.js`, `tests/cf237-validate-docker-tip.spec.js`, `docs/tasks/CF-237-validate-docker-tip.md`

## Background & Problem
In `web-proto/js/regions/output.js:416-424` (`diagnoseError`), any error message containing the words `docker` or `daemon` causes `diagnoseError` to classify it as an environment failure and append:
`💡 Environment Fix Tip: Make sure Docker Desktop or dockerd is running and accessible.`

However, when `crossplane render` is executed via Docker, render error messages from the Go backend are prefixed:
`crossplane internal render in Docker: pipeline returned fatal: …`
Because "docker" is present in this prefix, even when Docker is fully functional and running, template rendering failures (such as syntax errors, missing parameter keys, etc.) trigger the misleading Docker environment fix tip.

## Expected Behavior & Contract
The Docker environment fix tip should only be appended when Docker itself is unavailable, unreachable, or stopped (e.g. `Cannot connect to the Docker daemon`, `Is the docker daemon running?`, `docker: command not found`, `daemon not accessible`).
It must NOT be appended when Docker successfully ran and the pipeline inside it failed with a template or composition error (e.g. errors prefixed `crossplane internal render in Docker: pipeline returned fatal:` or containing `pipeline returned fatal:`).

## Acceptance Test
- Playwright spec `tests/cf237-validate-docker-tip.spec.js` asserting that pipeline fatal errors during validation do not display the Docker environment fix tip.
