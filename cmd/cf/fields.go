package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
)

type FieldsCmd struct {
	Kind      string `arg:"" help:"Kind name or apiVersion/Kind (e.g. Queue, sqs.aws.m.upbound.io/v1beta1/Queue)."`
	Required  bool   `help:"Print only required fields."`
	Status    bool   `help:"Print status output fields instead of spec fields."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
	Blueprint string `help:"Path to blueprint file to include declared sources." default:"doc.cf.yaml"`
}

func (c *FieldsCmd) Run(out io.Writer) error {
	store := cache.New(c.CacheDir)
	var refs []string
	var b *blueprint.Blueprint
	var blueprintDir string

	if _, err := os.Stat(c.Blueprint); err == nil {
		if loaded, err := blueprint.Load(c.Blueprint); err == nil {
			b = loaded
			blueprintDir = filepath.Dir(c.Blueprint)
			for _, s := range b.Spec.Sources {
				if s.Provider != "" {
					refs = append(refs, s.Provider)
				}
			}
		}
	}

	if len(refs) == 0 {
		cached, _ := store.List()
		refs = cached
	}

	idx, err := api.BuildIndex(store, refs, b, blueprintDir)
	if err != nil {
		return err
	}

	// Resolve the target kind
	var targetKind index.Kind
	var targetCRD schema.CRD
	var found bool

	// Check if arg has format apiVersion/kind
	if strings.Contains(c.Kind, "/") {
		lastSlash := strings.LastIndex(c.Kind, "/")
		apiVersion := c.Kind[:lastSlash]
		kindName := c.Kind[lastSlash+1:]
		if k, crd, ok := idx.LookupKind(apiVersion, kindName); ok {
			targetKind, targetCRD, found = k, crd, true
		}
	}

	if !found {
		// Search by kind name (case-insensitive)
		var candidates []index.Kind
		for _, k := range idx.All() {
			if strings.EqualFold(k.Kind, c.Kind) {
				candidates = append(candidates, k)
			}
		}
		if len(candidates) == 0 {
			// Try substring search
			for _, k := range idx.All() {
				if strings.Contains(strings.ToLower(k.Kind), strings.ToLower(c.Kind)) {
					candidates = append(candidates, k)
				}
			}
		}

		if len(candidates) == 1 {
			targetKind = candidates[0]
			if crd, ok := idx.Lookup(targetKind.APIVersion, targetKind.Kind); ok {
				targetCRD, found = crd, true
			}
		} else if len(candidates) > 1 {
			// If multiple candidates, prefer namespaced (.m.) variant or first exact match
			selected := candidates[0]
			for _, cand := range candidates {
				if strings.EqualFold(cand.Kind, c.Kind) && cand.Namespaced {
					selected = cand
					break
				}
			}
			targetKind = selected
			if crd, ok := idx.Lookup(targetKind.APIVersion, targetKind.Kind); ok {
				targetCRD, found = crd, true
			}
		}
	}

	if !found {
		return fmt.Errorf("kind %q not found in cache or blueprint sources; run: cf catalogue %s or cf provider add <ref>", c.Kind, c.Kind)
	}

	if c.Status {
		statusNodes, err := targetCRD.Status()
		if err != nil || len(statusNodes) == 0 {
			fmt.Fprintf(out, "Kind %s (%s) has no status fields.\n", targetKind.Kind, targetKind.APIVersion)
			return nil
		}
		fields := index.Fields(statusNodes, index.FieldQuery{RequiredOnly: c.Required})
		if len(fields) == 0 {
			fmt.Fprintf(out, "Kind %s (%s) has no matching status fields.\n", targetKind.Kind, targetKind.APIVersion)
			return nil
		}

		fmt.Fprintf(out, "KIND:       %s (Status)\n", targetKind.Kind)
		fmt.Fprintf(out, "APIVERSION: %s\n", targetKind.APIVersion)
		fmt.Fprintf(out, "PROVIDER:   %s\n\n", targetKind.Provider)

		w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
		fmt.Fprintln(w, "FIELD\tTYPE\tDESCRIPTION")
		for _, f := range fields {
			desc := strings.ReplaceAll(f.Description, "\n", " ")
			if len(desc) > 80 {
				desc = desc[:77] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", f.Path, f.Type, desc)
		}
		return w.Flush()
	}

	// Normal FieldTree (forProvider or native object tree)
	nodes, err := targetCRD.FieldTree()
	if err != nil || len(nodes) == 0 {
		fmt.Fprintf(out, "Kind %s (%s) has no settable fields.\n", targetKind.Kind, targetKind.APIVersion)
		return nil
	}

	fields := index.Fields(nodes, index.FieldQuery{RequiredOnly: c.Required})
	var branches []index.Field
	if c.Required {
		branches = index.RequiredBranches(nodes)
	}

	if len(fields) == 0 && len(branches) == 0 {
		fmt.Fprintf(out, "Kind %s (%s) has no matching fields.\n", targetKind.Kind, targetKind.APIVersion)
		return nil
	}

	fmt.Fprintf(out, "KIND:       %s\n", targetKind.Kind)
	fmt.Fprintf(out, "APIVERSION: %s\n", targetKind.APIVersion)
	fmt.Fprintf(out, "SCOPE:      %s\n", targetKind.Scope)
	fmt.Fprintf(out, "PROVIDER:   %s\n\n", targetKind.Provider)

	w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tTYPE\tREQUIRED\tDESCRIPTION")

	for _, b := range branches {
		desc := strings.ReplaceAll(b.Description, "\n", " ")
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\ttrue (branch)\t%s\n", b.Path, b.Type, desc)
	}

	for _, f := range fields {
		desc := strings.ReplaceAll(f.Description, "\n", " ")
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		reqStr := "false"
		if f.RequiredChain || f.Required {
			reqStr = "true"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Path, f.Type, reqStr, desc)
	}
	return w.Flush()
}
