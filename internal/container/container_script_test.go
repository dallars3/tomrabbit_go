package container

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tomrabbit/internal/registry"
	"tomrabbit/internal/session"
)

const demoScript = `
setHeader("X-Script", "works");
print("method=" + method());
print("name=" + query("name"));
`

func newScriptContainer(t *testing.T, files map[string]string, reg *registry.Registry) (*Container, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, "a", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := session.NewStore(30 * time.Minute)
	c, err := New(root, reg, store)
	if err != nil {
		t.Fatal(err)
	}
	return c, root
}

func TestScriptServletExecutesOverHTTP(t *testing.T) {
	reg := registry.New()
	reg.Register("a", "/__go", bodyServlet("go")) // context must exist from a Go registration
	c, _ := newScriptContainer(t, map[string]string{"hello.script": demoScript}, reg)
	srv := httptest.NewServer(c)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/a/hello.script?name=go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Script") != "works" {
		t.Fatalf("X-Script = %q", resp.Header.Get("X-Script"))
	}
	want := "method=GET\nname=go\n"
	if string(body) != want {
		t.Fatalf("body = %q, want %q", string(body), want)
	}
}

func TestScriptSourceIsNeverServed(t *testing.T) {
	reg := registry.New()
	reg.Register("a", "/__go", bodyServlet("go"))
	c, _ := newScriptContainer(t, map[string]string{"hello.script": demoScript}, reg)
	rec := httptest.NewRecorder()
	// Direct handler invocation bypasses client-side URL normalization.
	c.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a/hello.script", nil))
	if strings.Contains(rec.Body.String(), "setHeader") ||
		strings.Contains(rec.Body.String(), "method()") {
		t.Fatalf("raw script source was served as a static file:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "name=") {
		t.Fatalf("script should have been executed, got: %q", rec.Body.String())
	}
}

func TestBrokenScriptAbortsStartup(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "bad.script"),
		[]byte(`print(;`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.Register("a", "/__go", bodyServlet("go"))
	_, err := New(root, reg, session.NewStore(time.Minute))
	if err == nil {
		t.Fatal("a syntax error in a deployed script must stop startup")
	}
	if !strings.Contains(err.Error(), "bad.script") {
		t.Fatalf("startup error must identify the file: %v", err)
	}
}

func TestScriptClashingWithGoServletPanicsAtStartup(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "dup.script"),
		[]byte(`print("x");`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.Register("a", "/dup.script", bodyServlet("go"))

	defer func() {
		if recover() == nil {
			t.Fatal("a script mapped onto an existing pattern must panic like any duplicate")
		}
	}()
	_, _ = New(root, reg, session.NewStore(time.Minute))
}
