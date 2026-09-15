# Non-findings

Settled questions, recorded so they are not re-raised as issues. Check here and the closed
issues (`gh issue list --state all --search <keyword>`) before filing.

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
- [x] When running without a local Docker daemon (e.g. CI environments lacking `/var/run/docker.sock`), `cf validate` reports "validation check unavailable" (CF-093) and KCL docker execution tests skip/fail. Expected environment limitation, not an engine defect.
- [x] Lock-write-error unit tests (`TestCF098AddProviderReportsLockWriteError`, etc.) cannot fail when run as root. Expected OS privilege behavior.
- [x] In the Inspector, the ServiceAccount's Fields list shows only the whole-map `metadata.annotations` row. Per-key annotation binding is intentionally handled via the drag-to-card suggestion picker (which type-checks string vs map).
- [x] Generate success feedback uses the `#out-next-steps` banner rather than a temporary toast in `#toast-container` (guarded by `cf090-generate-toast-covers-all-outputs.spec.js`).
