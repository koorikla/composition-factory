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
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// shutdownTimeout bounds how long a SIGINT/SIGTERM's graceful shutdown waits
// for in-flight requests to finish before giving up. This server has no
// long-running requests by design (schema lookups and blueprint edits are
// all in-memory or single-file operations), so this only needs to be long
// enough to let a request that is already in flight complete, not to drain a
// queue.
const shutdownTimeout = 10 * time.Second

// ServeCmd starts the compositionfactory HTTP API over the blueprint and
// provider schema cache: schema browsing, blueprint editing and `cf gen`'s
// generate step, all served for the canvas (M3) and MCP tooling to drive.
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

	b, err := blueprint.Load(c.Blueprint)
	if err != nil {
		return err
	}

	store := cache.New(c.CacheDir)

	// INVARIANT, carried from Task 6's review (this is a requirement, not a
	// style preference): Options.Index and Options.Store MUST be built from
	// the SAME CRD load, over exactly the providers named in the blueprint's
	// spec.sources.
	//
	// internal/api's own tests deliberately diverge the two -- testIndex and
	// the Store testHandlerWithPath seeds cover different CRD shapes for the
	// same provider ref (see internal/api/server_test.go's
	// testGenerateFixtureCRDs comment) -- and that is fine there, because it
	// is the only way to exercise /api/kinds and /api/generate against
	// independently-crafted fixtures without one test file's needs
	// contorting the other's. It is NOT fine here: a production server whose
	// index (what /api/kinds shows the canvas) disagrees with its store
	// (what /api/generate actually reads when rendering) could advertise a
	// field the browser lets someone add and then fail, or silently render
	// against a different schema than the one just browsed. So: load each
	// source provider's CRDs from the store exactly once into byProvider,
	// build the index from that same map, and hand api.New that same store
	// instance -- never a second, independently-loaded map or store for
	// either side.
	// refs doubles as Options.Providers: the exact provider set the index is
	// built over, in blueprint-source order, deduplicated the same way the
	// byProvider map inherently is -- so GET /api/providers lists precisely
	// what /api/kinds serves from, never a second, independently-derived set.
	byProvider := make(map[string][]schema.CRD, len(b.Spec.Sources))
	refs := make([]string, 0, len(b.Spec.Sources))
	for _, s := range b.Spec.Sources {
		if _, ok := byProvider[s.Provider]; ok {
			continue // a duplicate source entry names the same load
		}
		crds, err := store.Load(s.Provider)
		if err != nil {
			// cache.Store.Load's own error already names the exact command
			// to run ("provider %q is not in the cache; run: cf provider
			// add %s") -- returning it unwrapped keeps that message intact
			// rather than presenting an empty index with no explanation.
			return err
		}
		byProvider[s.Provider] = crds
		refs = append(refs, s.Provider)
	}

	// The vendored native Kubernetes kinds are always in the index, under
	// their own provider label — no source entry names them (they are
	// compiled into the binary, pinned to one Kubernetes version) and no
	// blueprint opts into them. They deliberately do NOT join refs:
	// GET /api/providers lists xpkg packages with digests and cache entries,
	// and native kinds have neither — /api/kinds is where they surface,
	// wearing provider "k8s".
	native, err := k8s.Kinds()
	if err != nil {
		return err
	}
	byProvider[blueprint.NativeProvider] = native

	idx, err := index.Build(byProvider)
	if err != nil {
		return err
	}

	handler, err := api.New(api.Options{
		Index:     idx,
		Store:     store,
		Blueprint: c.Blueprint,
		OutDir:    c.Out,
		Lock:      c.Lock,
		Providers: refs,
	})
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

	srv := &http.Server{Handler: withRoot(handler)}

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

// noUIMessage is served at exactly "/" in M2. It is plain text, not JSON,
// because it is meant for a human opening the URL cf serve just printed in a
// browser -- every /api/... response stays JSON, for API clients.
const noUIMessage = "compositionfactory API is up.\n\n" +
	"There is no UI here yet: the canvas arrives with the frontend milestone (M3).\n" +
	"See GET /api/kinds, GET /api/blueprint and POST /api/generate.\n"

// withRoot adds a plain "the API is up" response at exactly "/", in front of
// api.
//
// It must match exactly "/" -- Go 1.22+ ServeMux's "GET /{$}" pattern, not a
// bare catch-all "/" -- because api's own mux deliberately registers no "/"
// pattern of its own (see internal/api/server.go's comment on why: a
// catch-all "/" registered ALONGSIDE more specific patterns in the same mux
// defeats ServeMux's built-in 404-vs-405 handling for those patterns). This
// is a different, outer mux wrapping api as one opaque handler: it holds
// exactly one specific pattern (root only) and one true catch-all that
// forwards every other path, completely unmodified, to api -- which then
// performs its own complete routing, 404s and 405s included, on it. Nothing
// here re-registers or shadows any of api's own routes.
func withRoot(api http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, noUIMessage)
	})
	mux.Handle("/", api)
	return mux
}
