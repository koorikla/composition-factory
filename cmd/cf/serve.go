package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/api"
	webproto "github.com/koorikla/compositionfactory/web-proto"
)

// shutdownTimeout bounds how long a SIGINT/SIGTERM's graceful shutdown waits
// for in-flight requests to finish before giving up. This server has no
// long-running requests by design (schema lookups and blueprint edits are
// all in-memory or single-file operations), so this only needs to be long
// enough to let a request that is already in flight complete, not to drain a
// queue.
const shutdownTimeout = 10 * time.Second

// ServeCmd starts the compositionfactory HTTP API over the blueprint and
// provider schema cache — schema browsing, blueprint editing and `cf gen`'s
// generate step — and serves the embedded canvas GUI at /, so `cf serve`
// alone gives the full app at the printed address. --no-ui drops the GUI and
// serves the API only (for MCP tooling, or a dev iterating on web-proto/
// through serve.py's live proxy instead of the embedded snapshot).
//
// SECURITY: this server has no authentication, by design -- see
// internal/api's package comment for the full reasoning. It also both reads
// the local schema cache and writes to the blueprint file and the output
// directory on the caller's behalf. Both of those are only acceptable
// because the server is unreachable off-host: Addr defaults to loopback, and
// check refuses to bind anywhere else unless the operator explicitly opts in
// via --i-know-this-is-unauthenticated.
type ServeCmd struct {
	Addr                       string `help:"Address to listen on. Must be loopback unless --i-know-this-is-unauthenticated is set." default:"127.0.0.1:8080"`
	Blueprint                  string `help:"Path to the blueprint file to serve." required:""`
	Out                        string `short:"o" help:"Output directory that POST /api/generate writes into." default:"."`
	CacheDir                   string `help:"Schema cache directory." default:"${cachedir}"`
	Lock                       string `help:"Lockfile path that POST /api/providers pins newly added providers into." default:".cf.lock"`
	NoUI                       bool   `help:"Serve only the API: do not serve the embedded canvas GUI at /."`
	IKnowThisIsUnauthenticated bool   `help:"Allow binding a non-loopback address. This server has no authentication and writes files to disk on your behalf -- only set this if you understand and accept that a non-loopback bind exposes both to your network."`

	// ready, when non-nil, receives the actual listening address (host:port)
	// once the listener is open and before this blocks serving requests --
	// so a test that binds an ephemeral port (":0") can learn the real port
	// and know precisely when the server is ready, without sleeping or
	// polling. An unexported field kong's reflection does not see, exactly
	// like ProviderAddCmd's unexported `fetch` seam in provider.go.
	ready chan<- string
}

// defaults applies kong's declared flag defaults (the `default:"..."` tags
// on ServeCmd's fields above) onto c, without going through a full Parse --
// so a test can assert on ServeCmd's zero-value defaults without building a
// complete CLI invocation or supplying the required --blueprint flag.
//
// It builds the exact same grammar kong.Parse would, via kongOptions() (so
// the ${cachedir} var resolves identically to production), traces an empty
// argument list to get a Context, and applies defaults through kong's own
// Context.ApplyDefaults. That writes through the reflect.Value kong.New
// already bound directly to c's fields, so this exercises the real default
// resolution the CLI itself uses -- not a hand-rolled reimplementation of it
// that could silently drift from what kong.Parse actually does.
func defaults(c *ServeCmd) error {
	k, err := kong.New(c, kongOptions()...)
	if err != nil {
		return err
	}
	ctx, err := kong.Trace(k, nil)
	if err != nil {
		return err
	}
	return ctx.ApplyDefaults()
}

// check resolves the host in c.Addr and refuses to proceed unless it is
// loopback or the operator has set --i-know-this-is-unauthenticated. The
// error names both what is wrong (the address is not loopback-only) and why
// it matters (this server has no authentication and writes files to disk),
// so a caller sees a decision to make, not just a rule to satisfy.
func (c *ServeCmd) check() error {
	if c.IKnowThisIsUnauthenticated {
		return nil
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("--addr %q is not a valid host:port: %w", c.Addr, err)
	}
	if isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf(
		"--addr %s binds %q, which is not loopback-only. This server has no authentication "+
			"(it is a local dev tool -- see the package comment in internal/api) and it writes "+
			"files to disk on your behalf (the blueprint file, and whatever POST /api/generate "+
			"emits), so binding it to a non-loopback address would expose an unauthenticated, "+
			"filesystem-writing API to your network. If you understand that and still want it, "+
			"pass --i-know-this-is-unauthenticated", c.Addr, host)
}

// isLoopbackHost reports whether host -- the host portion of an already
// split addr, so a bracketed IPv6 literal like "::1" arrives unbracketed --
// is loopback: 127.0.0.0/8, ::1, or the "localhost" name every OS resolves
// to one of those.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Run is ServeCmd's kong entry point. It ties the server's lifetime to
// SIGINT/SIGTERM so an operator's Ctrl-C (or a supervising process's TERM)
// triggers the same bounded graceful shutdown run performs on context
// cancellation, rather than the process dying mid-request.
func (c *ServeCmd) Run(out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return c.run(ctx, out)
}

// run does the actual work; Run is a thin wrapper that supplies the
// signal-bound context. Split out so tests can drive shutdown deterministically
// by cancelling a context they control, instead of sending the process a
// real signal.
func (c *ServeCmd) run(ctx context.Context, out io.Writer) error {
	if err := c.check(); err != nil {
		return err
	}

	// The single-load invariant (index and store built from one CRD load)
	// lives in buildAPIOptions, shared with `cf mcp` — see cmd/cf/options.go.
	o, err := buildAPIOptions(c.Blueprint, c.CacheDir, c.Out, c.Lock)
	if err != nil {
		return err
	}

	handler, err := api.New(o)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", c.Addr, err)
	}

	fmt.Fprintf(out, "cf serve: listening on http://%s\n", ln.Addr())
	if c.ready != nil {
		c.ready <- ln.Addr().String()
	}

	srv := &http.Server{Handler: withUI(handler, c.NoUI)}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		// Serve always returns (a non-nil, ErrServerClosed) once Shutdown
		// completes; drain it so the goroutine above does not leak.
		<-serveErr
		return nil
	}
}

// withUI mounts the embedded canvas app (web-proto/, via webproto.Files) in
// front of api, so `cf serve` alone gives the full GUI at /: index.html at
// the root, css/ and js/ served with the content types their extensions map
// to (http.FileServerFS goes through mime.TypeByExtension — .js is
// text/javascript, .css is text/css), and the app's relative /api/... calls
// landing on the very same origin, so they just work with no proxy.
//
// Routing is a different, outer mux wrapping api as one opaque handler —
// the same composition the previous withRoot used, and for the same reason:
// api's own mux deliberately registers no "/" pattern (see
// internal/api/server.go's comment on why a catch-all there would defeat
// ServeMux's built-in 404-vs-405 handling). /api/ and /healthz forward to
// api completely unmodified, method checks, 404s and 405s included; every
// other path is the UI's. The one behavioral trade: an unknown non-API path
// (/nope) is now the file server's plain-text 404 rather than api's JSON
// 404 — right for a browser-facing tree, and API clients talk to /api/*,
// whose error shape is unchanged.
//
// With noUI set there is no outer mux at all: api serves every path, so the
// UI paths 404 (in api's JSON shape) while the API itself is untouched.
func withUI(api http.Handler, noUI bool) http.Handler {
	if noUI {
		return api
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/healthz", api)
	mux.Handle("/", noStore(http.FileServerFS(webproto.Files)))
	return mux
}

// noStore disables client caching on every UI response. Same discipline as
// web-proto/serve.py's dev server, same reason: the assets change with every
// rebuild of this binary, and a browser that cached a module from the
// previous build ghosts stale code behind the module cache — the class of
// silent staleness this project exists to avoid. The payloads are a few
// hundred KB served over loopback, so re-fetching them is free.
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// A page whose modules were cached under an older policy (or an
		// older build) ghosts stale code forever; wiping the origin's HTTP
		// cache on every document load makes each visit self-healing.
		// Loopback-only traffic, a few hundred KB — refetching is free.
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Clear-Site-Data", `"cache"`)
		}
		h.ServeHTTP(w, r)
	})
}
