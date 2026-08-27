// Command cf generates Crossplane Compositions and XRDs from provider schemas.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// CLI is the kong root. Subcommands are added as fields in later tasks.
type CLI struct {
	Version VersionCmd `cmd:"" help:"Print the cf version."`
}

type VersionCmd struct{}

func (v *VersionCmd) Run(out io.Writer) error {
	_, err := fmt.Fprintf(out, "cf %s\n", version)
	return err
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("cf"),
		kong.Description("Generate Crossplane Compositions and XRDs from provider schemas."),
		kong.UsageOnError(),
		kong.BindTo(os.Stdout, (*io.Writer)(nil)),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
