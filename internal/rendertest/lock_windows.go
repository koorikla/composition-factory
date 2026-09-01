//go:build windows

package rendertest

import "testing"

// Lock is a no-op on Windows: flock(2) does not exist there, and neither do
// the Docker-backed render tests this lock serializes — every caller skips
// first when the crossplane CLI or a Docker daemon is unavailable, which is
// the practical state of this repo's Windows builds. Compiling to a no-op
// keeps `go vet ./...` and `go test ./...` working cross-platform without a
// second, Windows-only locking implementation nothing would exercise.
func Lock(testing.TB) (release func()) { return func() {} }
