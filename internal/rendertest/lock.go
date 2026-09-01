//go:build unix

// Package rendertest serializes REAL `crossplane composition render`
// executions across this repository's test binaries.
//
// Why it must exist: generated functions.yaml pins
// render.crossplane.io/runtime-docker-name (cf-function-go-templating, ...)
// so repeated renders reuse one container per function instead of leaking a
// new one on every run. That is correct for a human running renders — but
// `go test ./...` runs packages as separate processes IN PARALLEL, and both
// the root acceptance tests and internal/api's real-render integration test
// exec the CLI. Two concurrent renders then race on the same container
// name: each creates its own crossplane-render-* network, one creates the
// named container attached to ITS network, and the other finds a container
// that is "not connected" to the network it just made and fails. Observed
// exactly that way on a full `go test ./... -count=1 -race` run; a per-
// process sync.Mutex cannot fix it because the contenders are different
// processes, so this is an flock(2) on one well-known file instead.
//
// Only tests that exec the real CLI take this lock. Everything that stubs
// the render seam stays lock-free and parallel.
package rendertest

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Lock takes the machine-wide render lock for the duration of the calling
// test's real render exec; call the returned release as soon as the render
// has finished so other packages' render tests can proceed.
func Lock(t testing.TB) (release func()) {
	t.Helper()
	path := filepath.Join(os.TempDir(), "cf-crossplane-render-test.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatalf("rendertest: open lock file %s: %v", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		t.Fatalf("rendertest: flock %s: %v", path, err)
	}
	return func() {
		// Closing the descriptor releases the flock; the file itself stays
		// behind for the next run, which is the point of a well-known path.
		_ = f.Close()
	}
}
