# CF-032 — A CLI user cannot start a blueprint without running a web server

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-032 No CLI scaffold. cf --help has no init, and the providerName error tells a CLI user to start a web server. cf init emitting a minimal valid blueprint would remove the only rough step in an otherwise smooth CLI.` |
| **Worktree** | `.worktrees/CF-032` on branch `CF-032-cf-init` |
| **May write** | `cmd/cf/init.go`, `cmd/cf/init_test.go`, `cmd/cf/main.go`, `docs/cli.md`, `CHANGELOG.md` |
| **Merges after** | nothing |

## Symptom

Every `cf` verb operates on a blueprint that must already exist, and nothing in the
CLI creates one. The first-time CLI user's only documented path to a starting
document is to run `cf serve` and use the canvas — a web server, to obtain a
60-line YAML file. The discovery verbs (`cf kinds`, `cf fields`, `cf catalogue`) are
the best part of this tool and they all assume you already got past this step.

## Evidence

There is no `init` command, in the dispatch table or on disk:

```sh
$ ls cmd/cf/ | grep -i init
$ grep -n 'cmd:""' cmd/cf/main.go
20:	Version     VersionCmd       `cmd:"" help:"Print the cf version."`
21:	Provider    ProviderCmd      `cmd:"" help:"Manage provider schema sources."`
22:	Function    FunctionCmd      `cmd:"" help:"Manage function schema sources."`
23:	Gen         GenCmd           `cmd:"" help:"Generate XRD, Composition and functions.yaml from a blueprint."`
24:	Serve       ServeCmd         `cmd:"" help:"Serve the compositionfactory HTTP API, loopback-only by default."`
25:	MCP         MCPCmd           `cmd:"" name:"mcp" ...`
26:	Package     PackageCmd       `cmd:"" ...`
27:	Push        PushCmd          `cmd:"" ...`
28:	Adopt       AdoptCmd         `cmd:"" aliases:"import" ...`
29:	Kinds       KindsCmd         `cmd:"" ...`
30:	Fields      FieldsCmd        `cmd:"" ...`
31:	Catalogue   CatalogueCmd     `cmd:"" ...`
```

Verified twice on `3a3fef4`.

## Location

`cmd/cf/main.go:19-32` — the kong root. Subcommands are struct fields with a `cmd:""`
tag and a `Run` method; `cmd/cf/gen.go` is the closest existing shape (takes a
blueprint path, writes files, reports what it wrote).

## Acceptance test

Write this first, verbatim, and watch it fail before you write `init.go`.

```go
// cmd/cf/init_test.go
package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func runCF(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var cli CLI
	opts := append(kongOptions(), kong.Exit(func(int) {}))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	ctx.BindTo(&out, (*io.Writer)(nil))
	runErr := ctx.Run()
	return out.String(), runErr
}

func TestInitWritesAMinimalValidBlueprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blueprint.cf.yaml")

	if _, err := runCF(t, "init", path); err != nil {
		t.Fatalf("cf init: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cf init wrote no blueprint: %v", err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("cf init wrote a blueprint that does not parse: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("cf init wrote a blueprint that does not validate: %v", err)
	}
	if b.Spec.XRD.Kind == "" || b.Spec.XRD.Group == "" || b.Spec.XRD.Version == "" {
		t.Errorf("cf init wrote an XRD with no identity: %+v", b.Spec.XRD)
	}
	if _, ok := b.Spec.XRD.Parameters["providerName"]; !ok {
		t.Error("cf init omitted providerName, which every Namespaced XRD requires")
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blueprint.cf.yaml")
	const existing = "existing: document\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runCF(t, "init", path); err == nil {
		t.Fatal("cf init overwrote an existing file")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Errorf("file was modified: %q", data)
	}
}
```

**Fails today with:**

```
=== RUN   TestInitWritesAMinimalValidBlueprint
    init_test.go:38: cf init: unexpected argument init
--- FAIL: TestInitWritesAMinimalValidBlueprint (0.00s)
=== RUN   TestInitRefusesToOverwrite
--- PASS: TestInitRefusesToOverwrite (0.00s)
FAIL
```

Run on `3212106`. **`TestInitRefusesToOverwrite` passes today, and that pass is
worthless** — `parser.Parse` rejects the unknown `init` verb, `runCF` returns that
error, the test sees a non-nil error and concludes the file was protected. It starts
testing the behaviour it names only once `init` exists. Do not read its green as
coverage before then, and re-read its failure mode if you ever see it go red.

## Contract

`cf init [path]` writes one blueprint and nothing else.

- Default path `blueprint.cf.yaml` in the working directory; an explicit path
  overrides it. Parent directories are not created — a missing parent is an error.
- **Never overwrite.** An existing file at the target is an error naming the path,
  exit non-zero, file untouched. No `--force` in this task; if you think it is
  needed, say so in the handover rather than adding it.
- The document must satisfy `blueprint.Parse` then `Validate`, and must carry the
  `providerName` parameter — a Namespaced XRD without it is rejected downstream, and
  `scope: Cluster` is refused, so a scaffold omitting it hands the user a document
  that cannot generate. This is the CF-042 trap; do not reproduce it in the scaffold.
- No network. `cf init` must work offline, with an empty cache. That constrains what
  you may scaffold: a resource whose CRD is not cached cannot be validated, so a
  scaffold with zero resources and a commented example is acceptable and probably
  correct. Choose, and say which you chose and why.
- Print what was written and the single obvious next command.
- Document it in `docs/cli.md` under `## Command Reference`, in the shape the
  neighbouring entries use, and add a `CHANGELOG.md` `[Unreleased]` entry.

## Verification

```sh
make lint && make lint-strict && make test-race
go test ./cmd/cf/ -run TestInit -v
./bin/cf init /tmp/x.cf.yaml && ./bin/cf gen /tmp/x.cf.yaml --out /tmp/xout   # offline, empty cache
```

## Out of scope

`--force`. Interactive prompting. Templates or example selection (`cf init
--example rds`) — file it if you want it. Changing the `providerName` error message
that sends CLI users to the web server; that is CF-042's docs fix.

## Handover

Branch `CF-032-cf-init`, committed, not pushed, not merged. Paste both runs of the
acceptance test, state whether you scaffolded with or without a resource and why, and
report the offline `cf gen` result on the scaffold's own output.
