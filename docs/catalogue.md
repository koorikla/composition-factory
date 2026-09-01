# Provider discovery catalogue

`GET /api/catalogue` answers a question `GET /api/providers` cannot: not
"what is this server currently serving", but "what crossplane-contrib
providers and functions *exist* and could be added". This document is why
that has to be a static, CI-refreshed list rather than a live query, how the
list is built, and how to regenerate it — including in an environment where
a compiled Go binary cannot reach the network at all, which is how this
file's own `catalogue/providers.json` was generated.

## Why static

A live "list every provider" endpoint would need the registries to support
it, and they don't, for an anonymous client:

- `xpkg.upbound.io/v2/<repo>/tags/list` returns an **empty** tag list for
  anonymous requests — there is no tag-discovery API to poll.
- `ghcr.io/v2/_catalog` and `xpkg.upbound.io/v2/_catalog` both return
  `401`/`403` to an anonymous token. Repository enumeration is not part of
  the anonymous OCI distribution surface either registry exposes.

What *does* work anonymously is the two things this catalogue is built
from:

- GitHub's REST API lists a GitHub org's repositories with no
  authentication at all (rate-limited to 60 requests/hour, which
  crossplane-contrib's ~140 repos fit into with 2 requests).
- ghcr.io grants an anonymous, pull-scoped bearer token per repository
  (`GET /token?service=ghcr.io&scope=repository:<owner>/<repo>:pull`), and a
  token holder can then list that one repository's tags
  (`GET /v2/<owner>/<repo>/tags/list`).

So "what providers exist" has to be assembled ahead of time, from those two
per-repository-scoped calls run across every repository in the org, and
served as a fixed snapshot — not answered per request.

## What's in it

`scripts/build-catalogue` enumerates crossplane-contrib's `provider-*` and
`function-*` GitHub repositories (excluding forks) and, for each one, tries
to resolve the latest **stable** tag of its `ghcr.io/crossplane-contrib/<repo>`
image — a strict `vMAJOR.MINOR.PATCH` tag, never a pre-release
(`v1.2.3-rc1`) and never one of the Go pseudo-version tags several of these
repos push to ghcr.io on every commit to `main`
(`v0.0.0-20251028114116-30cc3a089783` — these outnumber real releases by
roughly 10:1 in the tag lists this generator observed).

The result is written to `catalogue/providers.json`:

```json
[
  {
    "name": "function-go-templating",
    "ref": "ghcr.io/crossplane-contrib/function-go-templating:v0.11.0",
    "description": "A Go templating composition function",
    "source": "https://github.com/crossplane-contrib/function-go-templating",
    "license": "Apache-2.0"
  }
]
```

Sorted by `name`, one entry per repository, five fields always present.

### Policy: label, don't hide

Not every `provider-*`/`function-*` repository has a resolvable
`ghcr.io/crossplane-contrib/<repo>` image. When this catalogue was first
generated, **63 of 108** matching repositories had none — most of the large
upjet provider families (`provider-upjet-aws`, `provider-upjet-gcp`, …)
publish many per-service images under `xpkg.upbound.io/upbound/provider-<service>`
instead of one monolithic `ghcr.io/crossplane-contrib/<repo>` image, and a
handful of archived repositories no longer have a package under that path at
all.

Those repositories are **not dropped from the catalogue**. Each still gets
an entry — `name`, `description`, `source` and `license` from GitHub, same
as any other — with `ref: ""` labelling that no installable image reference
could be resolved. A caller that only wants installable entries filters on
`ref != ""` itself; the generator does not make that filtering decision for
every consumer.

`license` follows the same rule: a repository GitHub reports no detected
license for gets `"NOASSERTION"` (the SPDX placeholder for "no license
asserted"), not an empty string a caller could mistake for "checked and
found to have none".

## The HTTP endpoint

`GET /api/catalogue?q=` — same shape as `GET /api/kinds`:

```json
{"providers": [ /* catalogue.Provider */ ]}
```

`q`, if present, is matched case-insensitively as a substring against each
entry's `name` **or** `description`; omitted or empty, every entry is
returned. Like every other route in this server, the response is ETag-cached
and gzip-compressed by the shared middleware in `internal/api/server.go` —
`internal/api/catalogue.go` is only a filter over an already-parsed,
already-validated in-memory slice.

The catalogue is embedded into the binary at compile time
(`catalogue/catalogue.go`'s `//go:embed providers.json`), not read from disk
at startup — the server has one static list, the same for every process and
every request, until the next release.

## Regenerating it

```
go run ./scripts/build-catalogue --out catalogue/providers.json
```

This is **live mode**: it talks to `api.github.com` and `ghcr.io` directly,
anonymously, with a bounded amount of concurrency (8 requests to ghcr.io in
flight at once — see `ghcrConcurrency` in `scripts/build-catalogue/ghcr.go`).
`.github/workflows/catalogue.yml` runs exactly this, on a weekly cron,
opening a PR when the output changes.

### Offline mode, and why it exists

```
go run ./scripts/build-catalogue --from-file manifest.json --out catalogue/providers.json
```

Every test in `scripts/build-catalogue` runs this way — no test in this
project makes a real network request — and this repository's own dev
sandbox needs it too: **compiled Go binaries in that sandbox cannot reach
the network at all**, only `curl` can. `catalogue/providers.json` as
committed in this repository was generated exactly this way: `curl` did the
live fetching, and its output was reshaped into a manifest the (offline,
sandboxed) generator could consume.

A manifest is a JSON object with two keys:

```json
{
  "repos": [
    {"name": "function-go-templating", "description": "...", "html_url": "...", "license_spdx_id": "Apache-2.0"}
  ],
  "tags": {
    "function-go-templating": ["v0.9.2", "v0.10.0", "v0.11.0", "v0.0.0-20251028114116-30cc3a089783"]
  }
}
```

`repos[].name`/`description`/`html_url`/`license_spdx_id` line up directly
with GitHub's own repo-list API response fields (`name`, `description`,
`html_url`, `license.spdx_id`), and `tags[<repo>]` is exactly what
`ghcr.io/v2/<owner>/<repo>/tags/list` returns for that repo's `tags` key —
unfiltered; `scripts/build-catalogue/build.go`'s `latestStableTag` does the
filtering. A repo present in `repos` but absent (or empty) in `tags` is
treated the same way live mode treats a repo whose ghcr.io token request was
denied: no resolvable ref, entry still present (see "label, don't hide"
above).

The curl recipe that built this repository's manifest, roughly:

```bash
# 1. Enumerate the org (paginate until a page is short of 100).
curl -s "https://api.github.com/orgs/crossplane-contrib/repos?per_page=100&page=1" -o page1.json
curl -s "https://api.github.com/orgs/crossplane-contrib/repos?per_page=100&page=2" -o page2.json

# 2. For each provider-*/function-* repo (non-fork), resolve its tags:
token=$(curl -s "https://ghcr.io/token?service=ghcr.io&scope=repository:crossplane-contrib/<repo>:pull" \
  | jq -r .token)
curl -s -H "Authorization: Bearer $token" \
  "https://ghcr.io/v2/crossplane-contrib/<repo>/tags/list" -o tags/<repo>.json

# 3. Assemble page1.json + page2.json + tags/*.json into one manifest.json
#    (repos: [...], tags: {<repo>: [...]}) and run the generator against it.
go run ./scripts/build-catalogue --from-file manifest.json --out catalogue/providers.json
```

`scripts/build-catalogue/testdata/manifest.json` is a small, hand-written
manifest in this exact shape, used by the package's own tests.

## Testing

- `catalogue/catalogue_test.go` validates the **real, committed**
  `catalogue/providers.json`: non-empty, parses, sorted by name, no
  duplicates (`TestEmbeddedCatalogueIsValid`) — this is the gate that fails
  `go test ./...` if a future regeneration ever produces a broken file.
- `scripts/build-catalogue/*_test.go` covers the generator itself against
  fake fixtures and an `httptest` server standing in for `api.github.com`
  and `ghcr.io` — tag selection (stable vs. pre-release vs. pseudo-version),
  the label-don't-hide policy, pagination, rate-limit handling, and
  deterministic output — with zero real network access.
- `internal/api/catalogue_test.go` covers the HTTP route: the full list, the
  `q` filter, and participation in the shared ETag/gzip middleware.
- `internal/api/contract_fixtures_test.go`'s
  `TestContractFixtureCatalogueRoundTripsKeySet` checks
  `web/src/api/fixtures/catalogue.json` against the same
  `{"providers": [...]}` envelope the Go handler actually serves, the same
  cross-language contract check every other route in this API gets.
