# CF-250 — One malformed JSON line on stdin exits cf mcp with status 1 instead of a JSON-RPC parse error

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `cmd/cf/mcp.go`, `internal/mcp/`

## Background & Problem
`cf mcp` runs the go-sdk stdio transport (`cmd/cf/mcp.go:80`). In go-sdk v1.7.0 `mcp/transport.go:462-476` a `json.Decoder` error on stdin is queued as a fatal read error, `ioConn.Read` (`:600-602`) returns it, the session ends, `Server.Run` returns the error (`mcp/server.go:1304-1310`) and kong prints it and exits 1. The same path fires for a batch under protocol 2025-06-18 (`transport.go:618-619`), an empty batch (`:657`), a message without `"jsonrpc":"2.0"`, and Content-Length-framed input. JSON-RPC 2.0 specifies a `-32700` Parse error / `-32600` Invalid Request response for these; the session is what is lost instead.

Repro (`.testrun-qa/proto2.py`; after a successful `initialize` + `notifications/initialized`, one raw line each in a fresh process):
```
>>> not json at all
    no response; exit status 1; stderr: cf: error: invalid character 'o' in literal null (expecting 'u')
>>> {"id":8,"method":"ping"}
    no response; exit status 1; stderr: cf: error: invalid message version tag ""; expected "2.0"
>>> [{"jsonrpc":"2.0","id":8,"method":"ping"},{"jsonrpc":"2.0","id":9,"method":"ping"}]
    no response; exit status 1; stderr: cf: error: JSON-RPC batching is not supported in 2025-06-18 and later (request version: 2025-06-18)
```
A well-formed `ping` sent after the bad line gets EPIPE.

## Expected Behavior & Contract
When malformed or non-JSON input or invalid framed lines arrive on stdin:
- The server responds with a JSON-RPC error response (`-32700` Parse error or `-32600` Invalid Request) or logs the offending line to stderr and drops it.
- The session does NOT terminate prematurely with exit 1; the server continues running and responds to subsequent valid JSON-RPC requests.
- The process terminates only when stdin is closed (EOF) or when an OS termination signal is received.

## Acceptance Test
- An automated integration test driving `cf mcp` stdio stream with an `initialize` handshake, followed by a malformed line (e.g. `not json at all\n`), followed by a valid JSON-RPC request (e.g. `{"jsonrpc":"2.0","id":2,"method":"ping"}` or `tools/list`), verifying that the session survives and the subsequent request receives a valid response.
