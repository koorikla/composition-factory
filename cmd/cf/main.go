// Command cf generates Crossplane Compositions and XRDs from provider schemas.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/cache"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// CLI is the kong root. Subcommands are added as fields in later tasks.
type CLI struct {
	VersionFlag kong.VersionFlag `name:"version" help:"Print the cf version."`
	Version     VersionCmd       `cmd:"" help:"Print the cf version."`
	Provider    ProviderCmd      `cmd:"" help:"Manage provider schema sources."`
	Gen         GenCmd           `cmd:"" help:"Generate XRD, Composition and functions.yaml from a blueprint."`
	Serve       ServeCmd         `cmd:"" help:"Serve the compositionfactory HTTP API, loopback-only by default."`
	MCP         MCPCmd           `cmd:"" name:"mcp" help:"Serve the compositionfactory MCP server over stdio, for agent tooling."`
	Package     PackageCmd       `cmd:"" help:"Build a Crossplane Configuration package (.xpkg) from a blueprint."`
	Push        PushCmd          `cmd:"" help:"Push a built .xpkg to an OCI registry."`
	Adopt       AdoptCmd         `cmd:"" help:"Import an existing Crossplane Composition into a blueprint."`
	Kinds       KindsCmd         `cmd:"" help:"List available CRD kinds from cached providers and native kinds."`
	Fields      FieldsCmd        `cmd:"" help:"Print the field schema tree for a given kind."`
	Catalogue   CatalogueCmd     `cmd:"" help:"Search the provider catalogue."`
}

type VersionCmd struct{}

func (v *VersionCmd) Run(out io.Writer) error {
	_, err := fmt.Fprintf(out, "cf %s\n", version)
	return err
}

func main() {
	var cli CLI
	opts := append(kongOptions(), kong.BindTo(os.Stdout, (*io.Writer)(nil)))
	ctx := kong.Parse(&cli, opts...)
	ctx.FatalIfErrorf(ctx.Run())
}

// kongOptions are the kong.Option values shared between the real CLI and its
// tests: the name, description, error handling and the ${cachedir} var that
// every subcommand's default cache directory flag resolves against. Keeping
// this in one place means a new subcommand with the same default (Gen, in a
// later task) only needs the var bound here, not re-duplicated at every
// kong.New/kong.Parse call site. main() and tests each add their own writer
// binding on top, since that differs between a real stdout and a test buffer.
func kongOptions() []kong.Option {
	return []kong.Option{
		kong.Name("cf"),
		kong.Description("Generate Crossplane Compositions and XRDs from provider schemas."),
		kong.UsageOnError(),
		kong.Vars{
			"cachedir": cache.DefaultRoot(),
			"version":  "cf " + version,
		},
	}
}
