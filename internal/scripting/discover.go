// Package scripting is the anti-corruption layer between tomrabbit and the
// embedded interpreter-go language: it is the ONLY package allowed to import
// the interpreter modules. The rest of the server only ever sees ordinary
// servlet.Servlet values produced here (ADR-0003).
package scripting

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"interpreter/lexer"
	"interpreter/parser"

	"tomrabbit/internal/servlet"
)

// ScriptExt is the deployed-script file extension. A file named "hello.script"
// inside a context directory is served at "/<context>/hello.script".
const ScriptExt = ".script"

// DefaultRunTimeout bounds one script execution, so an infinite while loop
// cannot pin a goroutine forever.
const DefaultRunTimeout = 2 * time.Second

// IsScriptPath reports whether an in-context relative path names a script.
// The container uses it to keep script files out of the static-file path.
func IsScriptPath(rel string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.ToSlash(rel)), ScriptExt)
}

// Discover scans ONE level of dir for *.script files, lexes and parses each
// file exactly once at startup, and returns URL-pattern -> Servlet mappings
// (the pattern is "/" plus the file name, e.g. "/hello.script"). Parsed ASTs
// are cached inside the servlets; request time never touches the disk or the
// parser. A syntax error in any script aborts startup with a file-prefixed
// error, preserving ADR-0001's "fail loudly before serving traffic" spirit.
func Discover(dir string, runTimeout time.Duration) (map[string]servlet.Servlet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]servlet.Servlet{}, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ScriptExt) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // deterministic duplicate-panic ordering

	out := make(map[string]servlet.Servlet, len(names))
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		s, err := Compile(name, string(src), runTimeout)
		if err != nil {
			return nil, err
		}
		out["/"+filepath.ToSlash(name)] = s
	}
	return out, nil
}

// Compile parses one in-memory script into a runnable servlet. It exists for
// tests and for callers that already hold source instead of a file.
func Compile(name, src string, runTimeout time.Duration) (*ScriptServlet, error) {
	toks, err := lexer.Tokenize(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	program, err := parser.Parse(toks)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return NewScriptServlet(name, program, runTimeout), nil
}
