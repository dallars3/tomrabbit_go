// Package container is TomRabbit's kernel: it owns the deployed Contexts and
// routes every HTTP request to a Static Resource or a Servlet, attaching the
// caller's Session along the way.
//
// It is a plain http.Handler (ADR-0002): net/http owns connection handling
// and wire parsing, the container owns only Context/Wrapper/Pipeline routing.
package container

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"tomrabbit/internal/pipeline"
	"tomrabbit/internal/registry"
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

// New snapshots the registry into Contexts. Registration happens in package
// init functions (ADR-0001), so by startup the registry is complete.
func New(webappsRoot string, reg *registry.Registry, sessions *session.Store) *Container {
	c := &Container{
		contexts: make(map[string]*Context),
		sessions: sessions,
	}
	for _, name := range reg.Contexts() {
		c.contexts[name] = &Context{
			Name:      name,
			StaticDir: filepath.Join(webappsRoot, filepath.FromSlash(name)),
			Wrappers:  pipeline.BuildChain(reg.Mappings(name)),
		}
	}
	return c
}

// ServeHTTP implements http.Handler.
func (c *Container) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, rest := splitContextPath(r.URL.Path)
	ctx, ok := c.contexts[name]
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess := c.resolveSession(w, r)

	// Static resources win over Servlets, mirroring the Java version's
	// file-exists check but with safe paths, real status codes and types.
	if serveStatic(w, r, ctx.StaticDir, rest) {
		return
	}

	req := &servlet.Request{Request: r, Session: sess}
	resp := &servlet.Response{ResponseWriter: w}
	if ctx.Wrappers.Invoke(rest, resp, req) {
		return
	}
	http.NotFound(w, r)
}

// resolveSession returns the caller's existing, non-expired Session and slides
// its deadline; otherwise it creates one and issues the cookie.
func (c *Container) resolveSession(w http.ResponseWriter, r *http.Request) *session.Session {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if sess, ok := c.sessions.Get(cookie.Value); ok {
			c.sessions.Touch(sess)
			return sess
		}
	}
	sess := c.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return sess
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
