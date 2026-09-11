package emit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// DefaultRenderTimeout bounds the render subprocess execution.
// The crossplane command itself is passed --timeout 5m, but the context
// bounds it to 60s by default so interactive or automated checks do not hang.
const DefaultRenderTimeout = 60 * time.Second

// composedResourceAnnotation is stamped by `crossplane composition render`
// on every composed resource it prints; the XR document never carries it.
const composedResourceAnnotation = "crossplane.io/composition-resource-name"

// RenderRunner executes the crossplane render command given file paths.
type RenderRunner func(ctx context.Context, xr, comp, fns, xrd string) ([]byte, error)

// RenderOptions configures the execution of RenderCheck.
type RenderOptions struct {
	Dir      string
	LookPath func(file string) (string, error)
	Runner   RenderRunner
	Timeout  time.Duration
}

// RenderResult represents the outcome of a Crossplane composition render check.
// OK is true when the composition renders cleanly and validates against CRD schemas.
// Error holds composition-level errors or schema validation errors.
// Unavailable holds environment-level errors (missing binary, unreachable Docker daemon).
type RenderResult struct {
	OK               bool   `json:"ok"`
	Resources        int    `json:"resources"`
	Error            string `json:"error"`
	Unavailable      string `json:"unavailable"`
	Container        bool   `json:"container,omitempty"`
	ValidationFailed bool   `json:"-"`
}

// GenerateError records a failure during manifest generation in the render pipeline.
type GenerateError struct {
	Err error
}

func (e *GenerateError) Error() string {
	return e.Err.Error()
}

func (e *GenerateError) Unwrap() error {
	return e.Err
}

// DefaultRenderRunner is the standard execution of `crossplane composition render`.
func DefaultRenderRunner(ctx context.Context, xr, comp, fns, xrd string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "crossplane", "composition", "render",
		xr, comp, fns, "--xrd", xrd, "--timeout", "5m")
	return cmd.CombinedOutput()
}

// IsDockerUnavailable reports whether a failed render's output indicates the
// Docker daemon is unreachable or in an invalid runtime state.
func IsDockerUnavailable(output string) bool {
	s := strings.ToLower(output)
	for _, marker := range []string{
		"docker daemon",
		"docker.sock",
		"error during connect",
		"runtime-docker-network",
		"is not connected to docker network",
		"is marked for removal",
		"marked for removal",
		"is already in progress",
		"daemon is not running",
		"cannot connect to the docker daemon",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// CountComposedResources counts the composed resources in a YAML stream by checking
// for the crossplane.io/composition-resource-name annotation.
func CountComposedResources(stream []byte) (int, error) {
	n := 0
	for i, doc := range blueprint.SplitDocs(stream) {
		var d struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal(doc, &d); err != nil {
			return 0, fmt.Errorf("render output document %d does not parse as YAML: %w", i+1, err)
		}
		if d.Metadata.Annotations != nil && d.Metadata.Annotations[composedResourceAnnotation] != "" {
			n++
		}
	}
	return n, nil
}

// RenderCheck runs the full Crossplane composition render pipeline:
// 1. Checks availability of the crossplane CLI.
// 2. Creates a temporary directory (if not provided in opts) and emits manifests via Generate.
// 3. Synthesizes a sample XR via SampleXR.
// 4. Executes `crossplane composition render` under context timeout.
// 5. Classifies errors (Docker daemon unavailability vs composition errors).
// 6. Counts composed resources.
// 7. Validates rendered composed resources against cached CRD schemas via ValidateRenderedWithBlueprint.
func RenderCheck(ctx context.Context, b *blueprint.Blueprint, crds []schema.CRD, opts RenderOptions) (RenderResult, error) {
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("crossplane"); err != nil {
		return RenderResult{
			Unavailable: fmt.Sprintf("crossplane CLI not found on PATH: %v", err),
		}, nil
	}

	dir := opts.Dir
	if dir == "" {
		tempDir, err := os.MkdirTemp("", "cf-render-")
		if err != nil {
			return RenderResult{}, err
		}
		defer os.RemoveAll(tempDir)
		dir = tempDir
	}

	outputs, err := Generate(b, crds, dir)
	if err != nil {
		return RenderResult{}, &GenerateError{Err: err}
	}

	var compPath, fnsPath, xrdPath string
	for _, out := range outputs {
		if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
			return RenderResult{}, err
		}
		if err := os.WriteFile(out.Path, out.Body, 0o644); err != nil {
			return RenderResult{}, err
		}
		switch {
		case filepath.Base(filepath.Dir(out.Path)) == "compositions":
			compPath = out.Path
		case filepath.Base(filepath.Dir(out.Path)) == "xrds":
			xrdPath = out.Path
		case filepath.Base(out.Path) == "functions.yaml":
			fnsPath = out.Path
		}
	}

	xr, err := SampleXR(b)
	if err != nil {
		return RenderResult{}, err
	}
	xrPath := filepath.Join(dir, "xr.yaml")
	if err := os.WriteFile(xrPath, xr, 0o644); err != nil {
		return RenderResult{}, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultRenderTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := opts.Runner
	if runner == nil {
		runner = DefaultRenderRunner
	}

	out, err := runner(ctx, xrPath, compPath, fnsPath, xrdPath)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if IsDockerUnavailable(msg) {
			return RenderResult{Unavailable: msg}, nil
		}
		return RenderResult{Error: msg}, nil
	}

	n, err := CountComposedResources(out)
	if err != nil {
		return RenderResult{Error: err.Error()}, nil
	}

	if err := ValidateRenderedWithBlueprint(out, crds, b); err != nil {
		return RenderResult{Error: err.Error(), ValidationFailed: true}, nil
	}

	return RenderResult{OK: true, Resources: n}, nil
}
