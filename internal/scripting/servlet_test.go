package scripting

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tomrabbit/internal/servlet"
)

func runScript(t *testing.T, src string, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	s, err := Compile("test.script", src, time.Second)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rec := httptest.NewRecorder()
	s.Service(&servlet.Response{ResponseWriter: rec}, servlet.NewRequest(req, nil))
	return rec
}

func get(t *testing.T, target string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, target, nil)
}

func TestScriptBasicPrint(t *testing.T) {
	rec := runScript(t, `print("hello", "world");`, get(t, "/a/test.script"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "hello world\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestScriptRequestBuiltins(t *testing.T) {
	req := get(t, "/a/test.script?name=tom")
	req.Header.Set("X-Test", "yes")
	rec := runScript(t, `
print(method());
print(path());
print(query("name"));
print(header("X-Test"));
`, req)
	want := "GET\n/a/test.script\ntom\nyes\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestScriptMissingQueryIsEmptyString(t *testing.T) {
	rec := runScript(t, `print(query("absent"));`, get(t, "/"))
	if rec.Body.String() != "\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestScriptSetStatusAndHeader(t *testing.T) {
	src := `
setHeader("X-Greeting", "hi");
setStatus(201);
print("created");
`
	rec := runScript(t, src, get(t, "/"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if rec.Header().Get("X-Greeting") != "hi" {
		t.Fatalf("X-Greeting = %q", rec.Header().Get("X-Greeting"))
	}
}

func TestScriptRuntimeErrorIs500AndDiscardsBody(t *testing.T) {
	// print before the error must not be committed with a 200.
	rec := runScript(t, `
print("partial");
print(missingFn());
`, get(t, "/"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "partial") {
		t.Fatalf("buffered output leaked into error response: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown function") {
		t.Fatalf("error response should explain the failure: %q", rec.Body.String())
	}
}

func TestScriptBuiltinTypeError(t *testing.T) {
	rec := runScript(t, `setStatus("nope");`, get(t, "/"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestScriptTimeoutKillsInfiniteLoop(t *testing.T) {
	s, err := Compile("loop.script", `while (true) {}`, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.Service(&servlet.Response{ResponseWriter: rec}, servlet.NewRequest(get(t, "/"), nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("infinite loop was not interrupted")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestConcurrentRequestsShareNothing(t *testing.T) {
	s, err := Compile("c.script", `
int n;
n = n + 1;
print(n);
`, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.Service(&servlet.Response{ResponseWriter: rec}, servlet.NewRequest(get(t, "/"), nil))
			results <- rec.Body.String()
		}()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r != "1\n" {
			t.Fatalf("request leaked script state from another request: %q", r)
		}
	}
}
