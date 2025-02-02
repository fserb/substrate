package substrate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"go.uber.org/zap"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

// Dummy next handler for middleware chain.
type dummyHandler struct {
	called bool
	check  func(r *http.Request)
}

func (d *dummyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	d.called = true
	if d.check != nil {
		d.check(r)
	}
	return nil
}

func TestServeHTTPWithoutOrder(t *testing.T) {
	sh := &SubstrateHandler{log: zap.NewNop()}
	// Order is nil.
	next := &dummyHandler{}
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Provide a replacer in context.
	repl := caddy.NewReplacer()
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(ctx)

	// Expect ServeHTTP to write a 500 since Order is nil.
	if err := sh.ServeHTTP(rr, req, next); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
	if next.called {
		t.Error("next handler should not be called when Order is nil")
	}
}

func TestServeHTTPWithOrder(t *testing.T) {
	// Set up a SubstrateHandler with an Order.
	sh := &SubstrateHandler{
		Order: &Order{
			Host:     "http://localhost:1234",
			TryFiles: []string{"/index.html"},
			Match:    []string{".html"},
		},
		log: zap.NewNop(),
	}
	// Create a fake file system with a file at "/foo/index.html".
	sh.fs = fstest.MapFS{
		"foo/index.html": &fstest.MapFile{Data: []byte("content")},
	}

	// Prepare a replacer with required variables.
	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", ".")
	repl.Set("http.vars.fs", "")

	req, err := http.NewRequest("GET", "/foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	next := &dummyHandler{
		check: func(r *http.Request) {
			// Expect the URL path to be updated to "/foo/index.html"
			if r.URL.Path != "/foo/index.html" {
				t.Errorf("expected URL path '/foo/index.html', got %s", r.URL.Path)
			}
			// Header should indicate the original path.
			if got := r.Header.Get("X-Forwarded-Path"); got != "/foo" {
				t.Errorf("expected X-Forwarded-Path '/foo', got %s", got)
			}
		},
	}

	if err := sh.ServeHTTP(rr, req, next); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !next.called {
		t.Error("next handler was not called")
	}
	// Check that reverse proxy enabling set the substrate.host variable.
	if val, _ := repl.Get("substrate.host"); val != "http://localhost:1234" {
		t.Errorf("expected replacer 'substrate.host' to be 'http://localhost:1234', got %s", val)
	}
}

func TestUnmarshalCaddyfile(t *testing.T) {
	caddyfileInput := `
	substrate {
	    command /bin/echo hello world
	    env FOO bar
	    user testuser
	    dir /tmp
	    restart_policy always
	    redirect_stdout file /tmp/stdout.log
	    redirect_stderr stderr
	}
	`
	d := caddyfile.NewTestDispenser(caddyfileInput)
	sh := &SubstrateHandler{}
	if err := sh.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile failed: %v", err)
	}

	if len(sh.Command) < 1 || sh.Command[0] != "/bin/echo" {
		t.Errorf("unexpected command: %v", sh.Command)
	}
	if val, ok := sh.Env["FOO"]; !ok || val != "bar" {
		t.Errorf("unexpected env: %v", sh.Env)
	}
	if sh.User != "testuser" {
		t.Errorf("unexpected user: %s", sh.User)
	}
	if sh.Dir != "/tmp" {
		t.Errorf("unexpected dir: %s", sh.Dir)
	}
	if sh.RestartPolicy != "always" {
		t.Errorf("unexpected restart policy: %s", sh.RestartPolicy)
	}
	if sh.RedirectStdout == nil || sh.RedirectStdout.Type != "file" || sh.RedirectStdout.File != "/tmp/stdout.log" {
		t.Errorf("unexpected redirect_stdout: %+v", sh.RedirectStdout)
	}
	if sh.RedirectStderr == nil || sh.RedirectStderr.Type != "stderr" {
		t.Errorf("unexpected redirect_stderr: %+v", sh.RedirectStderr)
	}
}

func TestGetRedirectFile(t *testing.T) {
	// Test stdout.
	target := &outputTarget{Type: "stdout"}
	f, err := getRedirectFile(target, "")
	if err != nil {
		t.Errorf("getRedirectFile(stdout) error: %v", err)
	}
	if f != os.Stdout {
		t.Error("expected os.Stdout for stdout target")
	}

	// Test stderr.
	target = &outputTarget{Type: "stderr"}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("getRedirectFile(stderr) error: %v", err)
	}
	if f != os.Stderr {
		t.Error("expected os.Stderr for stderr target")
	}

	// Test null.
	target = &outputTarget{Type: "null"}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("getRedirectFile(null) error: %v", err)
	}
	if f != nil {
		t.Error("expected nil for null target")
	}

	// Test file.
	tmpfile := filepath.Join(os.TempDir(), fmt.Sprintf("test_redirect_%d.log", time.Now().UnixNano()))
	target = &outputTarget{Type: "file", File: tmpfile}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("getRedirectFile(file) error: %v", err)
	}
	if f == nil {
		t.Error("expected non-nil file for file target")
	}
	f.Close()
	os.Remove(tmpfile)

	// Test invalid type.
	target = &outputTarget{Type: "invalid"}
	_, err = getRedirectFile(target, "")
	if err == nil {
		t.Error("expected error for invalid redirect target")
	}

	f, err = getRedirectFile(nil, "stdout")
	if err != nil {
		t.Errorf("getRedirectFile(nil) error: %v", err)
	}
	if f != os.Stdout {
		t.Error("expected os.Stdout for nil target")
	}
}

func TestUpdateOrder(t *testing.T) {
	sh := &SubstrateHandler{log: zap.NewNop()}
	order := Order{
		TryFiles: []string{"/a", "/abc", "/ab", "/abcd", "/ab2"},
	}
	sh.UpdateOrder(order)
	sorted := sh.Order.TryFiles
	// Expected sort: first by descending length, then lexicographically.
	expected := []string{"/abcd", "/ab2", "/abc", "/ab", "/a"}
	if len(sorted) != len(expected) {
		t.Fatalf("expected %d try_files, got %d", len(expected), len(sorted))
	}
	// Ensure sorting is as expected.
	if !slices.Equal(sorted, expected) {
		t.Errorf("sorted try_files: got %v, expected %v", sorted, expected)
	}
}

