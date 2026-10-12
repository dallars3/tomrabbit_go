// Package servlet defines the extension point of the TomRabbit container:
// a Servlet is a request-handling component deployed inside a Context.
//
// Per ADR-0001 a Servlet is a compile-time-registered singleton. An
// implementation must be safe to invoke concurrently.
package servlet

import (
	"net/http"

	"tomrabbit/internal/session"
)

// Request is what a Servlet receives. It embeds the standard library request
// (per ADR-0002 net/http owns wire details) and adds the caller's Session.
type Request struct {
	*http.Request
	Session *session.Session
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
