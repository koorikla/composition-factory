// This file declares the MCP tools and their handlers. Every tool mirrors
// exactly one HTTP route (the mapping is named on each registration below),
// bridged in process — see the package comment in server.go.
//
// Input conventions:
//
//   - Tool and property names are snake_case; each maps onto the HTTP
//     route's own path/query/body parameter, spelled for an agent caller.
//   - Whole-document and whole-parameter inputs (replace_blueprint's
//     blueprint, add_parameter/update_parameter's parameter) are held as
//     json.RawMessage and forwarded to the HTTP layer BYTE-FOR-BYTE. This is
//     load-bearing: internal/api decodes every body with
//     DisallowUnknownFields, so a typo'd key must reach that decoder intact
//     to be rejected with the same error a browser client would see.
//     Re-decoding into typed structs here would silently drop the very keys
//     that gate exists to catch. Those three tools carry hand-written input
//     schemas because the SDK's reflection would otherwise describe
//     json.RawMessage as a byte array.
//   - Numeric and boolean filters are typed (integer/boolean) rather than
//     the strings HTTP query parameters are, so the one class of request
//     the typed schema cannot express is the malformed number
//     (limit=abc) — which is a request an agent should never be able to
//     make, not a behavior gap.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// register adds every tool to srv. The set mirrors internal/api's route
// table; GET /api/kinds/{apiVersion}/{kind} (the envelope route) is the one
// route without a tool, per this milestone's declared tool list.
func (s *server) register(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "list_kinds",
		Description: "Search the managed-resource kinds available from the cached provider schemas. " +
			"Returns {\"kinds\":[...]}, each entry carrying kind, group, version, apiVersion, plural, " +
			"scope, provider (the xpkg ref it came from), namespaced, and the counts of forProvider " +
			"fields (fields) and required forProvider fields (required). Providers often ship a kind " +
			"twice — a namespaced and a cluster-scoped variant with different apiVersions — so match on " +
			"apiVersion, not kind alone. Omit search and limit to list everything.",
	}, s.listKinds)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_kind_fields",
		Description: "List the settable forProvider fields of one kind, addressed by dotted path " +
			"(e.g. containers[0].image). Returns {\"fields\":[...],\"total\":N}, each field carrying " +
			"path, type, description, required and depth; total counts the filtered set BEFORE limit " +
			"truncates it, so total > len(fields) means the response was cut short. Provider schemas " +
			"run to hundreds of fields — start with required_only:true or max_depth:1 and drill into " +
			"one subtree with prefix rather than fetching everything.",
	}, s.kindFields)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_blueprint",
		Description: "Read the current blueprint document — the single source of truth every edit " +
			"and generation works from. Returns the whole document as JSON: apiVersion, kind, metadata, " +
			"and spec with sources (provider refs), xrd (group/kind/plural/version/scope and the " +
			"parameters map) and resources (each with name, kind, provider and a fields map whose " +
			"values set exactly one of from/value/raw). Always re-read after editing; it is the exact " +
			"shape replace_blueprint expects back.",
	}, s.getBlueprint)

	replaceBlueprint := &sdk.Tool{
		Name: "replace_blueprint",
		Description: "Replace the whole blueprint document with the one given, validate it, and " +
			"persist it to disk. This is a full replace, not a merge: send the complete document in " +
			"the exact shape get_blueprint returns, with your changes applied. Unknown keys anywhere " +
			"in the document are rejected, and a document that fails validation is refused with the " +
			"file left untouched — the error names the offending field path. Prefer the parameter " +
			"tools for single-parameter edits; use this for structural changes (resources, sources, " +
			"xrd identity).",
	}
	replaceBlueprint.InputSchema = mustSchemaJSON(`{
		"type": "object",
		"properties": {
			"blueprint": {
				"type": "object",
				"description": "The complete blueprint document, in the exact JSON shape get_blueprint returns."
			}
		},
		"required": ["blueprint"],
		"additionalProperties": false
	}`)
	sdk.AddTool(srv, replaceBlueprint, s.replaceBlueprint)

	addParameter := &sdk.Tool{
		Name: "add_parameter",
		Description: "Declare a new XRD parameter on the blueprint and persist it. The name must be " +
			"camelCase and not already declared (a duplicate is refused). The parameter object's keys " +
			"are type (string|integer|number|boolean|object — required), required (boolean), enum " +
			"(array of strings), default (string; also used as the template default), description, " +
			"and — on type object only — properties (a map of member name to the same declaration " +
			"shape, scalar member types only, one level deep; members are wired as " +
			"params.<name>.<member>); omitted keys mean unset, unknown keys are rejected. Returns " +
			"the updated document.",
	}
	addParameter.InputSchema = mustSchemaJSON(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "The new parameter's name (camelCase, e.g. maxMessageSize)."
			},
			"parameter": {
				"type": "object",
				"description": "The parameter declaration: {type, required, enum, default, description}."
			}
		},
		"required": ["name", "parameter"],
		"additionalProperties": false
	}`)
	sdk.AddTool(srv, addParameter, s.addParameter)

	updateParameter := &sdk.Tool{
		Name: "update_parameter",
		Description: "Replace an existing XRD parameter's declaration IN FULL and persist it — this " +
			"is not a patch. Omitting a key the parameter currently holds a value for is refused " +
			"rather than silently discarding that value: to clear a key, send it explicitly with its " +
			"zero value (false, null, \"\"). The parameter object's keys are type, required, enum, " +
			"default, description and (type object only) properties — typed members, refused as a " +
			"silent drop when omitted while declared; an unknown name is an error. Returns the " +
			"updated document.",
	}
	updateParameter.InputSchema = mustSchemaJSON(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "The declared parameter to replace."
			},
			"parameter": {
				"type": "object",
				"description": "The complete replacement declaration: {type, required, enum, default, description}."
			}
		},
		"required": ["name", "parameter"],
		"additionalProperties": false
	}`)
	sdk.AddTool(srv, updateParameter, s.updateParameter)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "rename_parameter",
		Description: "Rename an XRD parameter and rewrite every resource field that references it " +
			"(from: params.<name>), atomically, then persist. Renaming to the same name is a no-op " +
			"success; renaming to an already-declared name is refused. Returns the updated document.",
	}, s.renameParameter)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "delete_parameter",
		Description: "Delete an XRD parameter and persist the result. Refused if any resource field " +
			"still references it (the error names the referencing resources — rewrite or delete " +
			"those fields first) or if deleting it would leave the blueprint invalid (e.g. " +
			"providerName on a Namespaced XRD). Returns the updated document.",
	}, s.deleteParameter)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "add_provider",
		Description: "Fetch a Crossplane provider package from its OCI registry (network access " +
			"required), cache its CRD schemas, pin its digest into the lockfile, and make its kinds " +
			"available to list_kinds/get_kind_fields immediately. ref is an xpkg reference like " +
			"ghcr.io/org/provider-name:v1.2.3 (or ...@sha256:<digest>). A ref already cached is " +
			"refused. Returns {\"provider\":{ref,digest,kinds},\"kinds\":[...]} with the kinds it added.",
	}, s.addProvider)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "list_providers",
		Description: "List the provider packages this server is serving schemas from: " +
			"{\"providers\":[{\"ref\",\"digest\",\"kinds\"}]} in blueprint-source order, then add " +
			"order. kinds is a count; fetch the kinds themselves with list_kinds. A provider with " +
			"kinds: 0 is usually a family root package carrying only config types — normal, not a " +
			"failure.",
	}, s.listProviders)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "generate",
		Description: "Render the blueprint's artifacts — XRD, Composition and functions.yaml — " +
			"through the same engine `cf gen` uses, byte-identical to a CLI run. With write:false " +
			"(the default) nothing touches disk: a dry-run preview. With write:true the files are " +
			"written into the declared --out workspace (the only directory this server will write; " +
			"any path outside it is refused). Either way returns " +
			"{\"outputs\":[{\"path\",\"bytes\",\"body\"}],\"written\":bool} with each file's full " +
			"content, so preview before writing costs nothing. Fails with the engine's own error if " +
			"the blueprint references a kind or field its cached provider schemas do not define.",
	}, s.generate)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "render_check",
		Description: "Run a real `crossplane composition render` of the current blueprint's " +
			"generated artifacts against a synthesized sample XR, without writing anything to the " +
			"workspace. The render's outcome is the payload, always with every key present: " +
			"{\"ok\":bool,\"resources\":N,\"error\":\"...\",\"unavailable\":\"...\"}. ok:true means " +
			"the composition rendered N composed resources; error carries the render's own failure " +
			"output verbatim; unavailable means the environment cannot run the check at all (no " +
			"crossplane CLI on PATH, or no Docker daemon) — which says nothing about whether the " +
			"blueprint is correct. Slow on first use: the render may pull function images.",
	}, s.renderCheck)
}

// mustSchemaJSON parses a hand-written 2020-12 JSON schema literal. The
// literals above are compile-time constants, so a parse failure is a
// programming error caught by the very first test that constructs the
// server — panicking beats every tool silently advertising a broken schema.
func mustSchemaJSON(literal string) json.RawMessage {
	var probe map[string]any
	if err := json.Unmarshal([]byte(literal), &probe); err != nil {
		panic(fmt.Sprintf("invalid tool input schema literal: %v", err))
	}
	return json.RawMessage(literal)
}

// --- kinds ---

type listKindsInput struct {
	Search string `json:"search,omitempty" jsonschema:"Case-insensitive substring matched against each kind's name and group. Omit to list every kind."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of kinds to return. Omit or 0 for unlimited."`
}

// listKinds mirrors GET /api/kinds?q=&limit=.
func (s *server) listKinds(_ context.Context, _ *sdk.CallToolRequest, in listKindsInput) (*sdk.CallToolResult, any, error) {
	q := url.Values{}
	if in.Search != "" {
		q.Set("q", in.Search)
	}
	if in.Limit != 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	return s.bridge(http.MethodGet, withQuery("/api/kinds", q), nil)
}

type kindFieldsInput struct {
	APIVersion   string `json:"api_version" jsonschema:"The kind's apiVersion exactly as list_kinds returned it (group/version, e.g. sqs.aws.m.upbound.io/v1beta1)."`
	Kind         string `json:"kind" jsonschema:"The kind name exactly as list_kinds returned it (e.g. Queue)."`
	Prefix       string `json:"prefix,omitempty" jsonschema:"Only fields under this dotted path prefix (e.g. template.spec). Omit for the whole tree."`
	MaxDepth     int    `json:"max_depth,omitempty" jsonschema:"Only fields at most this deep; a top-level field has depth 0, so max_depth:1 returns depths 0 and 1. Omit or 0 for unlimited."`
	Search       string `json:"search,omitempty" jsonschema:"Case-insensitive substring matched against each field's path and description."`
	RequiredOnly bool   `json:"required_only,omitempty" jsonschema:"Only fields the provider schema marks required."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum number of fields to return; total still counts the whole filtered set. Omit or 0 for unlimited."`
}

// kindFields mirrors GET
// /api/kinds/{apiVersion}/{kind}/fields?prefix=&max_depth=&q=&required_only=&limit=.
func (s *server) kindFields(_ context.Context, _ *sdk.CallToolRequest, in kindFieldsInput) (*sdk.CallToolResult, any, error) {
	q := url.Values{}
	if in.Prefix != "" {
		q.Set("prefix", in.Prefix)
	}
	if in.MaxDepth != 0 {
		q.Set("max_depth", strconv.Itoa(in.MaxDepth))
	}
	if in.Search != "" {
		q.Set("q", in.Search)
	}
	if in.RequiredOnly {
		q.Set("required_only", "true")
	}
	if in.Limit != 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	path := "/api/kinds/" + url.PathEscape(in.APIVersion) + "/" + url.PathEscape(in.Kind) + "/fields"
	return s.bridge(http.MethodGet, withQuery(path, q), nil)
}

// --- blueprint ---

// getBlueprint mirrors GET /api/blueprint.
func (s *server) getBlueprint(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, any, error) {
	return s.bridge(http.MethodGet, "/api/blueprint", nil)
}

type replaceBlueprintInput struct {
	// Raw, not a typed struct: forwarded verbatim so internal/api's
	// DisallowUnknownFields decode sees exactly what the agent sent. See the
	// file comment.
	Blueprint json.RawMessage `json:"blueprint"`
}

// replaceBlueprint mirrors PUT /api/blueprint.
func (s *server) replaceBlueprint(_ context.Context, _ *sdk.CallToolRequest, in replaceBlueprintInput) (*sdk.CallToolResult, any, error) {
	return s.bridge(http.MethodPut, "/api/blueprint", rawOrNull(in.Blueprint))
}

type addParameterInput struct {
	Name      string          `json:"name"`
	Parameter json.RawMessage `json:"parameter"` // raw: see the file comment
}

// addParameter mirrors POST /api/blueprint/parameters.
func (s *server) addParameter(_ context.Context, _ *sdk.CallToolRequest, in addParameterInput) (*sdk.CallToolResult, any, error) {
	body, err := json.Marshal(struct {
		Name      string          `json:"name"`
		Parameter json.RawMessage `json:"parameter"`
	}{Name: in.Name, Parameter: rawOrNull(in.Parameter)})
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}
	return s.bridge(http.MethodPost, "/api/blueprint/parameters", body)
}

type updateParameterInput struct {
	Name      string          `json:"name"`
	Parameter json.RawMessage `json:"parameter"` // raw: see the file comment
}

// updateParameter mirrors PUT /api/blueprint/parameters/{name}.
func (s *server) updateParameter(_ context.Context, _ *sdk.CallToolRequest, in updateParameterInput) (*sdk.CallToolResult, any, error) {
	body, err := json.Marshal(struct {
		Parameter json.RawMessage `json:"parameter"`
	}{Parameter: rawOrNull(in.Parameter)})
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}
	return s.bridge(http.MethodPut, "/api/blueprint/parameters/"+url.PathEscape(in.Name), body)
}

type renameParameterInput struct {
	Name string `json:"name" jsonschema:"The declared parameter to rename."`
	To   string `json:"to" jsonschema:"The new name (camelCase, not already declared)."`
}

// renameParameter mirrors POST /api/blueprint/parameters/{name}/rename.
func (s *server) renameParameter(_ context.Context, _ *sdk.CallToolRequest, in renameParameterInput) (*sdk.CallToolResult, any, error) {
	body, err := json.Marshal(struct {
		To string `json:"to"`
	}{To: in.To})
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}
	return s.bridge(http.MethodPost, "/api/blueprint/parameters/"+url.PathEscape(in.Name)+"/rename", body)
}

type deleteParameterInput struct {
	Name string `json:"name" jsonschema:"The declared parameter to delete."`
}

// deleteParameter mirrors DELETE /api/blueprint/parameters/{name}.
func (s *server) deleteParameter(_ context.Context, _ *sdk.CallToolRequest, in deleteParameterInput) (*sdk.CallToolResult, any, error) {
	return s.bridge(http.MethodDelete, "/api/blueprint/parameters/"+url.PathEscape(in.Name), nil)
}

// --- providers ---

type addProviderInput struct {
	Ref string `json:"ref" jsonschema:"The provider's xpkg reference, e.g. ghcr.io/org/provider-name:v1.2.3 or ...@sha256:<digest>."`
}

// addProvider mirrors POST /api/providers.
func (s *server) addProvider(_ context.Context, _ *sdk.CallToolRequest, in addProviderInput) (*sdk.CallToolResult, any, error) {
	body, err := json.Marshal(struct {
		Ref string `json:"ref"`
	}{Ref: in.Ref})
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}
	return s.bridge(http.MethodPost, "/api/providers", body)
}

// listProviders mirrors GET /api/providers.
func (s *server) listProviders(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, any, error) {
	return s.bridge(http.MethodGet, "/api/providers", nil)
}

// --- generate / render ---

type generateInput struct {
	Write bool `json:"write,omitempty" jsonschema:"true writes the rendered files into the --out workspace; false (the default) is a dry-run preview that touches nothing."`
}

// generateOutput mirrors the HTTP response's per-file summary
// (internal/api/generate.go's generateOutput, unexported there): path, size
// and the file's full content.
type generateOutput struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Body  string `json:"body"`
}

// generate mirrors POST /api/generate — with the write half re-homed behind
// the workspace gate. The bridge call is ALWAYS a {"write":false} dry run:
// rendering stays internal/api's (and thereby emit.Generate's, the sole
// generation entry point), while the writes happen here, after every
// returned path has passed the workspace check. Sending write:true through
// the bridge instead would have the HTTP handler write before this gate
// could look at a single path.
//
// The write loop is the same MkdirAll+WriteFile sequence cmd/cf/gen.go's
// run and internal/api's handleGenerate use, applied to the exact bodies
// the dry run returned — so a generation written by an MCP call leaves the
// identical tree a CLI run or a canvas write would have.
func (s *server) generate(_ context.Context, _ *sdk.CallToolRequest, in generateInput) (*sdk.CallToolResult, any, error) {
	status, body, err := s.call(http.MethodPost, "/api/generate", []byte(`{"write":false}`))
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return result(status, body)
	}
	if !in.Write {
		return textResult(body), nil, nil
	}

	var resp struct {
		Outputs []generateOutput `json:"outputs"`
		Written bool             `json:"written"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("decode generate response: %w", err)
	}

	if err := s.writeOutputs(resp.Outputs); err != nil {
		return nil, nil, err
	}

	resp.Written = true
	written, err := json.Marshal(resp)
	if err != nil {
		return nil, nil, fmt.Errorf("encode generate response: %w", err)
	}
	return textResult(written), nil, nil
}

// writeOutputs is the ONLY place this package writes generated files, and
// every path passes the workspace gate before any file is touched — so a
// refused path means zero files changed, not a partially-written tree. The
// write itself is the same MkdirAll+WriteFile sequence cmd/cf/gen.go uses.
func (s *server) writeOutputs(outputs []generateOutput) error {
	for _, out := range outputs {
		if err := s.ws.check(out.Path); err != nil {
			return err
		}
	}
	for _, out := range outputs {
		if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out.Path, []byte(out.Body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// renderCheck mirrors POST /api/render.
func (s *server) renderCheck(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, any, error) {
	return s.bridge(http.MethodPost, "/api/render", nil)
}

// --- small helpers ---

// withQuery appends q to path when q is non-empty. Encode escapes every
// value and emits keys sorted, so bridged URLs are deterministic.
func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// rawOrNull normalizes an absent raw field to the JSON null literal, so the
// marshaled request body is always valid JSON. The tools' schemas mark these
// fields required, so this only matters if a client bypasses schema
// validation — in which case the HTTP layer sees {"...":null} and answers
// with its own error, exactly as it would a browser's.
func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
