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
		{"dir", false},
		{"nonexistent", false},
	}
	for _, tt := range tests {
		got := sh.fileExists(tt.path)
		if got != tt.expected {
			t.Errorf("fileExists(%q) = %v; want %v", tt.path, got, tt.expected)
		}
	}

	CheckUsagePool(t)
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
		Match:    []string{"*.html"},
		CatchAll: []string{"/blog/index.html"},
	}
	sh := &SubstrateHandler{
		fs:  mfs,
		Cmd: &execCmd{},
	}
	order.Submit(sh.Cmd)

	tests := []struct {
		reqPath string
		want    *string
	}{
		{"/index.html", strPtr("/index.html")},
		{"/about", strPtr("/about.html")},
		{"/blog/post", strPtr("/blog/index.html")},
		{"/foo//bar", strPtr("/foo//bar.html")},
		{"/notfound", nil},
	}

	for _, tt := range tests {
		r := &http.Request{URL: &url.URL{Path: tt.reqPath}}
		ctx := context.WithValue(r.Context(), "root", ".")
		r = r.WithContext(ctx)
		got := sh.findBestResource(r)
		if tt.want == nil {
			if got != nil {
				t.Errorf("findBestResource(%q) = %v; want nil", tt.reqPath, *got)
			}
		} else if got == nil || *got != *tt.want {
			t.Errorf("findBestResource(%q) = %#v; want %v", tt.reqPath, got, *tt.want)
		}
	}

	CheckUsagePool(t)
}

func TestServeHTTP(t *testing.T) {
	mfs := fstest.MapFS{
		"about.html":      {Data: []byte("about")},
		"blog/index.html": {Data: []byte("blog index")},
	}
	order := &Order{
		CatchAll: []string{"index.html"},
		Host:     "example.com",
		Match:    []string{"*.html"},
	}
	drp := NewDummyReverseProxy()
	sh := &SubstrateHandler{
		fs:    mfs,
		log:   zap.NewNop(),
		proxy: drp,
		Cmd:   &execCmd{},
	}
	order.Submit(sh.Cmd)
	next := &dummyHandler{}

	repl := caddy.NewReplacer()
	ctx := context.WithValue(context.Background(), caddy.ReplacerCtxKey, repl)
	req := httptest.NewRequest("GET", "http://example.com/about", nil)
	req = req.WithContext(context.WithValue(ctx, "root", "."))
	rr := httptest.NewRecorder()

	err := sh.ServeHTTP(rr, req, next)
	if err != nil {
		t.Errorf("ServeHTTP returned error: %v", err)
	}
	if !drp.called {
		t.Error("next handler was not called")
	}
	if got := req.Header.Get("X-Forwarded-Path"); got != "/about" {
		t.Errorf("X-Forwarded-Path = %q; want /about", got)
	}
	if req.URL.Path != "/about.html" {
		t.Errorf("URL.Path = %q; want /about.html", req.URL.Path)
	}

	shNil := &SubstrateHandler{fs: mfs, log: zap.NewNop()}
	reqNil := httptest.NewRequest("GET", "http://example.com/", nil)
	rrNil := httptest.NewRecorder()
	err = shNil.ServeHTTP(rrNil, reqNil, next)

	CheckUsagePool(t)
}

