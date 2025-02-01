package substrate

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func strPtr(s string) *string { return &s }

func TestFileExists(t *testing.T) {
	mfs := fstest.MapFS{
		"file.txt":      {Data: []byte("content")},
		"dir":           {Mode: fs.ModeDir},
		"dir/file2.txt": {Data: []byte("content2")},
	}
	sh := &SubstrateHandler{fs: mfs}

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.txt", true},
		{"dir/", true},
		{"dir", false}, // missing trailing slash
		{"nonexistent", false},
	}

	for _, tt := range tests {
		got := sh.fileExists(tt.path)
		if got != tt.expected {
			t.Errorf("fileExists(%q) = %v; want %v", tt.path, got, tt.expected)
		}
	}
}

func TestFindBestResource(t *testing.T) {
	mfs := fstest.MapFS{
		"index.html":            {Data: []byte("index")},
		"about.html":            {Data: []byte("about")},
		"blog/index.html":       {Data: []byte("blog index")},
		"foo/bar.html":          {Data: []byte("foo bar")},
		"weird/path/index.html": {Data: []byte("weird")},
	}
	order := &Order{
		TryFiles: []string{".html"},
		CatchAll: []string{"index.html"},
	}
	sh := &SubstrateHandler{fs: mfs, Order: order}

	tests := []struct {
		reqPath string
		want    *string
	}{
		{"/index.html", strPtr("/index.html")},
		{"/about", strPtr("/about.html")},
		// For "/blog/post", "/blog/post.html" fails; catch-all climbs to "/blog/index.html".
		{"/blog/post", strPtr("/blog/index.html")},
		// Weird URL with double slash; tryFiles appends suffix.
		{"/foo//bar", strPtr("/foo//bar.html")},
		// No candidate found.
		{"/notfound", strPtr("/index.html")},
	}

	for _, tt := range tests {
		r := &http.Request{
			URL: &url.URL{Path: tt.reqPath},
		}
		// Inject "root" variable in context.
		ctx := context.WithValue(r.Context(), "root", ".")
		r = r.WithContext(ctx)
		got := sh.findBestResource(r)
		if tt.want == nil {
			if got != nil {
				t.Errorf("findBestResource(%q) = %v; want nil", tt.reqPath, *got)
			}
		} else if got == nil || *got != *tt.want {
			t.Errorf("findBestResource(%q) = %v; want %v", tt.reqPath, got, *tt.want)
		}
	}
}

func TestEnableReverseProxy(t *testing.T) {
	sh := &SubstrateHandler{
		Order: &Order{
			Match: []string{".jpg", ".png"},
		},
	}
	tests := []struct {
		path     string
		expected bool
	}{
		{"/image.jpg", true},
		{"/image.png", true},
		{"/image.gif", false},
	}
	for _, tt := range tests {
		r := &http.Request{URL: &url.URL{Path: tt.path}}
		got := sh.enableReverseProxy(r)
		if got != tt.expected {
			t.Errorf("enableReverseProxy(%q) = %v; want %v", tt.path, got, tt.expected)
		}
	}
}

func TestServeHTTP(t *testing.T) {
	mfs := fstest.MapFS{
		"about.html":      {Data: []byte("about")},
		"blog/index.html": {Data: []byte("blog index")},
	}
	order := &Order{
		TryFiles: []string{".html"},
		CatchAll: []string{"index.html"},
		Host:     "example.com",
		Match:    []string{".html"},
	}
	sh := &SubstrateHandler{fs: mfs, Order: order, log: zap.NewNop()}

	// Dummy next handler.
	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	// Create a replacer and inject it into the context.
	repl := caddy.NewReplacer()
	ctx := context.WithValue(context.Background(), caddy.ReplacerCtxKey, repl)

	// Test rewriting: /about doesn't exist, but /about.html does.
	req := httptest.NewRequest("GET", "http://example.com/about", nil)
	// Inject "root" var and replacer into context.
	req = req.WithContext(context.WithValue(ctx, "root", "."))
	rr := httptest.NewRecorder()

	err := sh.ServeHTTP(rr, req, next)
	if err != nil {
		t.Errorf("ServeHTTP returned error: %v", err)
	}
	if !nextCalled {
		t.Errorf("next handler was not called")
	}
	if got := req.Header.Get("X-Forwarded-Path"); got != "/about" {
		t.Errorf("expected X-Forwarded-Path header to be /about, got %q", got)
	}
	if req.URL.Path != "/about.html" {
		t.Errorf("expected URL.Path to be /about.html, got %q", req.URL.Path)
	}
	if v, _ := repl.Get("substrate.host"); v != "example.com" {
		t.Errorf("expected replacer substrate.host to be example.com, got %q", v)
	}

	// Test when Order is nil.
	shNil := &SubstrateHandler{fs: mfs, Order: nil, log: zap.NewNop()}
	reqNil := httptest.NewRequest("GET", "http://example.com/", nil)
	rrNil := httptest.NewRecorder()
	err = shNil.ServeHTTP(rrNil, reqNil, next)
	if rrNil.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for nil Order, got %d", rrNil.Code)
	}
}

