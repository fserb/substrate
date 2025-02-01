package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap/zaptest"
)

func TestUpdateOrderSorting(t *testing.T) {
	sh := &SubstrateHandler{}
	sh.log = zaptest.NewLogger(t)
	order := Order{
		TryFiles: []string{"a", "abc", "zz", "ab"},
		Match:    []string{"z", "xy", "x"},
	}
	sh.UpdateOrder(order)
	expected := []string{"abc", "ab", "zz", "a"}
	for i, v := range sh.Order.TryFiles {
		if v != expected[i] {
			t.Errorf("TryFiles[%d]: expected %s, got %s", i, expected[i], v)
		}
	}
}

func TestKeyNotEmpty(t *testing.T) {
	sh := &SubstrateHandler{Command: []string{"echo", "hello"}}
	sh.log = zaptest.NewLogger(t)
	if key := sh.Key(); key == "" {
		t.Error("key should not be empty")
	}
}

func TestUnmarshalCaddyfile(t *testing.T) {
	cfile := `substrate {
		command sleep 10
		env KEY value
		user nobody
		dir /tmp
		restart_policy always
		redirect_stdout file /tmp/out.log
		redirect_stderr stderr
	}`
	d := caddyfile.NewTestDispenser(cfile)
	var sh SubstrateHandler
	sh.log = zaptest.NewLogger(t)
	if err := sh.UnmarshalCaddyfile(d); err != nil {
		t.Fatal(err)
	}
	if len(sh.Command) == 0 || sh.Command[0] != "sleep" {
		t.Error("command not parsed correctly")
	}
	if sh.Env["KEY"] != "value" {
		t.Error("env not parsed correctly")
	}
	if sh.User != "nobody" {
		t.Error("user not parsed correctly")
	}
	if sh.Dir != "/tmp" {
		t.Error("dir not parsed correctly")
	}
	if sh.RestartPolicy != "always" {
		t.Error("restart_policy not parsed correctly")
	}
	if sh.RedirectStdout == nil || sh.RedirectStdout.Type != "file" || sh.RedirectStdout.File != "/tmp/out.log" {
		t.Error("redirect_stdout not parsed correctly")
	}
	if sh.RedirectStderr == nil || sh.RedirectStderr.Type != "stderr" {
		t.Error("redirect_stderr not parsed correctly")
	}
}

func TestGetRedirectFile(t *testing.T) {
	// Test stdout
	f, err := getRedirectFile(&outputTarget{Type: "stdout"})
	if err != nil || f != os.Stdout {
		t.Error("stdout redirect failed")
	}
	// Test stderr
	f, err = getRedirectFile(&outputTarget{Type: "stderr"})
	if err != nil || f != os.Stderr {
		t.Error("stderr redirect failed")
	}
	// Test null
	f, err = getRedirectFile(&outputTarget{Type: "null"})
	if err != nil || f != nil {
		t.Error("null redirect failed")
	}
	// Test file (using a temporary file)
	tempFile := "temp_test.log"
	defer os.Remove(tempFile)
	f, err = getRedirectFile(&outputTarget{Type: "file", File: tempFile})
	if err != nil || f == nil {
		t.Error("file redirect failed")
	}
	f.Close()
}

func TestSubstrateHandlerMiddleware_NoOrder(t *testing.T) {
	sh := &SubstrateHandler{}
	sh.log = zaptest.NewLogger(t)
	// No order set

	called := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	})

	req := httptest.NewRequest("GET", "/", nil)
	// Set a replacer in the context
	repl := caddy.NewReplacer()
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	rw := httptest.NewRecorder()

	err := sh.ServeHTTP(rw, req, next)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if called {
		t.Error("next handler should not be called when order is nil")
	}
	if rw.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rw.Code)
	}
}

func TestSubstrateHandlerMiddleware_WithOrder(t *testing.T) {
	sh := &SubstrateHandler{}
	sh.log = zaptest.NewLogger(t)
	sh.Order = &Order{
		Host:     "http://example.com",
		TryFiles: []string{"try1", "try2"},
		Match:    []string{"match1", "match2", "match3"},
	}

	called := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		// Check replacer mappings
		repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
		if got, ok := repl.Get("substrate.host"); ok && got != sh.Order.Host {
			t.Errorf("expected substrate.host %q, got %q", sh.Order.Host, got)
		}
		if got, ok := repl.Get("substrate.match_files.0"); ok && got != sh.Order.TryFiles[0] {
			t.Errorf("expected substrate.match_files.0 %q, got %q", sh.Order.TryFiles[0], got)
		}
		if got, ok := repl.Get("substrate.match_path.2"); ok && got != sh.Order.Match[2] {
			t.Errorf("expected substrate.match_path.2 %q, got %q", sh.Order.Match[2], got)
		}
		return nil
	})

	req := httptest.NewRequest("GET", "/", nil)
	repl := caddy.NewReplacer()
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	rw := httptest.NewRecorder()

	err := sh.ServeHTTP(rw, req, next)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("next handler should be called when order is set")
	}
}

