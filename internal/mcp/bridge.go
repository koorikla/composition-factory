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

	"github.com/koorikla/compositionfactory/internal/api"
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
func (s *server) call(method, path string, body []byte) (status int, respBody []byte, header http.Header, err error) {
	return s.callWithHeaders(method, path, body, nil)
}

// callWithHeaders serves one request against the api handler with optional
// request headers (such as If-Match for optimistic concurrency control).
func (s *server) callWithHeaders(method, path string, body []byte, headers http.Header) (status int, respBody []byte, respHeader http.Header, err error) {
	var r *http.Request
	if body == nil {
		r, err = http.NewRequest(method, "http://cf.local"+path, nil)
	} else {
		r, err = http.NewRequest(method, "http://cf.local"+path, bytes.NewReader(body))
	}
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, vv := range headers {
		for _, v := range vv {
			r.Header.Add(k, v)
		}
	}
	// No Accept-Encoding and no If-None-Match: the bridge never wants a
	// gzipped body or a 304, and not sending the headers is how HTTP asks
	// for neither.

	rec := api.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec.Status(), rec.Body(), rec.Header(), nil
}

// result converts a bridged response into the tool result: an error status
// surfaces the response's {"error": "..."} message VERBATIM as the tool
// error (the SDK packs an error's text into the isError result the agent
// sees — never paraphrase it, it names the offending field precisely), and a
// success returns the JSON body as the result's one text content.
func result(status int, body []byte) (*sdk.CallToolResult, any, error) {
	return resultWithHeader(status, body, nil)
}

// resultWithHeader converts a bridged response into the tool result and attaches
// any response ETag as revision/etag metadata on the tool result.
func resultWithHeader(status int, body []byte, header http.Header) (*sdk.CallToolResult, any, error) {
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
	res := textResult(body)
	if header != nil {
		if etag := header.Get("ETag"); etag != "" {
			res.Meta = sdk.Meta{
				"revision": etag,
				"etag":     etag,
			}
		}
	}
	return res, nil, nil
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
	status, resp, header, err := s.call(method, path, body)
	if err != nil {
		return nil, nil, err
	}
	return resultWithHeader(status, resp, header)
}
