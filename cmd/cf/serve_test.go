package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeDefaultsToLoopback(t *testing.T) {
	var c ServeCmd
	if err := defaults(&c); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if c.Addr != "127.0.0.1:8080" {
		t.Errorf("default Addr = %q, want 127.0.0.1:8080 — this server writes files and has no "+
			"authentication, so it must not be reachable off-host by default", c.Addr)
	}
}

func TestServeRefusesNonLoopbackWithoutTheExplicitFlag(t *testing.T) {
	c := ServeCmd{Addr: "0.0.0.0:8080"}
	err := c.check()
	if err == nil {
		t.Fatal("want an error binding 0.0.0.0 without --i-know-this-is-unauthenticated")
	}
	for _, want := range []string{"0.0.0.0", "authentication"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestServeAllowsNonLoopbackWithTheExplicitFlag(t *testing.T) {
	c := ServeCmd{Addr: "0.0.0.0:8080", IKnowThisIsUnauthenticated: true}
	if err := c.check(); err != nil {
		t.Errorf("explicit opt-in still refused: %v", err)
	}
}

func TestServeAcceptsOtherLoopbackForms(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9000", "localhost:9000", "[::1]:9000"} {
		c := ServeCmd{Addr: addr}
		if err := c.check(); err != nil {
			t.Errorf("%s rejected as non-loopback: %v", addr, err)
		}
	}
}

// --- Additional coverage beyond the brief's verbatim tests ---
//
// These exercise the other requirements Task 7's brief states in prose (a
// missing provider must name the exact `cf provider add` command, and the
// server must actually start, serve /healthz and shut down within its
// bounded timeout) but that the given test list above does not cover.

// TestServeMissingProviderNamesAddCommand covers "a provider in sources that
// is not cached is a clear startup error naming the exact `cf provider add
// <ref>` command -- not an empty palette." It reuses gen_test.go's seed
// helper for the blueprint file, but deliberately does NOT seed the cache
// Store it points at, so cache.Store.Load fails exactly the way it would for
// an operator who forgot to run `cf provider add` before `cf serve`.
func TestServeMissingProviderNamesAddCommand(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "xqueue.cf.yaml")
	if err := os.WriteFile(bp, []byte(genBlueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, "cache") // deliberately never populated

	c := &ServeCmd{Addr: "127.0.0.1:0", Blueprint: bp, Out: filepath.Join(dir, "out"), CacheDir: cacheDir}
	var buf bytes.Buffer
	err := c.run(context.Background(), &buf)
	if err == nil {
		t.Fatal("run() with an uncached provider = nil error, want one naming `cf provider add`")
	}
	const wantCmd = "cf provider add example.org/provider-test:v2"
	if !strings.Contains(err.Error(), wantCmd) {
		t.Errorf("error = %q, want it to contain the exact command %q, not an empty palette", err.Error(), wantCmd)
	}
}

// TestServeIntegration is the one place in this task's tests that actually
// binds and serves over real HTTP, per the brief's permitted (and
// encouraged) minimal integration test: bind 127.0.0.1:0 (an ephemeral
// port), hit /healthz, then shut down.
//
// It binds port 0 rather than a fixed one so the test cannot collide with
// another process (or another instance of itself running in parallel), and
// it learns the OS-assigned port through ServeCmd's unexported `ready`
// channel rather than sleeping and guessing -- run() sends the listener's
// real address there right after net.Listen succeeds and before it starts
// serving, so the test knows precisely when a request will actually be
// answered.
//
// It also exercises GET / while it is at it, since that path (no UI in M2,
// just a plain "the API is up" message) is otherwise untested: nothing in
// internal/api registers "/" (see server.go's comment on why), so it only
// exists once ServeCmd's own withRoot wrapper is in front of the handler --
// exactly the composition this test's server is actually running.
func TestServeIntegration(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	c := &ServeCmd{Addr: "127.0.0.1:0", Blueprint: bp, Out: filepath.Join(dir, "out"), CacheDir: cacheDir}
	ready := make(chan string, 1)
	c.ready = ready

	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(ctx, &buf) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-runErr:
		t.Fatalf("server exited before becoming ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not become ready in time")
	}

	healthzResp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	// Drain the body fully before Close(): an http.Response.Body that is
	// closed without being read to EOF prevents the client's Transport from
	// reusing the underlying keep-alive connection (see (*http.Response)'s
	// doc comment), which otherwise makes this test's own shutdown flaky --
	// srv.Shutdown races an idle-connection sweep whose polling interval
	// backs off geometrically, so a connection the server hasn't yet
	// resolved to idle can add several real seconds to a run that would
	// otherwise complete in microseconds. This is purely a test-client
	// hygiene concern; it has no bearing on ServeCmd's own shutdown logic.
	_, _ = io.Copy(io.Discard, healthzResp.Body)
	healthzResp.Body.Close()
	if healthzResp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", healthzResp.StatusCode)
	}

	rootResp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	rootBody, err := io.ReadAll(rootResp.Body)
	rootResp.Body.Close()
	if err != nil {
		t.Fatalf("read GET / body: %v", err)
	}
	if rootResp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", rootResp.StatusCode)
	}
	if !strings.Contains(string(rootBody), "M3") {
		t.Errorf("GET / body = %q, want it to say the canvas arrives with M3 (no UI in M2)", rootBody)
	}

	// Also confirm the real API routes are still reachable through the same
	// wrapped handler -- withRoot's catch-all must not shadow them.
	kindsResp, err := http.Get("http://" + addr + "/api/kinds")
	if err != nil {
		t.Fatalf("GET /api/kinds: %v", err)
	}
	_, _ = io.Copy(io.Discard, kindsResp.Body) // see the /healthz drain comment above
	kindsResp.Body.Close()
	if kindsResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/kinds status = %d, want 200", kindsResp.StatusCode)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("run() after shutdown = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within the bounded graceful-shutdown timeout")
	}

	if !strings.Contains(buf.String(), addr) {
		t.Errorf("startup output = %q, want it to print the listening address %q", buf.String(), addr)
	}
}
