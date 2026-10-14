// Package container is TomRabbit's kernel: it owns the deployed Contexts and
// routes every HTTP request to a Static Resource or a Servlet, resolving the
// caller's Session lazily only when a Servlet asks for it.
//
// It is a plain http.Handler (ADR-0002): net/http owns connection handling
// and wire parsing, the container owns only Context/Wrapper/Pipeline routing.
package container

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"tomrabbit/internal/pipeline"
	"tomrabbit/internal/registry"
	"tomrabbit/internal/scripting"
	"tomrabbit/internal/servlet"
	"tomrabbit/internal/session"
)

// SessionCookieName is the cookie carrying the session identifier, kept named
// after the Java servlet convention.
const SessionCookieName = "JSESSIONID"

// Context is one deployed Webapp: its static-file directory and its Wrapper
// pipeline.
type Context struct {
	Name      string
	StaticDir string
	Wrappers  pipeline.Chain
}

// Container routes requests across all deployed Contexts.
type Container struct {
	contexts map[string]*Context
	sessions *session.Store
}

// New snapshots the registry into Contexts. Go-servlet registration happens
// in package init functions (ADR-0001); interpreted *.script servlets are
// discovered from each context directory here (ADR-0003). A script parse
// error aborts startup with a file-prefixed error; a script mapped onto an
// existing pattern panics via registry.Register, like any duplicate mapping.
func New(webappsRoot string, reg *registry.Registry, sessions *session.Store) (*Container, error) {
	c := &Container{
		contexts: make(map[string]*Context),
		sessions: sessions,
	}
	for _, name := range reg.Contexts() {
		ctxDir := filepath.Join(webappsRoot, filepath.FromSlash(name))
		scripts, err := scripting.Discover(ctxDir, scripting.DefaultRunTimeout)
		if err != nil {
			return nil, fmt.Errorf("context %s: %w", name, err)
		}
		for pattern, serv := range scripts {
			reg.Register(name, pattern, serv)
		}
		c.contexts[name] = &Context{
			Name:      name,
			StaticDir: ctxDir,
			Wrappers:  pipeline.BuildChain(reg.Mappings(name)),
		}
	}
	return c, nil
}

// trackedWriter records whether the response headers have already been
// committed. Once committed, adding a Set-Cookie header is a silent no-op, so
// the container must not mint a session it could no longer identify to the
// client. It preserves http.Flusher so static range/streaming responses keep
// working.
type trackedWriter struct {
	http.ResponseWriter
	committed atomic.Bool
}

func (w *trackedWriter) WriteHeader(code int) {
	w.committed.Store(true)
	w.ResponseWriter.WriteHeader(code)
}

func (w *trackedWriter) Write(p []byte) (int, error) {
	// The first Write implicitly commits status 200.
	w.committed.Store(true)
	return w.ResponseWriter.Write(p)
}

func (w *trackedWriter) Flush() {
	// Flushing also commits the headers.
	w.committed.Store(true)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ServeHTTP implements http.Handler.
func (c *Container) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, rest := splitContextPath(r.URL.Path)
	ctx, ok := c.contexts[name]
	if !ok {
		http.NotFound(w, r)
		return
	}

	tw := &trackedWriter{ResponseWriter: w}

	// A session starts only when a Servlet asks for it (via req.GetSession).
	// Static resources and 404s must not mint sessions: otherwise crawlers
	// hitting static files would grow the session table without bound.
	// Request memoizes the result with sync.Once, so this runs at most once
	// even when the Servlet fans out concurrent GetSession calls.
	ensureSession := func() *session.Session {
		if cookie, err := r.Cookie(SessionCookieName); err == nil {
			// Open looks up and slides the idle deadline in one session-locked
			// step. The id already belongs to the client, so this session
			// stays reachable even if the response happens to be committed.
			if existing, live := c.sessions.Open(cookie.Value); live {
				return existing
			}
		}
		if tw.committed.Load() {
			// Headers are already on the wire: a brand-new JSESSIONID could
			// never be delivered, so any stored state would be unreachable.
			// Return nil instead of leaving an orphaned session row.
			return nil
		}
		sess := c.sessions.Create()
		http.SetCookie(tw, &http.Cookie{
			Name:     SessionCookieName,
			Value:    sess.ID(),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		return sess
	}

	// Static resources win over Servlets, mirroring the Java version's
	// file-exists check but with safe paths, real status codes and types.
	if serveStatic(tw, r, ctx.StaticDir, rest) {
		return
	}

	req := servlet.NewRequest(r, ensureSession)
	resp := &servlet.Response{ResponseWriter: tw}
	if ctx.Wrappers.Invoke(rest, resp, req) {
		return
	}
	http.NotFound(tw, r)
}

// splitContextPath divides "/a/ccc" into ("a", "/ccc"). With no in-context
// remainder the second result is "/".
func splitContextPath(urlPath string) (string, string) {
	trimmed := strings.TrimPrefix(urlPath, "/")
	name, rest, found := strings.Cut(trimmed, "/")
	if !found || rest == "" {
		return name, "/"
	}
	return name, "/" + rest
}

// serveStatic serves a file under dir for the in-context URL path. It returns
// false (serving nothing) when the path escapes the directory, names a
// directory, or does not exist, so the caller falls through to Servlets and
// then 404.
func serveStatic(w http.ResponseWriter, r *http.Request, dir, urlPath string) bool {
	// URL paths use slash; collapse to a clean OS-relative path. Leading "/"
	// plus Clean is the same containment trick net/http's FileServer uses.
	rel := filepath.Clean("/" + filepath.FromSlash(urlPath))
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	if rel == "" || rel == "." {
		rel = "index.html"
	}
	if scripting.IsScriptPath(rel) {
		// Deployed scripts execute via ScriptServlet; never serve their source.
		return false
	}

	full := filepath.Join(dir, rel)
	if !inside(dir, full) {
		return false
	}

	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	// ServeContent derives Content-Type from the extension, handles
	// Last-Modified and range requests, and sets Content-Length. *os.File
	// satisfies io.ReadSeeker.
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return true
}

// inside reports whether full is located at or beneath dir.
func inside(dir, full string) bool {
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../")
}
