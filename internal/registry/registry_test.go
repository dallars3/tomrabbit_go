package registry

import (
	"testing"

	"tomrabbit/internal/servlet"
)

type stubServlet struct{ tag string }

func (s *stubServlet) Service(*servlet.Response, *servlet.Request) {}

func TestRegisterAndLookup(t *testing.T) {
	reg := New()
	s := &stubServlet{tag: "one"}
	reg.Register("a", "/ccc", s)

	got, ok := reg.Lookup("a", "/ccc")
	if !ok {
		t.Fatal("expected servlet to be found after registration")
	}
	if got != servlet.Servlet(s) {
		t.Fatal("lookup returned a different servlet instance")
	}

	if _, ok := reg.Lookup("a", "/nope"); ok {
		t.Fatal("unexpected servlet for an unmapped pattern")
	}
	if _, ok := reg.Lookup("b", "/ccc"); ok {
		t.Fatal("pattern must not leak across contexts")
	}
}

func TestRegisterRejectsBlankKeysAndNil(t *testing.T) {
	reg := New()
	s := &stubServlet{}

	cases := []struct {
		context string
		pattern string
		servlet servlet.Servlet
	}{
		{"", "/ccc", s},
		{"a", "", s},
		{"a", "ccc", s},    // pattern must be rooted
		{"a", "/ccc", nil}, // nil servlet
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for context=%q pattern=%q nil=%v", c.context, c.pattern, c.servlet == nil)
				}
			}()
			reg.Register(c.context, c.pattern, c.servlet)
		}()
	}
}

func TestRegisterRejectsDuplicateMapping(t *testing.T) {
	reg := New()
	reg.Register("a", "/ccc", &stubServlet{tag: "first"})

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate (context, pattern) must panic instead of silently overwriting")
		}
	}()
	reg.Register("a", "/ccc", &stubServlet{tag: "second"})
}

func TestContextsAndMappings(t *testing.T) {
	reg := New()
	reg.Register("b", "/x", &stubServlet{})
	reg.Register("a", "/x", &stubServlet{})
	reg.Register("a", "/y", &stubServlet{})

	// Enumeration order is deterministic (sorted), so chain construction is
	// reproducible across processes.
	if got := reg.Contexts(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("contexts = %v, want [a b]", got)
	}

	m := reg.Mappings("a")
	if len(m) != 2 || m["/x"] == nil || m["/y"] == nil {
		t.Fatalf("mappings for a = %v", m)
	}
	if reg.Mappings("missing") != nil {
		t.Fatal("unknown context should return nil, not a non-nil empty map")
	}
}
