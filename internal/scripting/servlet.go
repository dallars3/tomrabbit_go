package scripting

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"interpreter/ast"
	"interpreter/interp"
	"interpreter/value"

	"tomrabbit/internal/servlet"
)

// ScriptServlet runs one pre-parsed script per request. It is a singleton:
// the cached AST is immutable, while each request gets a fresh interpreter,
// a fresh variable environment and a fresh response buffer, so concurrent
// requests share nothing.
type ScriptServlet struct {
	name    string
	program *ast.Program
	timeout time.Duration
}

// NewScriptServlet wraps a parsed program. timeout zero means DefaultRunTimeout.
func NewScriptServlet(name string, program *ast.Program, timeout time.Duration) *ScriptServlet {
	if program == nil {
		panic("scripting: script " + name + " has no program")
	}
	if timeout <= 0 {
		timeout = DefaultRunTimeout
	}
	return &ScriptServlet{name: name, program: program, timeout: timeout}
}

// Service implements servlet.Servlet.
func (s *ScriptServlet) Service(resp *servlet.Response, req *servlet.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), s.timeout)
	defer cancel()

	var body bytes.Buffer
	h := newScriptHost(req.Request)
	runner := interp.New(&body)
	registerBuiltins(runner, h)

	runErr := runner.RunContext(ctx, s.program)
	if runErr != nil {
		// Runtime error (including the timeout signal): discard buffered
		// output and answer 500, so partial prints never commit with a
		// success status.
		http.Error(resp, "script "+s.name+" failed: "+runErr.Error(), http.StatusInternalServerError)
		return
	}

	// Headers/status are collected while the script runs and only committed
	// after the full body succeeds, so scripts may call setHeader in any order.
	for k, vs := range h.header {
		for _, v := range vs {
			resp.Header().Add(k, v)
		}
	}
	if resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	status := h.status
	if status == 0 {
		status = http.StatusOK
	}
	resp.WriteHeader(status)
	_, _ = resp.Write(body.Bytes())
}

// scriptHost carries the request-side bindings and the response-side effects a
// script requests through builtins.
type scriptHost struct {
	req    *http.Request
	header http.Header
	status int
}

func newScriptHost(r *http.Request) *scriptHost {
	return &scriptHost{req: r, header: make(http.Header)}
}

// registerBuiltins projects the HTTP world into the language. Input functions
// expose request data; output functions record response intent. The language
// itself stays HTTP-agnostic — none of these names exist in interpreter-go.
func registerBuiltins(in *interp.Interp, h *scriptHost) {
	in.RegisterBuiltin("method", func([]value.Value) (value.Value, error) {
		return value.NewStr(h.req.Method), nil
	})
	in.RegisterBuiltin("path", func([]value.Value) (value.Value, error) {
		return value.NewStr(h.req.URL.Path), nil
	})
	in.RegisterBuiltin("query", func(args []value.Value) (value.Value, error) {
		if err := requireArgs("query", args, 1, value.Str); err != nil {
			return value.Value{}, err
		}
		return value.NewStr(h.req.URL.Query().Get(args[0].S)), nil
	})
	in.RegisterBuiltin("header", func(args []value.Value) (value.Value, error) {
		if err := requireArgs("header", args, 1, value.Str); err != nil {
			return value.Value{}, err
		}
		return value.NewStr(h.req.Header.Get(args[0].S)), nil
	})
	in.RegisterBuiltin("setStatus", func(args []value.Value) (value.Value, error) {
		if err := requireArgs("setStatus", args, 1, value.Int); err != nil {
			return value.Value{}, err
		}
		code := int(args[0].I)
		if code < 100 || code > 999 {
			return value.Value{}, fmt.Errorf("setStatus: status code %d out of range (100-999)", code)
		}
		h.status = code
		return value.NewVoid(), nil
	})
	in.RegisterBuiltin("setHeader", func(args []value.Value) (value.Value, error) {
		if err := requireArgs("setHeader", args, 2, value.Str, value.Str); err != nil {
			return value.Value{}, err
		}
		h.header.Add(args[0].S, args[1].S)
		return value.NewVoid(), nil
	})
}

// requireArgs validates builtin arity and declared argument kinds.
func requireArgs(name string, args []value.Value, want int, kinds ...value.Kind) error {
	if len(args) != want {
		return fmt.Errorf("%s: expects %d argument(s), got %d", name, want, len(args))
	}
	for i, k := range kinds {
		if args[i].Kind != k {
			return fmt.Errorf("%s: argument %d must be %s, got %s",
				name, i+1, value.TypeName(k), args[i].KindName())
		}
	}
	return nil
}
