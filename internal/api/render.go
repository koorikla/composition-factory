// This file implements the /api/render route: a real `crossplane
// composition render` of the current blueprint against a synthesized sample
// XR, reporting whether the generated artifacts actually render.
//
// The same single-rendering-path rule as generate.go applies: the artifacts
// come from emit.Generate, byte-identical to what `cf gen` and POST
// /api/generate produce — this file never assembles a byte of XRD,
// Composition or functions.yaml itself. What it adds is only the harness the
// render command needs around those artifacts: a temp dir holding them as
// files, a sample XR synthesized from the blueprint's own XRD declaration,
// and the exec of the CLI — the exact invocation the repo's acceptance test
// uses (`crossplane composition render <xr> <comp> <functions> --xrd <xrd>
// --timeout 5m`).
//
// Outcome model: the render's result — success, composition-level failure,
// or "the environment cannot run the check at all" — is the endpoint's
// PAYLOAD, always a 200 with every key present. HTTP error codes are
// reserved for the request itself failing the way every other route fails:
// 400 when the blueprint does not validate (or its provider schemas cannot
// load), 500 when the server's own environment breaks (unreadable blueprint
// path, temp dir I/O). Never a fake ok: a missing crossplane binary or an
// unreachable Docker daemon is reported in `unavailable`, distinctly from a
// blueprint whose composition genuinely fails to render (`error`).
package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"sigs.k8s.io/yaml"
)

// renderTimeout bounds the whole render exec. The command itself is given
// --timeout 5m (mirroring the acceptance test's invocation, where a cold
// run really can pull function images for minutes), but an interactive
// canvas request cannot usefully wait that long, so the context cancels the
// process at 60s — the shorter bound wins.
const renderTimeout = 60 * time.Second

// composedResourceAnnotation is stamped by `crossplane composition render`
// on every composed resource it prints; the XR document never carries it.
// Counting annotated documents is therefore the reliable way to count
// composed resources, immune to however the XR's own kind/name might
// coincide with a composed resource's.
const composedResourceAnnotation = "crossplane.io/composition-resource-name"

// renderResponse is POST /api/render's body. No key is ever omitted: a
// client branches on ok, then reads error (the render's own failure output,
// verbatim) or unavailable (why the check could not run at all) — the two
// are mutually exclusive and an empty string means "not this one".
type renderResponse struct {
	OK          bool   `json:"ok"`
	Resources   int    `json:"resources"`
	Error       string `json:"error"`
	Unavailable string `json:"unavailable"`
}

// handleRender serves POST /api/render. Sequence: availability check
// (cheapest first — no artifact is generated for a machine that cannot
// render it), then load+validate the blueprint and emit its artifacts into
// a fresh temp dir under srv.mu (the same doc-consistency the other
// handlers get; the exec itself runs unlocked so a 60s render never blocks
// blueprint edits), then synthesize the sample XR and run the CLI.
func (srv *server) handleRender(w http.ResponseWriter, r *http.Request) {
	lookPath := srv.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("crossplane"); err != nil {
		writeJSON(w, http.StatusOK, renderResponse{
			Unavailable: fmt.Sprintf("crossplane CLI not found on PATH: %v", err),
		})
		return
	}

	dir, err := os.MkdirTemp("", "cf-render-")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(dir)

	b, outputs, ok := srv.renderInputs(w, dir)
	if !ok {
		return
	}

	// Write the generated artifacts into the temp dir and classify each by
	// the directory emit.Generate placed it in (compositions/, xrds/, and
	// functions.yaml at the root) rather than re-deriving file names here.
	var compPath, fnsPath, xrdPath string
	for _, out := range outputs {
		if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.WriteFile(out.Path, out.Body, 0o644); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		switch filepath.Base(filepath.Dir(out.Path)) {
		case "compositions":
			compPath = out.Path
		case "xrds":
			xrdPath = out.Path
		default:
			fnsPath = out.Path
		}
	}

	xr, err := sampleXR(b)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	xrPath := filepath.Join(dir, "xr.yaml")
	if err := os.WriteFile(xrPath, xr, 0o644); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	render := srv.render
	if render == nil {
		render = runCrossplaneRender
	}
	ctx, cancel := context.WithTimeout(r.Context(), renderTimeout)
	defer cancel()

	out, err := render(ctx, xrPath, compPath, fnsPath, xrdPath)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if dockerUnavailable(msg) {
			// The render failed because the function runtime's Docker daemon
			// is unreachable — the environment cannot run the check, which
			// says nothing about whether the blueprint renders.
			writeJSON(w, http.StatusOK, renderResponse{Unavailable: msg})
			return
		}
		writeJSON(w, http.StatusOK, renderResponse{Error: msg})
		return
	}

	n, err := countComposedResources(out)
	if err != nil {
		// The command exited 0 but printed something this server cannot
		// parse — report it as a failed check rather than guessing a count.
		writeJSON(w, http.StatusOK, renderResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, renderResponse{OK: true, Resources: n})
}

// renderInputs loads and validates the blueprint and emits its artifacts
// into dir, all under srv.mu so the render describes a document that
// actually existed as a whole (the same snapshot discipline handleGenerate
// applies). On failure it writes the response itself — the identical
// 400-validation / 500-I/O split generate.go uses — and returns ok=false.
func (srv *server) renderInputs(w http.ResponseWriter, dir string) (*blueprint.Blueprint, []emit.Output, bool) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return nil, nil, false
	}
	crds, err := srv.loadSourceCRDs(b)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	outputs, err := emit.Generate(b, crds, dir)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	return b, outputs, true
}

// runCrossplaneRender is the real render seam default: the exact invocation
// the repo's acceptance test uses, run under the caller's (60s) context.
func runCrossplaneRender(ctx context.Context, xr, comp, fns, xrd string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "crossplane", "composition", "render",
		xr, comp, fns, "--xrd", xrd, "--timeout", "5m")
	return cmd.CombinedOutput()
}

// sampleXR synthesizes the composite resource the render is checked
// against, from the blueprint's own XRD declaration: the XRD's
// group/version/kind, a fixed name, a namespace only when the XRD is
// Namespaced, and a type-appropriate placeholder for every REQUIRED
// parameter. Non-required parameters are deliberately omitted so the render
// exercises the composition's default-injection path for them — exactly
// what a minimal real XR would do.
func sampleXR(b *blueprint.Blueprint) ([]byte, error) {
	spec := map[string]any{}
	for name, p := range b.Spec.XRD.Parameters {
		if !p.Required {
			continue
		}
		spec[name] = placeholderValue(p)
	}
	metadata := map[string]any{"name": "render-check"}
	if b.Spec.XRD.Scope == "Namespaced" {
		metadata["namespace"] = "default"
	}
	// sigs.k8s.io/yaml sorts map keys, so this marshal is deterministic.
	return yaml.Marshal(map[string]any{
		"apiVersion": b.Spec.XRD.Group + "/" + b.Spec.XRD.Version,
		"kind":       b.Spec.XRD.Kind,
		"metadata":   metadata,
		"spec":       spec,
	})
}

// placeholderValue picks a value that satisfies p's declared type. An enum
// wins over the type: its first value is, by the XRD's own schema, always
// legal, where a generic placeholder might not be.
func placeholderValue(p blueprint.Parameter) any {
	if len(p.Enum) > 0 {
		return p.Enum[0]
	}
	switch p.Type {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "object":
		return map[string]any{}
	default: // string
		return "sample"
	}
}

// dockerUnavailable reports whether a failed render's output indicates the
// Docker daemon is unreachable, rather than the composition failing to
// render. The markers cover the daemon's own connectivity messages ("Cannot
// connect to the Docker daemon at unix:///var/run/docker.sock. Is the
// docker daemon running?" — captured verbatim from crossplane v2.5.0 with
// the daemon stopped) and Docker Desktop's "error during connect" variant.
func dockerUnavailable(output string) bool {
	s := strings.ToLower(output)
	for _, marker := range []string{"docker daemon", "docker.sock", "error during connect"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// countComposedResources counts the composed resources in a successful
// render's YAML stream: every document carrying the render command's
// composition-resource-name annotation, which the XR document never does.
func countComposedResources(stream []byte) (int, error) {
	n := 0
	for i, doc := range splitYAMLStream(stream) {
		var d struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal(doc, &d); err != nil {
			return 0, fmt.Errorf("render output document %d does not parse as YAML: %v", i+1, err)
		}
		if d.Metadata.Annotations[composedResourceAnnotation] != "" {
			n++
		}
	}
	return n, nil
}

// splitYAMLStream splits a multi-document YAML stream on "---" at column
// zero and drops empty documents — a small deliberate duplicate of
// internal/xpkg's unexported splitYAML (the same precedent as blueprint.go's
// referencingResources: the original is private to its package, and this
// three-line scan is not worth an export).
func splitYAMLStream(in []byte) [][]byte {
	var out [][]byte
	for _, part := range strings.Split(string(in), "\n---") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(part, "---"))
		if len(trimmed) == 0 {
			continue
		}
		out = append(out, []byte(trimmed))
	}
	return out
}
