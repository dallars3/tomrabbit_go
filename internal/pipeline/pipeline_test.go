package pipeline

import (
	"net/http/httptest"
	"testing"

	"tomrabbit/internal/servlet"
)

type marker struct{ name string }

func (m *marker) Service(resp *servlet.Response, req *servlet.Request) {
	_, _ = resp.Write([]byte(m.name))
}

func newReq(path string) *servlet.Request {
	return &servlet.Request{Request: httptest.NewRequest("GET", path, nil)}
}

func TestBuildIsSortedForDeterminism(t *testing.T) {
	chain := BuildChain(map[string]servlet.Servlet{
		"/z": &marker{"z"},
		"/a": &marker{"a"},
		"/m": &marker{"m"},
	})
	got := []string{chain[0].Pattern, chain[1].Pattern, chain[2].Pattern}
	want := []string{"/a", "/m", "/z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chain order = %v, want %v", got, want)
		}
	}
}

func TestInvokeReachesEveryNode(t *testing.T) {
	// Regression test for the Java version's hand-rolled linked list, which
	// dropped every node after the second: a chain of three must serve all
	// three patterns.
	chain := BuildChain(map[string]servlet.Servlet{
		"/c1": &marker{"one"},
		"/c2": &marker{"two"},
		"/c3": &marker{"three"},
	})

	for _, c := range []struct {
		path string
		body string
	}{
		{"/c1", "one"},
		{"/c2", "two"},
		{"/c3", "three"},
	} {
		rec := httptest.NewRecorder()
		ok := chain.Invoke(c.path, &servlet.Response{ResponseWriter: rec}, newReq(c.path))
		if !ok {
			t.Errorf("%s: pattern should have been matched", c.path)
			continue
		}
		if rec.Body.String() != c.body {
			t.Errorf("%s: body = %q, want %q", c.path, rec.Body.String(), c.body)
		}
	}
}

func TestInvokeReturnsFalseWhenNothingMatches(t *testing.T) {
	// Regression test for the Java version's NPE at the tail of the chain:
	// a miss returns false so the caller can answer 404.
	chain := BuildChain(map[string]servlet.Servlet{
		"/c1": &marker{"one"},
	})

	rec := httptest.NewRecorder()
	if ok := chain.Invoke("/missing", &servlet.Response{ResponseWriter: rec}, newReq("/missing")); ok {
		t.Fatal("unmatched path must report false instead of invoking a servlet")
	}
	if rec.Body.Len() != 0 {
		t.Fatal("nothing should have been written on a miss")
	}
}

func TestEmptyChainNeverMatches(t *testing.T) {
	rec := httptest.NewRecorder()
	if (Chain{}).Invoke("/anything", &servlet.Response{ResponseWriter: rec}, newReq("/anything")) {
		t.Fatal("empty chain cannot match anything")
	}
}
