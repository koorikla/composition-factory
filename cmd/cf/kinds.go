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
)

type KindsCmd struct {
	Q         string `arg:"" optional:"" help:"Optional substring/fuzzy filter for kind name, group, or provider."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
	Blueprint string `help:"Path to blueprint file to include declared sources." default:"doc.cf.yaml"`
}

func (c *KindsCmd) Run(out io.Writer) error {
	store := cache.New(c.CacheDir)
	var b *blueprint.Blueprint
	var blueprintDir string

	isDefault := c.Blueprint == "" || c.Blueprint == "doc.cf.yaml" || filepath.Base(c.Blueprint) == "doc.cf.yaml"
	if _, err := os.Stat(c.Blueprint); err == nil || (!isDefault && c.Blueprint != "") {
		loaded, err := blueprint.Load(c.Blueprint)
		if err != nil {
			return err
		}
		b = loaded
		blueprintDir = filepath.Dir(c.Blueprint)
	}

	refs := AssembleProviders(store, b, nil, false)

	idx, err := api.BuildIndex(store, refs, b, blueprintDir)
	if err != nil {
		return err
	}

	var kinds []index.Kind
	if c.Q == "" {
		kinds = idx.All()
	} else {
		// Match against kind, group, apiVersion, or provider
		qLower := strings.ToLower(c.Q)
		for _, k := range idx.All() {
			if strings.Contains(strings.ToLower(k.Kind), qLower) ||
				strings.Contains(strings.ToLower(k.Group), qLower) ||
				strings.Contains(strings.ToLower(k.APIVersion), qLower) ||
				strings.Contains(strings.ToLower(k.Provider), qLower) {
				kinds = append(kinds, k)
			}
		}
	}

	if len(kinds) == 0 {
		if c.Q != "" {
			fmt.Fprintf(out, "No kinds found matching %q.\n", c.Q)
		} else {
			fmt.Fprintln(out, "No kinds found in cache. Run 'cf provider add <ref>' to add a provider.")
		}
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tAPIVERSION\tSCOPE\tREQUIRED/FIELDS\tPROVIDER")
	for _, k := range kinds {
		scope := k.Scope
		if scope == "" {
			if k.Namespaced {
				scope = "Namespaced"
			} else {
				scope = "Cluster"
			}
		}
		reqFields := fmt.Sprintf("%d/%d", k.Required, k.Fields)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", k.Kind, k.APIVersion, scope, reqFields, k.Provider)
	}
	return w.Flush()
}
