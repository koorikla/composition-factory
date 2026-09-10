# Backlog

**The backlog lives in GitHub Issues:** https://github.com/koorikla/composition-factory/issues

```sh
gh issue list --label severity:P0,severity:P1          # what to pick next
gh issue list --label brief-ready                       # dispatchable now
gh issue list --state all --search "CF-0NN"             # history of one id
```

How items are filed, triaged, taken and closed is `AGENTS.md` §4; the authoring rules are
`.claude/skills/backlog-authoring/`; the execution side is `docs/task-execution-contract.md`.
Settled non-findings: [docs/non-findings.md](docs/non-findings.md). Everything closed before
2026-09-11 is in [docs/backlog-archive.md](docs/backlog-archive.md); `git log -p BACKLOG.md`
has the full pre-migration history.

## Architectural principles

They moved to `AGENTS.md` §1 ("Blueprint as Single Source of Truth", "The Round-Trip Rule"),
where they bind every agent.

## Migration record (2026-09-11, tree `6efa086`)

| id | issue |
|---|---|
| CF-130 | [#3](https://github.com/koorikla/composition-factory/issues/3) |
| CF-092 | [#4](https://github.com/koorikla/composition-factory/issues/4) |
| CF-107 | [#5](https://github.com/koorikla/composition-factory/issues/5) |
| CF-108 | [#6](https://github.com/koorikla/composition-factory/issues/6) |
| CF-119 | [#7](https://github.com/koorikla/composition-factory/issues/7) |
| CF-093 | [#8](https://github.com/koorikla/composition-factory/issues/8) |
| CF-135 | [#9](https://github.com/koorikla/composition-factory/issues/9) |
| CF-103 | [#10](https://github.com/koorikla/composition-factory/issues/10) |
| CF-104 | [#11](https://github.com/koorikla/composition-factory/issues/11) |
| CF-105 | [#12](https://github.com/koorikla/composition-factory/issues/12) |
| CF-115 | [#13](https://github.com/koorikla/composition-factory/issues/13) |
| CF-118 | [#14](https://github.com/koorikla/composition-factory/issues/14) |
| CF-127 | [#15](https://github.com/koorikla/composition-factory/issues/15) |
| CF-128 | [#16](https://github.com/koorikla/composition-factory/issues/16) |
| CF-131 | [#17](https://github.com/koorikla/composition-factory/issues/17) |
| CF-132 | [#18](https://github.com/koorikla/composition-factory/issues/18) |
| CF-133 | [#19](https://github.com/koorikla/composition-factory/issues/19) |
| CF-134 | [#20](https://github.com/koorikla/composition-factory/issues/20) |
