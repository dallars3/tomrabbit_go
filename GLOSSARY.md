# Glossary

Language shared between humans and agents working on TomRabbit. Terms describe the
domain only, not implementation.

## Deployment

- **Webapp** — One deployed application, identified by a Context name. Owns a set
  of Servlets and a directory of Static Resources.
- **Context** — The URL namespace a Webapp occupies: every path beginning with
  `/<context-name>/`. Two webapps never share a context name.
- **Static Resource** — A file in the Webapp's directory (such as `1.html`) served
  verbatim when the request path names an existing file. Checked before Servlets.
- **Script Servlet** — A Servlet deployed as interpreted source (a `*.script`
  file) in the Webapp's directory rather than compiled into the server. Parsed
  once at startup and re-executed per request; an exception to compile-time
  deployment, scoped by ADR-0003.

## Request handling

- **Servlet** — A request-handling component exposing a single service operation.
  One instance exists for the life of the server; it must be safe for concurrent
  requests.
- **Wrapper** — Associates one Servlet with one URL Pattern inside a Context.
- **URL Pattern** — The exact in-context path (for example `/ccc`) a Wrapper
  matches. Matching is exact; wildcard patterns do not exist.
- **Pipeline** — The ordered chain a request traverses on its way to a handler.
  TomRabbit has two levels: the chain of Contexts, and within the matched Context,
  the chain of Wrappers.
- **Valve** — One node in a Pipeline. A valve either handles the request or passes
  it to the next valve; the chain ends with an explicit not-found answer.

## Sessions

- **Session** — Server-side state attributed to one client across multiple
  requests, accessed only through the cookie that identifies it.
- **JSESSIONID** — The cookie carrying a Session's identifier. Absence or expiry
  of the cookie means a new Session is started, but only on demand: a Session
  is created when a Servlet calls `Request.GetSession()`, never for static
  resources, unmatched paths, or Servlets that do not use it.
- **Sliding Expiry** — A Session is discarded after a period of idleness (time
  since its last request), not after a fixed lifetime. Only a request that uses
  the Session (`Store.Open`, i.e. a Servlet reading it) resets the deadline; a
  plain lookup (`Store.Get`) and static requests do not.
