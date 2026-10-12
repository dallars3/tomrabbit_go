package container

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tomrabbit/internal/registry"
	"tomrabbit/internal/servlet"
	"tomrabbit/internal/session"
)

// ---- test servlets ---------------------------------------------------------

type visitServlet struct{}

func (visitServlet) Service(resp *servlet.Response, req *servlet.Request) {
	n := 0
	if v, ok := req.Session.Get("visits"); ok {
		n = v.(int)
	}
	n++
	req.Session.Set("visits", n)
	name := req.URL.Query().Get("name")
	if name == "" {
		name = "world"
	}
	fmt.Fprintf(resp, "Hello, %s! visits=%d", name, n)
}

type bodyServlet string

func (b bodyServlet) Service(resp *servlet.Response, _ *servlet.Request) {
	_, _ = resp.Write([]byte(b))
}

// ---- fixtures --------------------------------------------------------------

type fixture struct {
	server *httptest.Server
	store  *session.Store
	clock  *fakeClock
	reg    *registry.Registry
	root   string
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newFixture(t *testing.T) *fixture {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "1.html"), []byte("<h1>tomrabbit</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := registry.New()
	reg.Register("a", "/ccc", visitServlet{})
	// Three wrappers: the Java chain dropped the third node.
	reg.Register("a", "/c1", bodyServlet("one"))
	reg.Register("a", "/c2", bodyServlet("two"))
	reg.Register("a", "/c3", bodyServlet("three"))

	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	store := session.NewStoreWithClock(30*time.Minute, clock.now)

	c := New(root, reg, store)
	return &fixture{
		server: httptest.NewServer(c),
		store:  store,
		clock:  clock,
		reg:    reg,
		root:   root,
	}
}

func (f *fixture) close() { f.server.Close() }

func (f *fixture) get(t *testing.T, client *http.Client, path string) *http.Response {
	t.Helper()
	resp, err := client.Get(f.server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

func sessionCookie(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "JSESSIONID" {
			return c.Value
		}
	}
	return ""
}

// ---- tests -----------------------------------------------------------------

func TestServletRoutingWithParameters(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	resp := f.get(t, &http.Client{}, "/a/ccc?name=fredy")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := readBody(t, resp); body != "Hello, fredy! visits=1" {
		t.Fatalf("body = %q", body)
	}
}

func TestSessionCookieIssuedAndReused(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	first := f.get(t, client, "/a/ccc")
	id1 := sessionCookie(first)
	if id1 == "" {
		t.Fatal("first response must set JSESSIONID")
	}
	readBody(t, first)

	second := f.get(t, client, "/a/ccc")
	if id2 := sessionCookie(second); id2 != "" {
		t.Fatalf("valid cookie must not be reissued, got %q", id2)
	}
	if body := readBody(t, second); body != "Hello, world! visits=2" {
		t.Fatalf("session did not carry state: body = %q", body)
	}
}

func TestThirdWrapperIsReachable(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	client := &http.Client{}
	for _, c := range []struct {
		path string
		want string
	}{
		{"/a/c1", "one"},
		{"/a/c2", "two"},
		{"/a/c3", "three"},
	} {
		resp := f.get(t, client, c.path)
		if body := readBody(t, resp); body != c.want {
			t.Errorf("%s: body = %q, want %q", c.path, body, c.want)
		}
	}
}

func TestUnknownContextAndPatternAre404(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	client := &http.Client{}
	for _, path := range []string{"/nope/xxx", "/a/unknown", "/"} {
		resp := f.get(t, client, path)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%q: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestStaticFileServedWithContentType(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	resp := f.get(t, &http.Client{}, "/a/1.html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if body := readBody(t, resp); !strings.Contains(body, "tomrabbit") {
		t.Fatalf("body = %q", body)
	}
}

func TestPathTraversalIsRejected(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	// Drive the handler directly so the raw traversal path survives (an
	// http.Client would clean the URL before sending).
	c := New(f.root, f.reg, f.store)
	for _, evil := range []string{
		"/a/../../../../etc/passwd",
		"/a/../secret.txt",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", evil, nil)
		c.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%q: status = %d, want 404", evil, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "root:") {
			t.Errorf("%q: traversal leaked host file contents", evil)
		}
	}
}

func TestUnrelatedCookiesAreTolerated(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	// The Java parser indexed the cookie list looking for an exact-name entry
	// and crashed on any other cookie. Extra cookies must be harmless here.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/a/ccc", nil)
	req.Header.Set("Cookie", "foo=bar; baz=qux")
	New(f.root, f.reg, f.store).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if id := sessionCookie(rec.Result()); id == "" {
		t.Fatal("new visitor with stray cookies must still get a session")
	}
}

func TestExpiredCookieStartsNewSession(t *testing.T) {
	f := newFixture(t)
	defer f.close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	first := f.get(t, client, "/a/ccc")
	oldID := sessionCookie(first)
	readBody(t, first)

	// Advance past the idle TTL, then reuse the stale cookie.
	f.clock.t = f.clock.t.Add(31 * time.Minute)
	second := f.get(t, client, "/a/ccc")
	newID := sessionCookie(second)
	if newID == "" || newID == oldID {
		t.Fatalf("expired cookie must be replaced by a new session, old=%q new=%q", oldID, newID)
	}
	if body := readBody(t, second); body != "Hello, world! visits=1" {
		t.Fatalf("expired session should restart at visits=1, body = %q", body)
	}
}
