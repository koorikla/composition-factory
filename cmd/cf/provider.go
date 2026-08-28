package main

import (
	"context"
	"fmt"
	"io"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// ProviderCmd groups provider subcommands.
type ProviderCmd struct {
	Add ProviderAddCmd `cmd:"" help:"Fetch a provider package and cache its schemas."`
}

// ProviderAddCmd pulls an xpkg image, extracts its CRDs, caches them and pins
// the digest. It needs no cluster and no Docker.
type ProviderAddCmd struct {
	Ref      string `arg:"" help:"xpkg reference, e.g. xpkg.upbound.io/upbound/provider-aws-sqs:v2"`
	CacheDir string `help:"Schema cache directory." default:"${cachedir}"`
	Lock     string `help:"Lockfile path." default:".cf.lock"`

	// fetch is swapped in tests.
	fetch func(ref string) (*xpkg.Package, error)
}

func (c *ProviderAddCmd) Run(out io.Writer) error {
	fetch := c.fetch
	if fetch == nil {
		fetch = func(ref string) (*xpkg.Package, error) {
			return xpkg.Fetch(context.Background(), ref)
		}
	}
	pkg, err := fetch(c.Ref)
	if err != nil {
		return err
	}
	crds, err := schema.ParseCRDs(pkg.Docs)
	if err != nil {
		return fmt.Errorf("%s: %w", c.Ref, err)
	}
	if err := cache.New(c.CacheDir).Save(pkg, crds); err != nil {
		return err
	}
	l, err := cache.ReadLock(c.Lock)
	if err != nil {
		return err
	}
	l.Set(c.Ref, pkg.Digest)
	if err := l.Write(c.Lock); err != nil {
		return err
	}

	managed := 0
	for _, crd := range crds {
		if crd.IsManaged() {
			managed++
		}
	}
	noun := "managed resources"
	if managed == 1 {
		noun = "managed resource"
	}
	fmt.Fprintf(out, "added %s\n  digest %s\n  %d %s of %d CRDs\n",
		c.Ref, pkg.Digest, managed, noun, len(crds))
	if managed == 0 {
		fmt.Fprintf(out, "  note: this package defines no managed resources. Family packages\n"+
			"  carry only ProviderConfig types; add the service package too.\n")
	}
	return nil
}
