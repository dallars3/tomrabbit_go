// Package connector is the TomRabbit network endpoint: it binds a port and
// hands each accepted connection to net/http, which runs one goroutine per
// connection (the Go equivalent of the Java version's thread pool).
package connector

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Connector wraps an http.Server.
type Connector struct {
	addr   string
	server *http.Server
}

// New creates a Connector listening on addr (for example ":8090") and serving
// handler.
func New(addr string, handler http.Handler) *Connector {
	return &Connector{
		addr: addr,
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
}

// Run serves until the context is cancelled, then shuts down gracefully. It
// returns nil on a clean shutdown.
func (c *Connector) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errCh:
		return err
	}
}
