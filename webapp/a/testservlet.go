// Package a is the example webapp deployed at context "/a". Its init()
// registers one Servlet at /ccc, the Go counterpart of the Java project's
// test/a/bin/wsTest/TestServlet mapped in web.xml.
package a

import (
	"fmt"

	"tomrabbit/internal/registry"
	"tomrabbit/internal/servlet"
)

func init() {
	registry.Register("a", "/ccc", servlet.ServletFunc(TestServlet))
}

// TestServlet greets the "name" query parameter and counts how many times the
// caller's session has visited.
func TestServlet(resp *servlet.Response, req *servlet.Request) {
	visits := 0
	if v, ok := req.Session.Get("visits"); ok {
		visits = v.(int)
	}
	visits++
	req.Session.Set("visits", visits)

	name := req.URL.Query().Get("name")
	if name == "" {
		name = "world"
	}
	fmt.Fprintf(resp, "Hello, %s! visits=%d", name, visits)
}
