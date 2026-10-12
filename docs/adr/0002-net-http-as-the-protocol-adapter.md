# 0002 — Use net/http for the HTTP protocol layer

The Java TomRabbit hand-parsed request text: one blocking `read()`,
`split("\r\n")` for headers, `split(";")` / `split("=")` for cookies and
parameters. This caused truncated requests on TCP segmentation, crashes on
unexpected cookies, missing Content-Type/Content-Length, and no 404 path.

We considered keeping a hand-written parser and patching each defect. Instead,
the Go version uses the standard library (`net/http`, `net/http/httptest`):
one goroutine per connection, full request parsing, cookies, query parsing,
keep-alive, and content negotiation come from the standard library.

Decision: **net/http owns the wire protocol; TomRabbit owns only the container
kernel** — Context routing, the Wrapper pipeline, Static Resources, and
Sessions.

Consequences:

- The educational value moves from "parsing HTTP" to "how a servlet container
  routes a parsed request", which is the part worth studying.
- Protocol-level regressions are prevented by the standard library's tests;
  TomRabbit's own tests cover routing, sessions, static-file safety, and the
  pipeline end to end through `httptest`.
- The container is a plain `http.Handler`, so every standard-library middleware
  can be placed in front of it.
