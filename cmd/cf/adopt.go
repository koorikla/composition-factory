package main

import (
	"fmt"
	"io"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/internal/adopt"
)

// AdoptCmd imports an existing Crossplane Composition (and optional XRD) into a blueprint.
type AdoptCmd struct {
	Composition string `arg:"" help:"Path to the Composition YAML file (or - for stdin)."`
	Out         string `short:"o" help:"Output file to write the blueprint to (defaults to stdout)."`
	Provider    string `help:"Default provider package reference when not inferrable from CRDs."`
}

func (c *AdoptCmd) Run(out io.Writer) error {
	var data []byte
	var err error

	if c.Composition == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(c.Composition)
	}
	if err != nil {
		return fmt.Errorf("read composition: %w", err)
	}

	bp, err := adopt.Adopt(data, adopt.Options{
		DefaultProviderRef: c.Provider,
	})
	if err != nil {
		return fmt.Errorf("adopt composition: %w", err)
	}

	outBytes, err := yaml.Marshal(bp)
	if err != nil {
		return fmt.Errorf("marshal blueprint: %w", err)
	}

	if c.Out != "" {
		if err := os.WriteFile(c.Out, outBytes, 0644); err != nil {
			return fmt.Errorf("write blueprint: %w", err)
		}
		fmt.Fprintf(out, "Adopted blueprint written to %s\n", c.Out)
		return nil
	}

	_, err = out.Write(outBytes)
	return err
}
