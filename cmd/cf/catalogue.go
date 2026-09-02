package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/koorikla/compositionfactory/catalogue"
)

type CatalogueCmd struct {
	Q    string `arg:"" optional:"" help:"Search query for provider name, description, or served kinds."`
	Type string `help:"Filter by package type: provider or function." default:""`
}

func (c *CatalogueCmd) Run(out io.Writer) error {
	entries, err := catalogue.Load()
	if err != nil {
		return err
	}

	results := catalogue.Search(entries, c.Q, c.Type)
	if len(results) == 0 {
		if c.Q != "" {
			fmt.Fprintf(out, "No packages found matching %q.\n", c.Q)
		} else {
			fmt.Fprintln(out, "No packages found in catalogue.")
		}
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tLATEST REF\tDESCRIPTION")
	for _, p := range results {
		ref := p.Ref
		if ref == "" {
			ref = "(no stable release)"
		}
		desc := strings.ReplaceAll(p.Description, "\n", " ")
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, ref, desc)
	}
	return w.Flush()
}
