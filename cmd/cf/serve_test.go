package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

// Every spelling of "bind everything" must be refused, not just the obvious
// one.
//
// Fix round 2 (Important): this test covered 0.0.0.0:8080 alone, and the two
// spellings it left out are the two that do not look like an address at all
// — ":8080" leaves the host empty, and "[::]:8080" is the IPv6 unspecified
// address. Both bind every interface on the machine. The behaviour was
// already correct, but nothing pinned it: injecting `if host == "" { return
// true }` into isLoopbackHost left the whole suite green while opening an
// unauthenticated, file-writing API to the network. With ":8080" here, that
// injection fails this test.
// TestServeDefaultsLockfilePath pins --lock's default to the same ".cf.lock"
// ProviderAddCmd uses: POST /api/providers and `cf provider add` pin into
// the same lockfile unless the operator says otherwise, so a provider added
// from the canvas and one added from the CLI can never end up pinned in two
// different files by default.
func TestServeDefaultsLockfilePath(t *testing.T) {
	var c ServeCmd
	if err := defaults(&c); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if c.Lock != ".cf.lock" {
		t.Errorf("default Lock = %q, want .cf.lock (ProviderAddCmd's default)", c.Lock)
	}
}

func TestServeRefusesNonLoopbackWithoutTheExplicitFlag(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080"} {
		c := ServeCmd{Addr: addr}
		err := c.check()
		if err == nil {
			t.Errorf("--addr %s was accepted without --i-know-this-is-unauthenticated: it binds every "+
				"interface, which exposes this unauthenticated, file-writing server to the network", addr)
			continue
		}
		for _, want := range []string{addr, "authentication"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("--addr %s: error %q does not mention %q", addr, err.Error(), want)
			}
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
// It also exercises the embedded UI while it is at it: GET / must serve the
// canvas app's HTML and GET /js/store.js must come back with a JavaScript
// content type. Nothing in internal/api registers "/" (see server.go's
// comment on why), so the UI only exists once ServeCmd's own withUI wrapper
// is in front of the handler -- exactly the composition this test's server
// is actually running.
func TestServeIntegration(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	c := &ServeCmd{Addr: "127.0.0.1:0", Blueprint: bp, Out: filepath.Join(dir, "out"),
		CacheDir: cacheDir, Lock: filepath.Join(dir, ".cf.lock")}
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
	if !strings.Contains(string(rootBody), "Composition Factory Canvas") {
		t.Errorf("GET / body does not look like the embedded canvas app (no title marker); got %d bytes starting %.80q",
			len(rootBody), rootBody)
	}

	// A module asset must come back with a JavaScript content type, or the
	// browser refuses to execute it as an ES module.
	jsResp, err := http.Get("http://" + addr + "/js/store.js")
	if err != nil {
		t.Fatalf("GET /js/store.js: %v", err)
	}
	_, _ = io.Copy(io.Discard, jsResp.Body) // see the /healthz drain comment above
	jsResp.Body.Close()
	if jsResp.StatusCode != http.StatusOK {
		t.Errorf("GET /js/store.js status = %d, want 200", jsResp.StatusCode)
	}
	if ct := jsResp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET /js/store.js Content-Type = %q, want a JavaScript type", ct)
	}

	// Also confirm the real API routes are still reachable through the same
	// wrapped handler -- the UI mount must not shadow them, and they must
	// still answer JSON, not HTML.
	kindsResp, err := http.Get("http://" + addr + "/api/kinds")
	if err != nil {
		t.Fatalf("GET /api/kinds: %v", err)
	}
	_, _ = io.Copy(io.Discard, kindsResp.Body) // see the /healthz drain comment above
	kindsResp.Body.Close()
	if kindsResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/kinds status = %d, want 200", kindsResp.StatusCode)
	}
	if ct := kindsResp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("GET /api/kinds Content-Type = %q, want application/json — the UI mount must not "+
			"swallow API routes", ct)
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

// TestServeNoUIServesAPIOnly: with --no-ui the embedded canvas is not
// mounted — / and every asset path 404 — while the API itself is completely
// untouched. The 404s come back in the API's own JSON error shape, because
// without the UI mount the api handler serves every path.
func TestServeNoUIServesAPIOnly(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	c := &ServeCmd{Addr: "127.0.0.1:0", Blueprint: bp, Out: filepath.Join(dir, "out"),
		CacheDir: cacheDir, Lock: filepath.Join(dir, ".cf.lock"), NoUI: true}
	ready := make(chan string, 1)
	c.ready = ready

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

	get := func(path string) (int, string, string) {
		t.Helper()
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, err := io.ReadAll(resp.Body) // full drain; see TestServeIntegration's comment
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read GET %s body: %v", path, err)
		}
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
	}

	for _, path := range []string{"/", "/js/store.js"} {
		code, ct, body := get(path)
		if code != http.StatusNotFound {
			t.Errorf("GET %s with --no-ui: status = %d, want 404", path, code)
		}
		if !strings.Contains(ct, "application/json") || !strings.Contains(body, `"error"`) {
			t.Errorf("GET %s with --no-ui: Content-Type = %q body = %q, want the API's JSON error shape", path, ct, body)
		}
	}

	code, ct, _ := get("/api/kinds")
	if code != http.StatusOK {
		t.Errorf("GET /api/kinds with --no-ui: status = %d, want 200 — --no-ui must not touch the API", code)
	}
	if !strings.Contains(ct, "application/json") {
		t.Errorf("GET /api/kinds with --no-ui: Content-Type = %q, want application/json", ct)
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
}

// The embedded UI is a build-time snapshot; a checkout serving stale code in
// a fresh browser looked like time travel. When ./web-proto exists on disk,
// serve THAT.
func TestServePrefersLiveWebProtoOverEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web-proto"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "<title>live-from-disk</title>"
	if err := os.WriteFile(filepath.Join(dir, "web-proto", "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	h := withUI(http.NotFoundHandler(), false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), "live-from-disk") {
		t.Fatalf("/ served the embedded snapshot, not the on-disk web-proto; body: %.120s", rec.Body.String())
	}
}

func TestServeFallsBackToEmbeddedWithoutWebProto(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	h := withUI(http.NotFoundHandler(), false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Composition Factory") {
		t.Fatalf("embedded fallback broken: code=%d body: %.120s", rec.Code, rec.Body.String())
	}
}
