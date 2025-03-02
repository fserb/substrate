package substrate

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"go.uber.org/zap"
)

type mockHandler struct {
	called bool
	check  func(r *http.Request)
}

func (d *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	d.called = true
	if d.check != nil {
		d.check(r)
	}
	return nil
}

func TestMWServeHTTP(t *testing.T) {
	t.Run("WithReadyWatcherAndMatch", func(t *testing.T) {
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		proxy := &mockReverseProxy{}

		watcher := &Watcher{
			Order: order,
			cmd:   &execCmd{},
		}

		sh := &SubstrateHandler{
			watcher: watcher,
			proxy:   proxy,
			log:     zap.NewNop(),
		}

		req, _ := http.NewRequest("GET", "/api", nil)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		err := sh.ServeHTTP(rr, req, next)

		if err != nil {
			t.Errorf("ServeHTTP returned error: %v", err)
		}

		if !proxy.called {
			t.Error("Proxy should have been called")
		}

		if proxy.host != "http://localhost:8080" {
			t.Errorf("Proxy host = %q, want %q", proxy.host, "http://localhost:8080")
		}

		if next.called {
			t.Error("Next handler should not have been called")
		}
	})

	t.Run("WithReadyWatcherNoMatch", func(t *testing.T) {
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		proxy := &mockReverseProxy{}

		testFS := fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("index")},
		}

		watcher := &Watcher{
			Order: order,
			cmd:   &execCmd{},
		}

		sh := &SubstrateHandler{
			watcher: watcher,
			proxy:   proxy,
			log:     zap.NewNop(),
			fs:      testFS,
		}

		req, _ := http.NewRequest("GET", "/other", nil)
		ctx := context.WithValue(req.Context(), "root", ".")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		err := sh.ServeHTTP(rr, req, next)

		if err != nil {
			t.Errorf("ServeHTTP returned error: %v", err)
		}

		if proxy.called {
			t.Error("Proxy should not have been called")
		}

		if !next.called {
			t.Error("Next handler should have been called")
		}
	})
}

func TestMWFileExists(t *testing.T) {
	testFS := fstest.MapFS{
		"file.txt":      &fstest.MapFile{Data: []byte("content")},
		"dir/file2.txt": &fstest.MapFile{Data: []byte("content2")},
		"dir":           &fstest.MapFile{Mode: fs.ModeDir},
	}

	sh := &SubstrateHandler{fs: testFS}

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.txt", true},      // Regular file
		{"dir/", true},          // Directory with trailing slash
		{"dir", false},          // Directory without trailing slash (not a file)
		{"nonexistent", false},  // Non-existent file
		{"dir/file2.txt", true}, // Nested file
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := sh.fileExists(tc.path)
			if result != tc.expected {
				t.Errorf("fileExists(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestMWFindBestResource(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("index")},
		"about.html":      &fstest.MapFile{Data: []byte("about")},
		"blog/index.html": &fstest.MapFile{Data: []byte("blog index")},
		"blog/post1.html": &fstest.MapFile{Data: []byte("post1")},
		"docs/index.md":   &fstest.MapFile{Data: []byte("docs index")},
		"docs/guide.md":   &fstest.MapFile{Data: []byte("guide")},
	}

	order := &Order{
		Match:    []string{"*.html", "*.md"},
		CatchAll: []string{"/blog/index.html"},
	}
	order.matchers = []orderMatcher{
		{path: "/", ext: ".html"},
		{path: "/", ext: ".md"},
	}

	watcher := &Watcher{
		Order: order,
	}

	sh := &SubstrateHandler{
		fs:      testFS,
		watcher: watcher,
	}

	tests := []struct {
		name     string
		path     string
		expected *string
	}{
		{"Direct file match", "/index.html", strPtr("/index.html")},
		{"Extension match", "/about", strPtr("/about.html")},
		{"Directory index", "/blog", strPtr("/blog/index.html")},
		{"Nested file", "/blog/post1", strPtr("/blog/post1.html")},
		{"Different extension", "/docs/guide", strPtr("/docs/guide.md")},
		{"CatchAll fallback", "/blog/nonexistent", strPtr("/blog/index.html")},
		{"No match", "/nonexistent", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tc.path, nil)
			ctx := context.WithValue(req.Context(), "root", ".")
			req = req.WithContext(ctx)

			result := sh.findBestResource(req, watcher)

			if tc.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", *result)
				}
			} else if result == nil {
				t.Errorf("Expected %v, got nil", *tc.expected)
			} else if *result != *tc.expected {
				t.Errorf("Expected %v, got %v", *tc.expected, *result)
			}
		})
	}
}

func TestMWMatchPath(t *testing.T) {
	order := &Order{
		Paths: []string{"/api/v1", "/api/v2/users", "/static"},
	}

	watcher := &Watcher{
		Order: order,
	}

	sh := &SubstrateHandler{}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/v1", true},
		{"/api/v2/users", true},
		{"/static", true},
		{"/api", false},
		{"/api/v2", false},
		{"/api/v1/users", false},
		{"/static/", false},           // Trailing slash
		{"/static/css", false},        // Subdirectory
		{"/api/v1?param=value", true}, // With query parameters
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tc.path, nil)

			result := sh.matchPath(req, watcher)

			if result != tc.expected {
				t.Errorf("matchPath(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}

	req, _ := http.NewRequest("GET", "/api/v1", nil)
	if sh.matchPath(req, nil) {
		t.Error("matchPath with nil watcher should return false")
	}

	if sh.matchPath(req, &Watcher{Order: nil}) {
		t.Error("matchPath with nil order should return false")
	}

	// Test with empty paths
	emptyOrder := &Order{
		Paths: []string{},
	}
	emptyWatcher := &Watcher{
		Order: emptyOrder,
	}
	if sh.matchPath(req, emptyWatcher) {
		t.Error("matchPath with empty paths should return false")
	}
}

func strPtr(s string) *string {
	return &s
}
