// Package servlet defines the extension point of the TomRabbit container:
// a Servlet is a request-handling component deployed inside a Context.
//
// Per ADR-0001 a Servlet is a compile-time-registered singleton. An
// implementation must be safe to invoke concurrently.
package servlet

import (
	"net/http"
	"sync"

	"tomrabbit/internal/session"
)

// Request is what a Servlet receives. It embeds the standard library request
// (per ADR-0002 net/http owns wire details). A Session is not attached
// eagerly: GetSession starts one only when the Servlet asks for it, so static
// resources and unmatched paths never create sessions.
type Request struct {
	*http.Request
	ensureSession func() *session.Session

	// once makes the first GetSession call resolve the session and every
	// later call return the cached result. This is safe even when a Servlet
	// fans out goroutines that call GetSession concurrently: callers block on
	// the first resolution and then share one Session, and the provider runs
	// exactly once.
	once sync.Once
	sess *session.Session
}

// NewRequest wraps a standard request. ensureSession resolves the caller's
// session on demand (returning the existing one or starting a new session and
// issuing the cookie). It is invoked at most once; pass nil only in tests that
// never call GetSession.
func NewRequest(r *http.Request, ensureSession func() *session.Session) *Request {
	return &Request{Request: r, ensureSession: ensureSession}
}

// GetSession returns the caller's session, creating one (and issuing
// JSESSIONID) on first use, mirroring HttpServletRequest.getSession(). It is
// safe to call concurrently from goroutines spawned while handling one
// request; all calls return the same instance.
//
// It returns nil in two cases: the Request was built without a session
// provider (test use), or the response had already been committed on a
// request that carried no valid session cookie — a new identifier could no
// longer be delivered, so no unreachable session is stored.
func (r *Request) GetSession() *session.Session {
	if r.ensureSession == nil {
		return nil
	}
	r.once.Do(func() { r.sess = r.ensureSession() })
	return r.sess
}

// Response is what a Servlet writes to. It embeds the standard library
// ResponseWriter, so Servlets use ordinary http.ResponseWriter methods.
type Response struct {
	http.ResponseWriter
}

// Servlet handles one matched request. The URL Pattern has already been
// matched by the time Service is called.
type Servlet interface {
	Service(resp *Response, req *Request)
}

// ServletFunc adapts an ordinary function into a Servlet.
type ServletFunc func(resp *Response, req *Request)

// Service implements Servlet.
func (f ServletFunc) Service(resp *Response, req *Request) {
	f(resp, req)
}
