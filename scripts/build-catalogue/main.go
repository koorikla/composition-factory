// Command build-catalogue enumerates crossplane-contrib's provider-* and
// function-* repositories and their published ghcr.io images, and writes
// the result to catalogue/providers.json — the static, CI-refreshed index
// GET /api/catalogue serves (see internal/api/catalogue.go and
// docs/catalogue.md for why it has to be static rather than queried live).
//
// Two ways to run it:
//
//	go run ./scripts/build-catalogue --out catalogue/providers.json
//
// Live mode: talks to api.github.com and ghcr.io directly, anonymously, no
// credentials. This is what .github/workflows/catalogue.yml runs on a
// weekly cron.
//
//	go run ./scripts/build-catalogue --from-file manifest.json --out catalogue/providers.json
//
// Offline mode: reads a manifest (see manifest.go) instead of the network.
// This is what every test in this package uses (no test may touch the
// network — see build_test.go, github_test.go, ghcr_test.go), and what a
// sandboxed environment where a compiled Go binary cannot reach the network
// at all — this repo's own dev sandbox is exactly that — has to use in
// place of live mode. docs/catalogue.md has the curl recipe that built the
// manifest catalogue/providers.json was actually generated from.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// defaultOrg is the GitHub org / ghcr.io namespace this catalogue indexes.
const defaultOrg = "crossplane-contrib"

// githubAPIBaseURL and ghcrBaseURL are the real endpoints live mode talks
// to. Threaded through as parameters (not compiled-in constants inside
// github.go/ghcr.go) so github_test.go and ghcr_test.go can point the exact
// same request-building code at an httptest server instead — no test in
// this package touches the real network.
const (
	githubAPIBaseURL = "https://api.github.com"
	ghcrBaseURL      = "https://ghcr.io"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "build-catalogue:", err)
		os.Exit(1)
	}
}

// run is main's testable body: every dependency on the outside world (argv,
// stdout/stderr, the network) is a parameter or an explicit flag, so
// main_test.go can drive it end to end in --from-file mode without a
// subprocess.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build-catalogue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fromFile := fs.String("from-file", "", "read a pre-fetched manifest JSON instead of the network (see manifest.go)")
	out := fs.String("out", "catalogue/providers.json", "path to write the catalogue JSON to")
	org := fs.String("org", defaultOrg, "GitHub org / ghcr.io namespace to enumerate")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var repos []repo
	var tags map[string][]string

	if *fromFile != "" {
		m, err := loadManifest(*fromFile)
		if err != nil {
			return err
		}
		repos = m.repos(*org)
		tags = m.Tags
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		client := &http.Client{Timeout: 20 * time.Second}

		all, err := fetchGitHubRepos(ctx, client, githubAPIBaseURL, *org)
		if err != nil {
			if len(all) == 0 {
				return fmt.Errorf("fetch %s repos: %w", *org, err)
			}
			// Partial result: proceed with what we got (rate-limit-graceful —
			// see fetchGitHubRepos' doc comment), but say so loudly.
			fmt.Fprintln(stderr, "build-catalogue:", err)
		}
		filtered := filterCatalogueRepos(all)
		repos = toRepos(*org, filtered)
		tags = fetchAllGhcrTags(ctx, client, ghcrBaseURL, *org, repos, func(format string, a ...any) {
			fmt.Fprintf(stderr, format+"\n", a...)
		})
	}

	entries := buildCatalogue(repos, tags)
	if err := writeCatalogue(*out, entries); err != nil {
		return err
	}

	resolved := 0
	for _, e := range entries {
		if e.Ref != "" {
			resolved++
		}
	}
	fmt.Fprintf(stdout, "build-catalogue: wrote %d entries (%d with a resolved ref) to %s\n", len(entries), resolved, *out)
	return nil
}
