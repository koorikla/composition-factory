package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// FunctionCmd groups function subcommands.
type FunctionCmd struct {
	Add FunctionAddCmd `cmd:"" help:"Fetch a function package and cache its input schemas."`
}

// FunctionAddCmd pulls a function xpkg image, extracts its Input CRDs, caches them and pins
// the digest in .cf.lock.
type FunctionAddCmd struct {
	Ref      string `arg:"" help:"xpkg reference, e.g. xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.4.1"`
	CacheDir string `help:"Schema cache directory." default:"${cachedir}"`
	Lock     string `help:"Lockfile path." default:".cf.lock"`

	// fetch is swapped in tests.
	fetch func(ref string) (*xpkg.Package, error)
}

func (c *FunctionAddCmd) Run(out io.Writer) error {
	store := cache.New(c.CacheDir)
	_, loadErr := store.Load(c.Ref)
	wasCached := loadErr == nil
	cleanup := func(ref string) {
		if !wasCached {
			_ = store.Delete(ref)
			_ = os.Remove(c.CacheDir)
		}
	}

	pkg, crds, err := store.FetchAndSave(context.Background(), "", c.Ref, c.fetch)
	if err != nil {
		return err
	}

	inputs := 0
	managed := 0
	for _, crd := range crds {
		if crd.IsFunctionInput() || crd.Function {
			inputs++
		}
		if crd.IsManaged() {
			managed++
		}
	}
	if inputs == 0 && managed > 0 {
		cleanup(pkg.Ref)
		return fmt.Errorf("package %q is a provider package, not a function (use 'cf provider add %s')", c.Ref, c.Ref)
	}
	if err := store.PinLock(c.Lock, pkg.Ref, pkg.Digest); err != nil {
		cleanup(pkg.Ref)
		return err
	}
	noun := "function input schemas"
	if inputs == 1 {
		noun = "function input schema"
	}
	fmt.Fprintf(out, "added %s\n  digest %s\n  %d %s of %d CRDs\n",
		c.Ref, pkg.Digest, inputs, noun, len(crds))
	return nil
}
