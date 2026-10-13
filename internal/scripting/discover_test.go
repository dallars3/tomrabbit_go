package scripting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsScriptPath(t *testing.T) {
	cases := map[string]bool{
		"hello.script":    true,
		"x.SCRIPT":        true,
		"sub/deep.script": true,
		"index.html":      false,
		"script.txt":      false,
		"notascript.html": false,
	}
	for in, want := range cases {
		if got := IsScriptPath(in); got != want {
			t.Errorf("IsScriptPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDiscoverCompilesAndMaps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.script"),
		[]byte(`print("hi");`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"),
		[]byte(`not a script`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Discover(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("discovered %d servlets, want 1: %v", len(m), m)
	}
	if _, ok := m["/hello.script"]; !ok {
		t.Fatalf("mapping key = %v, want /hello.script", m)
	}
}

func TestDiscoverMissingDirIsEmpty(t *testing.T) {
	m, err := Discover(filepath.Join(t.TempDir(), "does-not-exist"), time.Second)
	if err != nil {
		t.Fatalf("missing dir must not be an error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("got %d mappings, want 0", len(m))
	}
}

func TestDiscoverSyntaxErrorAbortsWithFileName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.script"),
		[]byte(`print(;`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(dir, time.Second)
	if err == nil {
		t.Fatal("syntax error must abort discovery")
	}
	if !strings.Contains(err.Error(), "broken.script") {
		t.Fatalf("error must name the offending file: %v", err)
	}
}

func TestDiscoverRejectsNestedDirs(t *testing.T) {
	dir := t.TempDir()
	// Only the first level of a context dir deploys; nested directories are
	// static resource territory and must not be scanned for scripts.
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.script"),
		[]byte(`print("x");`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Discover(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("nested script must not be deployed, got %v", m)
	}
}
