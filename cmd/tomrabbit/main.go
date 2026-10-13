// Command tomrabbit starts the Go port of the TomRabbit servlet container.
//
// Webapp packages are compiled in as a blank import (ADR-0001); their init()
// functions populate the default registry before main runs.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "tomrabbit/webapp/a"

	"tomrabbit/internal/connector"
	"tomrabbit/internal/container"
	"tomrabbit/internal/registry"
	"tomrabbit/internal/session"
)

func main() {
	port := flag.Int("port", envInt("TOMRABBIT_PORT", 8090), "TCP port to listen on")
	webapps := flag.String("webapps", envOr("TOMRABBIT_WEBAPPS", "testdata/webapps"),
		"directory holding one subdirectory per context (static resources)")
	sessionTTL := flag.Duration("session-ttl", 30*time.Minute, "session idle timeout (sliding expiry)")
	flag.Parse()

	store := session.NewStore(*sessionTTL)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go store.StartCleanup(ctx, time.Minute)

	handler, err := container.New(*webapps, registry.Default(), store)
	if err != nil {
		log.Fatalf("tomrabbit failed to start: %v", err)
	}
	addr := ":" + strconv.Itoa(*port)
	conn := connector.New(addr, handler)

	log.Printf("tomrabbit listening on %s (webapps=%s, session-ttl=%s)", addr, *webapps, *sessionTTL)
	if err := conn.Run(ctx); err != nil {
		log.Fatalf("tomrabbit stopped with error: %v", err)
	}
	log.Print("tomrabbit stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
