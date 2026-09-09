# CF-113 — CLI and docs ergonomics polish across help, init defaults, and error guidance

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (polish: documentation and message consistency) |
| **Closes** | `CF-113 — CLI/docs polish from the same run: cf help exits 80 as an error while cf --help says to use it; cf init writes blueprint.cf.yaml but kinds/fields/serve default to doc.cf.yaml and init's own hint points at cf kinds; the providerName remedy sends CLI users to cf serve; --validate failures cite line N of a file never written; cf fields Instanc silently resolves to InstanceProfile; docs/dsl.md quotes four error messages the binary no longer emits; docs/cli.md omits cf adopt <dir>, -, the import alias and provider add --lock/--cache-dir; docs/mcp.md:3 says "full authoring surface" for 19 of 36 routes and never names the api_version argument.` |
| **Worktree** | `.worktrees/CF-113` on branch `CF-113-cli-polish` |
| **May write** | `cmd/cf/main.go`, `cmd/cf/init.go`, `cmd/cf/kinds.go`, `cmd/cf/fields.go`, `cmd/cf/serve.go`, `internal/blueprint/validate_params.go`, `docs/dsl.md`, `docs/cli.md`, `docs/mcp.md`, `cmd/cf/main_test.go` |
| **Merges after** | nothing |

## Symptom

Multiple small inconsistencies between the CLI flags, defaults, error messages, and documentation:
1. `cf help` exits with error code 80 (`cf: error: unexpected argument help`) while `cf --help` footer instructs `Run "cf <command> --help"`. Kong needs `kong.Help(kong.HelpOptions{})` or a dedicated `HelpCmd` / kong configuration so `cf help` and `cf help <cmd>` exit 0 and print help.
2. `cf init` writes `blueprint.cf.yaml`, but `cf kinds`, `cf fields`, and `cf serve` default to `--blueprint doc.cf.yaml`. Also, `cf init` says: `next: cf kinds, cf provider add, or cf gen blueprint.cf.yaml` without passing `--blueprint blueprint.cf.yaml` to `kinds`. Unifying default blueprint path to `doc.cf.yaml` or supporting both / ensuring consistent defaults.
3. In `internal/blueprint/validate_params.go:65-66`: the `providerName` error text says:
   `spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}`
   CLI users run `cf gen` and are told to run `cf serve` to scaffold; the CLI scaffold command is `cf init`. It should mention `cf init` or general guidance (`run cf init to scaffold one, or add: ...`).
4. Docs mismatch updates:
   - `docs/cli.md`: document `cf adopt <dir>`, `-`, the `import` alias, and `provider add --lock/--cache-dir`.
   - `docs/mcp.md`: clarify tool surface and document `api_version` argument in `get_kind_fields`.

## Acceptance test

Write this test **first**, verbatim in `cmd/cf/main_test.go`, and watch it fail before changing production code:

```go
func TestCF113HelpCommandSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := CLI{}
	parser, err := kong.New(&cli, append(kongOptions(), kong.Writers(&stdout, &stderr))...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"help"})
	if err != nil {
		t.Fatalf("parse 'help': %v (stderr: %s)", err, stderr.String())
	}
	if err := ctx.Run(); err != nil {
		t.Fatalf("run 'help': %v", err)
	}
}

func TestCF113ProviderNameRemedyMentionsInit(t *testing.T) {
	badYAML := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  xrd:
    group: platform.example.org
    version: v1alpha1
    kind: XApp
    plural: xapps
    scope: Namespaced
  resources:
    - name: bucket
      kind: Bucket
      provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0
`
	_, err := blueprint.Parse([]byte(badYAML))
	if err == nil {
		t.Fatal("expected error for missing providerName, got nil")
	}
	if !strings.Contains(err.Error(), "cf init") {
		t.Errorf("expected providerName error to mention 'cf init', got: %v", err)
	}
}
```

## Contract

- `cf help` and `cf help <subcommand>` must execute cleanly with exit 0 and print help output.
- `providerName` error text must mention `cf init` as the CLI scaffold alternative.
- Documentation in `docs/cli.md` and `docs/mcp.md` must accurately reflect CLI aliases, options, and tool parameters.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-113-cli-polish`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
