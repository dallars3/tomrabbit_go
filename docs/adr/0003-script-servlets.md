# 0003 — Script servlets: boot-time discovery for interpreted pages (Amends 0001)

ADR-0001 requires every Servlet to be compiled into the server binary: a Go
type registered from a webapp package's `init()`. That gives startup-time
validation and a single deployable, at the accepted cost that adding a
Servlet requires rebuilding.

A separate, pure-Go tree-walking interpreter (interpreter-go) now exists and
can be linked as an ordinary Go library. We want pages written in its
C-like language deployed as source files, without rebuilding the server.

We considered:

- **Resolving scripts per request (the early-PHP model):** read and parse the
  file named by the URL when a request arrives. Maximally dynamic, but every
  conflict, missing file and syntax error is discovered only by the client
  that hits that URL, and every request pays disk + parse cost. Rejected.
- **Go plugins / an external process:** already rejected in 0001; they still
  do not fit a single-binary teaching server.
- **Boot-time discovery:** scan each context directory once at startup, parse
  every `*.script` file, and register the parsed program wrapped in a
  `ScriptServlet`. Chosen.

Decision: **at startup the container scans one level of every context
directory for `*.script` files, parses them once, and registers each as a
singleton Servlet at `/<filename>`.** Requests never touch the disk or the
parser: the immutable AST is shared, while a fresh interpreter, variable
environment, and response buffer are created per request, so concurrent
requests share nothing.

- The embedded interpreter stays HTTP-agnostic. All HTTP contact lives in one
  anti-corruption package, `internal/scripting` — the only package importing
  the interpreter modules. It exposes request data through builtins the
  language gained for this purpose (`method()`, `path()`, `query()`,
  `header()`) and records response intent (`setStatus()`, `setHeader()`).
- Response headers and status are buffered until the script completes
  successfully; a runtime error discards partial output and returns 500.
- Each execution runs under a timeout (default 2s); the interpreter checks the
  context at loop iteration boundaries, so an infinite `while` cannot pin a
  goroutine.
- Script source is never served by the Static Resource servlet.

Consequences:

- Adding or changing an interpreted Servlet no longer requires a rebuild — a
  restart is enough. This is the deliberate, scoped exception to ADR-0001;
  Go servlets remain compile-time registered exactly as before.
- What compile-time type checking used to catch is now caught at **startup**:
  a syntax error in any script fails startup with the file name, and duplicate
  URL patterns panic through the same registry rule as Go servlets. Runtime
  type errors surface per request as 500.
- The interpreter gained first-class function-call syntax and a host-builtin
  registration API, even though the language has no user-defined functions; a
  script can only call builtins provided by the host.
- Discovery only covers contexts that already exist because a Go webapp
  package registered them; there are no script-only contexts.
