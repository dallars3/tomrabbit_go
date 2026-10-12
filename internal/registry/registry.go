// Package registry is the compile-time deployment map for Servlets
// (ADR-0001). Webapp packages call Register from init(); the container reads
// the registry once at startup to build its pipelines.
package registry

import (
	"sort"
	"sync"

	"tomrabbit/internal/servlet"
)

// Registry maps (Context name, URL Pattern) pairs to Servlet singletons.
type Registry struct {
	mu       sync.RWMutex
	contexts map[string]map[string]servlet.Servlet
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{contexts: make(map[string]map[string]servlet.Servlet)}
}

// Register deploys s into contextName at pattern. Pattern must be an exact,
// rooted path like "/ccc". Duplicate mappings panic: two Servlets answering
// the same URL is a deployment error that should stop startup, not be decided
// at request time.
func (r *Registry) Register(contextName, pattern string, s servlet.Servlet) {
	if contextName == "" {
		panic("registry: context name must not be empty")
	}
	if pattern == "" || pattern[0] != '/' {
		panic("registry: URL pattern must be rooted and non-empty, got " + pattern)
	}
	if s == nil {
		panic("registry: servlet for " + contextName + pattern + " must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	wrappers, ok := r.contexts[contextName]
	if !ok {
		wrappers = make(map[string]servlet.Servlet)
		r.contexts[contextName] = wrappers
	}
	if _, exists := wrappers[pattern]; exists {
		panic("registry: duplicate mapping " + contextName + pattern)
	}
	wrappers[pattern] = s
}

// Lookup finds the Servlet registered at (contextName, pattern).
func (r *Registry) Lookup(contextName, pattern string) (servlet.Servlet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.contexts[contextName][pattern]
	return s, ok
}

// Contexts lists deployed context names in sorted order.
func (r *Registry) Contexts() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.contexts))
	for name := range r.contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Mappings returns a copy of one context's pattern->servlet map, or nil for an
// unknown context.
func (r *Registry) Mappings(contextName string) map[string]servlet.Servlet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.contexts[contextName]
	if !ok {
		return nil
	}
	dst := make(map[string]servlet.Servlet, len(src))
	for p, s := range src {
		dst[p] = s
	}
	return dst
}

// defaultRegistry backs the package-level Register used by webapp init()
// functions.
var defaultRegistry = New()

// Register installs into the default registry.
func Register(contextName, pattern string, s servlet.Servlet) {
	defaultRegistry.Register(contextName, pattern, s)
}

// Default returns the registry populated by package init() functions.
func Default() *Registry { return defaultRegistry }
