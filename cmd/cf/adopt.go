package main

import (
	"fmt"
	"io"
	"os"

	"github.com/koorikla/compositionfactory/internal/adopt"
)

// AdoptCmd imports an existing Crossplane Composition (and optional XRD) into a blueprint.
type AdoptCmd struct {
	Composition string `arg:"" help:"Path to the Composition YAML file (or - for stdin)."`
	Out         string `short:"o" help:"Output file to write the blueprint to (defaults to stdout)."`
	Provider    string `help:"Default provider package reference when not inferrable from CRDs."`
	CacheDir    string `help:"Schema cache directory." default:"${cachedir}"`
}

func (c *AdoptCmd) Run(out io.Writer) error {
	code, err := c.run(out)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func (c *AdoptCmd) run(out io.Writer) (int, error) {
	var data []byte
	var err error

	if c.Composition == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(c.Composition)
	}
	if err != nil {
		return 1, fmt.Errorf("read composition: %w", err)
	}

	bp, report, err := adopt.Adopt(data, adopt.Options{
		DefaultProviderRef: c.Provider,
		CacheDir:           c.CacheDir,
	})
	if err != nil {
		return 1, fmt.Errorf("adopt composition: %w", err)
	}

	outBytes, err := adopt.FormatAdoptedYAML(bp, report)
	if err != nil {
		return 1, fmt.Errorf("marshal blueprint: %w", err)
	}

	if c.Out != "" {
		if err := os.WriteFile(c.Out, outBytes, 0644); err != nil {
			return 1, fmt.Errorf("write blueprint: %w", err)
		}
		if report != nil && report.IsLossy() {
			fmt.Fprint(out, report.String())
		}
		fmt.Fprintf(out, "Adopted blueprint written to %s\n", c.Out)
	} else {
		if _, err := out.Write(outBytes); err != nil {
			return 1, err
		}
	}

	if report != nil && report.IsLossy() {
		return 2, nil
	}
	return 0, nil
}
