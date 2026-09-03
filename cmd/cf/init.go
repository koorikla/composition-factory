package main

import (
	"fmt"
	"io"
	"os"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// InitCmd scaffolds a minimal valid blueprint file.
type InitCmd struct {
	Path string `arg:"" help:"Path to the blueprint file to create." default:"blueprint.cf.yaml" optional:""`
}

const scaffoldBlueprint = `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: my-app
spec:
  xrd:
    group: platform.example.org
    version: v1alpha1
    kind: XApp
    plural: xapps
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
        description: Name of the ProviderConfig to use.
`

func (c *InitCmd) Run(out io.Writer) error {
	if _, err := os.Stat(c.Path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", c.Path)
	}

	b, err := blueprint.Parse([]byte(scaffoldBlueprint))
	if err != nil {
		return fmt.Errorf("scaffold blueprint parse error: %w", err)
	}
	if err := b.Validate(); err != nil {
		return fmt.Errorf("scaffold blueprint validation error: %w", err)
	}

	if err := os.WriteFile(c.Path, []byte(scaffoldBlueprint), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(out, "scaffolded %s\n", c.Path)
	fmt.Fprintf(out, "next: cf kinds, cf provider add, or cf gen %s\n", c.Path)
	return nil
}
