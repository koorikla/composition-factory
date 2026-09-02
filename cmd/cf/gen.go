package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	// filesystem ships the go-template body as a templates/ folder (one
	// object per file), ConfigMaps under the ~1MiB cap, and a
	// DeploymentRuntimeConfig mounting them into the function pod. The
	// rendered documents are byte-identical to the inline form: the
	// function concatenates the folder exactly back into the inline body.
	TemplateSource string `help:"Where the Composition's go-template body lives: inline (default) or filesystem (templates/ folder + ConfigMaps + DeploymentRuntimeConfig)." enum:"inline,filesystem" default:"inline"`
	Engine         string `help:"Composition rendering engine: go-templating (default) or kcl." enum:"go-templating,kcl" default:""`
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
		if b.Spec.Emit == nil {
			b.Spec.Emit = &blueprint.Emit{}
		}
		b.Spec.Emit.Engine = c.Engine
	}
	outputs, err := emit.Generate(b, crds, c.Out)
	if err != nil {
		return 1, err
	}

	if c.Check {
		drift := false
		for _, o := range outputs {
			existing, err := os.ReadFile(o.Path)
			if err != nil || !bytes.Equal(existing, o.Body) {
				fmt.Fprintf(out, "drift: %s\n", o.Path)
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
	for _, o := range outputs {
		if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(o.Path, o.Body, 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(out, "wrote %s\n", o.Path)
	}
	return 0, nil
}
