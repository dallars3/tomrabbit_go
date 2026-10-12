// Package pipeline implements the Wrapper pipeline: the ordered chain of
// Wrappers a request crosses inside one Context.
//
// The Java TomRabbit hand-linked Wrapper nodes and its append routine dropped
// every node beyond the second, and the tail node called a nil next, producing
// an NPE instead of 404. The Go version is a slice walked by a range: no
// append bug, and a miss is a false return the container turns into 404.
package pipeline

import (
	"sort"

	"tomrabbit/internal/servlet"
)

// Wrapper associates one Servlet with the exact URL Pattern it answers.
type Wrapper struct {
	Pattern string
	Servlet servlet.Servlet
}

// Chain is an ordered set of Wrappers.
type Chain []Wrapper

// BuildChain turns a pattern->servlet map into a deterministic chain sorted by
// pattern, so request handling is reproducible across processes.
func BuildChain(mappings map[string]servlet.Servlet) Chain {
	patterns := make([]string, 0, len(mappings))
	for p := range mappings {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)

	chain := make(Chain, 0, len(patterns))
	for _, p := range patterns {
		chain = append(chain, Wrapper{Pattern: p, Servlet: mappings[p]})
	}
	return chain
}

// Invoke walks the chain and runs the first Wrapper whose Pattern equals the
// in-context request path. It reports whether a Wrapper handled the request;
// false means the container must answer 404.
func (c Chain) Invoke(inContextPath string, resp *servlet.Response, req *servlet.Request) bool {
	for i := range c {
		if c[i].Pattern == inContextPath {
			c[i].Servlet.Service(resp, req)
			return true
		}
	}
	return false
}
