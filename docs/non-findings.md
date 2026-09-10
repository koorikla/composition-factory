# Non-findings

Settled questions, recorded so they are not re-raised as issues. Check here and the closed
issues (`gh issue list --state all --search <keyword>`) before filing.

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
