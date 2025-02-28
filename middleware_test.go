package substrate

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// Mock implementations for testing
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

type mockReverseProxy struct {
	host   string
	called bool
}

func (d *mockReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	d.called = true
	return nil
}

func (d *mockReverseProxy) Provision(ctx caddy.Context) error {
	return nil
}

func (d *mockReverseProxy) SetHost(host string) {
	d.host = host
}

// TestServeHTTP tests the ServeHTTP method
func TestMWServeHTTP(t *testing.T) {
	// Test with a ready watcher and matching path
	t.Run("WithReadyWatcherAndMatch", func(t *testing.T) {
		// Create a test order
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		// Create a mock proxy
		proxy := &mockReverseProxy{}

		// Create a watcher with the order
		watcher := &Watcher{
			Order: order,
			cmd:   &execCmd{},
		}

		sh := &SubstrateHandler{
			watcher: watcher,
			proxy:   proxy,
			log:     zap.NewNop(),
		}

		// Create a request with a matching path
		req, _ := http.NewRequest("GET", "/api", nil)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		// Call ServeHTTP
		err := sh.ServeHTTP(rr, req, next)

		// Check results
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

	// Test with a ready watcher but no match
	t.Run("WithReadyWatcherNoMatch", func(t *testing.T) {
		// Create a test order
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		// Create a mock proxy
		proxy := &mockReverseProxy{}

		// Create a test filesystem
		testFS := fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("index")},
		}

		// Create a watcher with the order
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

		// Create a request with a non-matching path
		req, _ := http.NewRequest("GET", "/other", nil)
		ctx := context.WithValue(req.Context(), "root", ".")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		// Call ServeHTTP
		err := sh.ServeHTTP(rr, req, next)

		// Check results
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

// TestFileExists tests the fileExists method
func TestMWFileExists(t *testing.T) {
	// Create a test filesystem
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

// TestFindBestResource tests the findBestResource method
func TestMWFindBestResource(t *testing.T) {
	// Create a test filesystem
	testFS := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("index")},
		"about.html":      &fstest.MapFile{Data: []byte("about")},
		"blog/index.html": &fstest.MapFile{Data: []byte("blog index")},
		"blog/post1.html": &fstest.MapFile{Data: []byte("post1")},
		"docs/index.md":   &fstest.MapFile{Data: []byte("docs index")},
		"docs/guide.md":   &fstest.MapFile{Data: []byte("guide")},
	}

	// Create a test order with matchers
	order := &Order{
		Match:    []string{"*.html", "*.md"},
		CatchAll: []string{"/blog/index.html"},
	}
	order.matchers = []orderMatcher{
		{path: "/", ext: ".html"},
		{path: "/", ext: ".md"},
	}

	// Create a watcher with the order
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

// TestMatchPath tests the matchPath method
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

	// Test with nil watcher or nil order
	req, _ := http.NewRequest("GET", "/api/v1", nil)
	if sh.matchPath(req, nil) {
		t.Error("matchPath with nil watcher should return false")
	}

	if sh.matchPath(req, &Watcher{Order: nil}) {
		t.Error("matchPath with nil order should return false")
	}
}

// Helper function to create string pointer
func strPtr(s string) *string {
	return &s
}
