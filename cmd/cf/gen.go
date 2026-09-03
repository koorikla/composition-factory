package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// GenCmd renders a blueprint to YAML on disk.
type GenCmd struct {
	Blueprint string `arg:"" help:"Path to the blueprint file."`
	Out       string `short:"o" help:"Output directory." default:"."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
	Check     bool   `help:"Do not write. Exit 0 if in sync, 2 if the tree has drifted."`
	Validate  bool   `help:"Validate rendered output against CRD schemas."`

	// filesystem ships the go-template body as a templates/ folder (one
	// object per file), ConfigMaps under the ~1MiB cap, and a
	// DeploymentRuntimeConfig mounting them into the function pod. The
	// rendered documents are byte-identical to the inline form: the
	// function concatenates the folder exactly back into the inline body.
	TemplateSource string `help:"Where the Composition's go-template body lives: inline (default) or filesystem (templates/ folder + ConfigMaps + DeploymentRuntimeConfig)." enum:"inline,filesystem" default:"inline"`
	// Engine rendering engine: go-templating, kcl, or python.
	Engine string `help:"Composition rendering engine: go-templating, kcl, or python (defaults to blueprint setting)."`
	// GroupSuffix appends a workspace isolation suffix to the XRD group.
	GroupSuffix string `help:"Suffix to append to the XRD group (e.g. .cf-slug for workspace isolation in a shared cluster)."`
}

func (c *GenCmd) Run(out io.Writer) error {
	code, err := c.run(out)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// run returns the intended exit code so tests can assert it without exiting.
// 0 = in sync or written, 1 = tool error (returned as err), 2 = drift.
func (c *GenCmd) run(out io.Writer) (int, error) {
	b, err := blueprint.Load(c.Blueprint)
	if err != nil {
		return 1, err
	}
	store := cache.New(c.CacheDir)
	crds, err := cache.LoadSources(store, b, filepath.Dir(c.Blueprint))
	if err != nil {
		return 1, err
	}
	// Native Kubernetes kinds are always available: vendored into the
	// binary, pinned to one Kubernetes version, never fetched or cached —
	// so they join the schema set unconditionally rather than via a source.
	native, err := k8s.Kinds()
	if err != nil {
		return 1, err
	}
	crds = append(crds, native...)
	if c.TemplateSource == "filesystem" {
		if b.Spec.Emit == nil {
			b.Spec.Emit = &blueprint.Emit{}
		}
		b.Spec.Emit.TemplateSource = blueprint.TemplateSourceFileSystem
	}
	if c.Engine != "" {
		if c.Engine != blueprint.EngineGoTemplating && c.Engine != blueprint.EngineKCL && c.Engine != blueprint.EnginePython {
			return 1, fmt.Errorf("--engine must be %q, %q, or %q, got %q", blueprint.EngineGoTemplating, blueprint.EngineKCL, blueprint.EnginePython, c.Engine)
		}
		if b.Spec.Emit == nil {
			b.Spec.Emit = &blueprint.Emit{}
		}
		b.Spec.Emit.Engine = c.Engine
	}
	if c.GroupSuffix != "" {
		suffix := c.GroupSuffix
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		b.Spec.XRD.Group = b.Spec.XRD.Group + suffix
	}
	outputs, err := emit.Generate(b, crds, c.Out)
	if err != nil {
		return 1, err
	}

	if c.Validate {
		if _, err := exec.LookPath("crossplane"); err != nil {
			return 1, fmt.Errorf("crossplane CLI not found on PATH: %w", err)
		}
		tempDir, err := os.MkdirTemp("", "cf-gen-validate-")
		if err != nil {
			return 1, err
		}
		defer os.RemoveAll(tempDir)

		tempOutputs, err := emit.Generate(b, crds, tempDir)
		if err != nil {
			return 1, err
		}

		var compPath, fnsPath, xrdPath string
		for _, o := range tempOutputs {
			if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
				return 1, err
			}
			if err := os.WriteFile(o.Path, o.Body, 0o644); err != nil {
				return 1, err
			}
			switch {
			case filepath.Base(filepath.Dir(o.Path)) == "compositions":
				compPath = o.Path
			case filepath.Base(filepath.Dir(o.Path)) == "xrds":
				xrdPath = o.Path
			case filepath.Base(o.Path) == "functions.yaml":
				fnsPath = o.Path
			}
		}

		sampleXRBytes, err := emit.SampleXR(b)
		if err != nil {
			return 1, err
		}
		xrPath := filepath.Join(tempDir, "xr.yaml")
		if err := os.WriteFile(xrPath, sampleXRBytes, 0o644); err != nil {
			return 1, err
		}

		cmd := exec.Command("crossplane", "composition", "render", xrPath, compPath, fnsPath, "--xrd", xrdPath, "--timeout", "5m")
		renderOut, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(renderOut))
			if msg == "" {
				msg = err.Error()
			}
			return 1, fmt.Errorf("render failed: %s", msg)
		}

		if err := emit.ValidateRendered(renderOut, crds); err != nil {
			return 1, fmt.Errorf("render validation failed:\n%w", err)
		}
		fmt.Fprintln(out, "render validation ok")
	}

	if c.Check {
		drift := false
		expected := make(map[string]bool, len(outputs))
		for _, o := range outputs {
			cleanPath := filepath.Clean(o.Path)
			expected[cleanPath] = true
			existing, err := os.ReadFile(o.Path)
			if err != nil || !bytes.Equal(existing, o.Body) {
				fmt.Fprintf(out, "drift: %s\n", o.Path)
				drift = true
			}
		}
		existingFiles, _ := c.findExistingManagedFiles()
		for _, path := range existingFiles {
			if !expected[path] {
				fmt.Fprintf(out, "drift: %s\n", path)
				drift = true
			}
		}
		if drift {
			fmt.Fprintln(out, "generated output is stale; run: cf gen")
			return 2, nil
		}
		fmt.Fprintln(out, "in sync")
		return 0, nil
	}

	// Writes are not atomic across outputs: a failure partway through this
	// loop can leave a partially-written tree. Accepted for M1 — output is
	// fully regenerable from the blueprint, and the next --check reports
	// exactly this as drift rather than silently trusting a stale file.
	expected := make(map[string]bool, len(outputs))
	for _, o := range outputs {
		expected[filepath.Clean(o.Path)] = true
		if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(o.Path, o.Body, 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(out, "wrote %s\n", o.Path)
	}

	// Clean up orphaned/stale files in managed locations
	existingFiles, _ := c.findExistingManagedFiles()
	for _, path := range existingFiles {
		if !expected[path] {
			if err := os.Remove(path); err == nil {
				fmt.Fprintf(out, "removed %s\n", path)
				// Prune empty parent directory if inside managed scope
				dir := filepath.Dir(path)
				for dir != "." && dir != c.Out && dir != "/" {
					if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
						_ = os.Remove(dir)
						dir = filepath.Dir(dir)
					} else {
						break
					}
				}
			}
		}
	}

	for _, o := range outputs {
		if filepath.Base(o.Path) == "rbac.yaml" {
			fmt.Fprintf(out, "warning: composed native Kubernetes kinds require cluster RBAC permissions; apply %s to your cluster\n", o.Path)
		}
	}
	return 0, nil
}

func (c *GenCmd) findExistingManagedFiles() ([]string, error) {
	cleanOut := filepath.Clean(c.Out)
	var found []string

	if cleanOut == "." {
		managedDirs := []string{"compositions", "xrds", "providerconfigs", "runtime", "templates"}
		for _, d := range managedDirs {
			if _, err := os.Stat(d); err == nil {
				_ = filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return nil
					}
					found = append(found, filepath.Clean(path))
					return nil
				})
			}
		}
		topFiles := []string{"functions.yaml", "rbac.yaml"}
		for _, f := range topFiles {
			if _, err := os.Stat(f); err == nil {
				found = append(found, filepath.Clean(f))
			}
		}
	} else {
		if _, err := os.Stat(cleanOut); err == nil {
			_ = filepath.Walk(cleanOut, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				found = append(found, filepath.Clean(path))
				return nil
			})
		}
	}
	return found, nil
}
