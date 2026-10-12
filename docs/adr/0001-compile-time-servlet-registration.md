# 0001 — Servlet deployment is compile-time registration, not runtime loading

The Java TomRabbit discovered Servlets at runtime by reflecting `.class` files
out of each webapp's `bin/` directory into a shared system `URLClassLoader`.
Go has no portable equivalent.

We considered:

- **`plugin.Open` (.so)** — closest to a class loader, but plugins are
  Linux/macOS only and must be compiled with the exact Go toolchain and exact
  dependency versions of the host binary. Fragile, operationally painful.
- **External processes reverse-proxied by the container** — fully dynamic, but
  multiplies processes, health checks, and failure modes for a teaching server.
- **Compile-time registration** — each webapp is a Go package that calls
  `Register(context, pattern, servlet)` from `init()`; the main binary imports
  the package. Deployment happens when the program is built.

Decision: **compile-time registration**. The registry is the single map from
(Context name, URL Pattern) to Servlet, built before the server starts.

Consequences:

- Adding a Servlet requires a rebuild — accepted; this project ships one binary.
- Servlet identity is statically typed, and duplicate mappings fail loudly at
  startup instead of silently at request time.
- Servlets are singletons by construction, fixing the Java version's
  per-request `newInstance()`.
