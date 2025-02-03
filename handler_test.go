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
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

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

type dummyReverseProxy struct {
	ReverseProxy
	called bool
}

func NewDummyReverseProxy() *dummyReverseProxy {
	return &dummyReverseProxy{ReverseProxy: ReverseProxy{&reverseproxy.Handler{
		Upstreams: reverseproxy.UpstreamPool{
			&reverseproxy.Upstream{
				Dial: "",
			}}}}}
}

func (d *dummyReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	d.called = true
	return nil
}

func TestServeHTTPWithoutOrder(t *testing.T) {
	sh := &SubstrateHandler{
		Cmd: &execCmd{}, // Order is nil
		log: zap.NewNop(),
	}
	next := &dummyHandler{}
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	repl := caddy.NewReplacer()
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(ctx)

	if err := sh.ServeHTTP(rr, req, next); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
	if next.called {
		t.Error("next handler should not be called when Order is nil")
	}

	CheckUsagePool(t)
}

func TestServeHTTPWithOrder(t *testing.T) {
	drp := NewDummyReverseProxy()
	sh := &SubstrateHandler{
		Cmd: &execCmd{
			Order: &Order{
				Host:     "http://localhost:1234",
				TryFiles: []string{"/index.html"},
				Match:    []string{".html"},
			},
		},
		proxy: drp,
		fs:    fstest.MapFS{"foo/index.html": &fstest.MapFile{Data: []byte("content")}},
		log:   zap.NewNop(),
	}

	req, err := http.NewRequest("GET", "/foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()

	next := &dummyHandler{
		check: func(r *http.Request) {
			if r.URL.Path != "/foo/index.html" {
				t.Errorf("expected '/foo/index.html', got %s", r.URL.Path)
			}
			if got := r.Header.Get("X-Forwarded-Path"); got != "/foo" {
				t.Errorf("expected '/foo', got %s", got)
			}
		},
	}

	if err := sh.ServeHTTP(rr, req, next); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !drp.called {
		t.Error("proxy was not called")
	}

	CheckUsagePool(t)
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

	if sh.Cmd == nil {
		t.Fatal("Cmd should not be nil")
	}
	if len(sh.Cmd.Command) < 1 || sh.Cmd.Command[0] != "/bin/echo" {
		t.Errorf("unexpected command: %v", sh.Cmd.Command)
	}
	if val, ok := sh.Cmd.Env["FOO"]; !ok || val != "bar" {
		t.Errorf("unexpected env: %v", sh.Cmd.Env)
	}
	if sh.Cmd.User != "testuser" {
		t.Errorf("unexpected user: %s", sh.Cmd.User)
	}
	if sh.Cmd.Dir != "/tmp" {
		t.Errorf("unexpected dir: %s", sh.Cmd.Dir)
	}
	if sh.Cmd.RestartPolicy != "always" {
		t.Errorf("unexpected restart policy: %s", sh.Cmd.RestartPolicy)
	}
	if sh.Cmd.RedirectStdout == nil || sh.Cmd.RedirectStdout.Type != "file" || sh.Cmd.RedirectStdout.File != "/tmp/stdout.log" {
		t.Errorf("unexpected redirect_stdout: %+v", sh.Cmd.RedirectStdout)
	}
	if sh.Cmd.RedirectStderr == nil || sh.Cmd.RedirectStderr.Type != "stderr" {
		t.Errorf("unexpected redirect_stderr: %+v", sh.Cmd.RedirectStderr)
	}

	CheckUsagePool(t)
}

func TestGetRedirectFile(t *testing.T) {
	target := &outputTarget{Type: "stdout"}
	f, err := getRedirectFile(target, "")
	if err != nil {
		t.Errorf("stdout error: %v", err)
	}
	if f != os.Stdout {
		t.Error("expected os.Stdout")
	}

	target = &outputTarget{Type: "stderr"}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("stderr error: %v", err)
	}
	if f != os.Stderr {
		t.Error("expected os.Stderr")
	}

	target = &outputTarget{Type: "null"}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("null error: %v", err)
	}
	if f != nil {
		t.Error("expected nil for null")
	}

	tmpfile := filepath.Join(os.TempDir(), fmt.Sprintf("test_redirect_%d.log", time.Now().UnixNano()))
	target = &outputTarget{Type: "file", File: tmpfile}
	f, err = getRedirectFile(target, "")
	if err != nil {
		t.Errorf("file error: %v", err)
	}
	if f == nil {
		t.Error("expected file handle, got nil")
	}
	f.Close()
	os.Remove(tmpfile)

	target = &outputTarget{Type: "invalid"}
	_, err = getRedirectFile(target, "")
	if err == nil {
		t.Error("expected error for invalid target")
	}

	f, err = getRedirectFile(nil, "stdout")
	if err != nil {
		t.Errorf("nil target error: %v", err)
	}
	if f != os.Stdout {
		t.Error("expected os.Stdout")
	}

	CheckUsagePool(t)
}

func TestUpdateOrder(t *testing.T) {
	sh := &SubstrateHandler{log: zap.NewNop()}
	order := Order{
		TryFiles: []string{"/a", "/abc", "/ab", "/abcd", "/ab2"},
	}

	sh.Cmd = &execCmd{}
	sh.Cmd.UpdateOrder(order)

	sorted := sh.Cmd.Order.TryFiles
	expected := []string{"/abcd", "/ab2", "/abc", "/ab", "/a"}

	if len(sorted) != len(expected) {
		t.Fatalf("expected %d, got %d", len(expected), len(sorted))
	}
	if !slices.Equal(sorted, expected) {
		t.Errorf("try_files sorted incorrectly: got %v, want %v", sorted, expected)
	}

	CheckUsagePool(t)
}

