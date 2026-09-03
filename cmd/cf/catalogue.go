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
	Kind string `help:"Filter by specific served CRD kind (e.g. DatabaseInstance, Bucket, Topic)." default:""`
}

func (c *CatalogueCmd) Run(out io.Writer) error {
	entries, err := catalogue.Load()
	if err != nil {
		return err
	}

	var results []catalogue.Provider
	if c.Kind != "" {
		pkgs := catalogue.PackagesForKind(c.Kind)
		if len(pkgs) == 0 {
			fmt.Fprintf(out, "No packages found serving kind %q.\n", c.Kind)
			return nil
		}
		pkgSet := make(map[string]bool, len(pkgs))
		for _, p := range pkgs {
			pkgSet[p] = true
		}
		var kindFiltered []catalogue.Provider
		for _, e := range entries {
			if pkgSet[e.Name] {
				kindFiltered = append(kindFiltered, e)
			}
		}
		results = catalogue.Search(kindFiltered, c.Q, c.Type)
	} else {
		results = catalogue.Search(entries, c.Q, c.Type)
	}

	if len(results) == 0 {
		if c.Q != "" {
			fmt.Fprintf(out, "No packages found matching %q.\n", c.Q)
		} else {
			fmt.Fprintln(out, "No packages found in catalogue.")
		}
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tLATEST REF\tSERVED KINDS\tDESCRIPTION")
	for _, p := range results {
		ref := p.Ref
		if ref == "" {
			ref = "(no stable release)"
		}
		desc := strings.ReplaceAll(p.Description, "\n", " ")
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		kinds := catalogue.Kinds(p.Name)
		kindsStr := "-"
		if len(kinds) > 0 {
			if len(kinds) > 3 {
				kindsStr = strings.Join(kinds[:3], ", ") + fmt.Sprintf(" (+%d)", len(kinds)-3)
			} else {
				kindsStr = strings.Join(kinds, ", ")
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, ref, kindsStr, desc)
	}
	return w.Flush()
}
