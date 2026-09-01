// This file is the in-process bridge from a tool call to the HTTP API: it
// builds a real *http.Request, serves it straight through the api.New
// handler with no listener or socket involved, and hands back the recorded
// status and body. See the package comment for why the bridge exists instead
// of extracted handler cores.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// call serves one request against the api handler in process. path must be
// the full request target (path plus any encoded query); a nil body sends no
// request body, matching a browser's bodyless GET/DELETE.
//
// The URL is assembled around a fixed dummy origin because http.NewRequest
// wants an absolute URL; the handler routes on the path alone. Percent
// escapes inside path (an apiVersion's %2F) survive parsing in URL.RawPath,
// which is exactly the escaped form ServeMux (1.22+) segments on — the same
// mechanism the browser client relies on, verified in internal/api's
// pathAPIVersion.
func (s *server) call(method, path string, body []byte) (status int, respBody []byte, err error) {
	var r *http.Request
	if body == nil {
		r, err = http.NewRequest(method, "http://cf.local"+path, nil)
	} else {
		r, err = http.NewRequest(method, "http://cf.local"+path, bytes.NewReader(body))
	}
	if err != nil {
		return 0, nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	// No Accept-Encoding and no If-None-Match: the bridge never wants a
	// gzipped body or a 304, and not sending the headers is how HTTP asks
	// for neither.

	rec := &recorder{header: make(http.Header), status: http.StatusOK}
	s.handler.ServeHTTP(rec, r)
	return rec.status, rec.body.Bytes(), nil
}

// result converts a bridged response into the tool result: an error status
// surfaces the response's {"error": "..."} message VERBATIM as the tool
// error (the SDK packs an error's text into the isError result the agent
// sees — never paraphrase it, it names the offending field precisely), and a
// success returns the JSON body as the result's one text content.
func result(status int, body []byte) (*sdk.CallToolResult, any, error) {
	if status >= http.StatusBadRequest {
		var e struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
			return nil, nil, errors.New(e.Error)
		}
		// Unreachable with the current api handler (every error response is
		// the one JSON error shape, normalized in wrap) — kept so a future
		// divergence fails loudly instead of returning an empty error.
		return nil, nil, fmt.Errorf("unexpected HTTP %d response: %s", status, body)
	}
	return textResult(body), nil, nil
}

// textResult wraps a JSON response body as a tool result's text content.
func textResult(body []byte) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: string(body)}},
	}
}

// bridge is the common happy path shared by the tools that need no
// post-processing: call, then convert.
func (s *server) bridge(method, path string, body []byte) (*sdk.CallToolResult, any, error) {
	status, resp, err := s.call(method, path, body)
	if err != nil {
		return nil, nil, err
	}
	return result(status, resp)
}

// recorder is a minimal http.ResponseWriter buffering the handler's whole
// response — a small deliberate duplicate of internal/api's unexported
// recorder (the same precedent as that package's own splitYAMLStream copy:
// the original is private to its package, and twelve lines are not worth an
// export or an httptest dependency in non-test code).
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
