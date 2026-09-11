# CF-242 — get_kind_fields/rename_* with an empty name arg return ServeMux's 307 redirect page as a success

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/mcp/tools.go`, `internal/mcp/tools_test.go` (or `bridge_test.go`)

## Background & Problem
`internal/mcp/bridge.go:56-69` treats every HTTP status below 400 as a success.
`internal/mcp/tools.go` builds route paths using `url.PathEscape(in.APIVersion)`, `url.PathEscape(in.Kind)`, and `url.PathEscape(in.Name)` without rejecting empty strings.
When an empty string is supplied:
- `get_kind_fields` with empty `api_version` or `kind` requests `/api/kinds///fields` or `/api/kinds/<api_version>//fields`.
- `rename_parameter` / `rename_resource` with empty `name` requests `/api/blueprint/parameters//rename`.
- `update_parameter`, `delete_parameter`, `update_resource`, `delete_resource` with empty `name` request `/api/blueprint/parameters/` or `/api/blueprint/resources/`.

Go's standard `http.ServeMux` cleans paths with consecutive slashes by answering with a 307 Temporary Redirect to the normalized path. The MCP bridge sees 307 (< 400), treats it as success, and returns the HTML redirect page or empty body back to the MCP client with `isError: false`.

## Expected Behavior & Contract
Tools in `internal/mcp/tools.go` must validate required identifier arguments before invoking the HTTP bridge:
- `getKindFields`: `in.APIVersion` and `in.Kind` must not be empty.
- `updateParameter`, `renameParameter`, `deleteParameter`: `in.Name` must not be empty.
- `updateResource`, `renameResource`, `deleteResource`: `in.Name` must not be empty.
When any required name or identifier argument is empty, the tool must return an error result (`isError: true`) naming the empty argument (e.g. `"name is required"`).

## Acceptance Test
- Unit test in `internal/mcp/` asserting that calling `get_kind_fields`, `rename_parameter`, `delete_parameter`, `rename_resource`, and `delete_resource` with empty string arguments returns an error result (`isError: true`) with a message naming the missing argument.
