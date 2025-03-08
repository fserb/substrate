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

// This test has been removed as findBestResource is no longer used

func TestMWFileMatchWithRealFile(t *testing.T) {
	// Create a test filesystem with the example/index.md file
	testFS := fstest.MapFS{
		"example/index.md": &fstest.MapFile{Data: []byte("xxxx")},
	}

	order := &Order{
		Routes: []string{"/example/?.md"},
	}

	watcher := &Watcher{
		Order: order,
	}
	
	// Manually compile the patterns for testing
	watcher.Order.compiledRoutes = []*routePattern{
		{
			pattern: "/example/?.md", 
			fileMatchParts: []string{"/example/", ".md"}, 
			hasFileMatch: true,
		},
	}

	sh := &SubstrateHandler{
		fs: testFS,
	}

	tests := []struct {
		path     string
		expected bool
		expectedPath string
	}{
		{"/example", true, "/example/index.md"},
		{"/example/index", true, "/example/index.md"},
		{"/example/index.md", true, "/example/index.md"},
		{"/example/nonexistent", false, "/example/nonexistent"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			matched, matchedPath := sh.matchRoute(tc.path, watcher)

			if matched != tc.expected {
				t.Errorf("matchRoute(%q) = %v, want %v", tc.path, matched, tc.expected)
			}
			
			if matched && matchedPath != tc.expectedPath {
				t.Errorf("matchRoute(%q) returned path %q, want %q", tc.path, matchedPath, tc.expectedPath)
			}
		})
	}
}

func TestMWMatchRoute(t *testing.T) {
	order := &Order{
		Routes: []string{"/api/*", "/static", "/exact/path"},
		Avoid:  []string{"/api/internal/*"},
	}

	watcher := &Watcher{
		Order: order,
	}
	
	// Manually compile the patterns for testing
	watcher.Order.compiledRoutes = []*routePattern{
		{pattern: "/api/*", parts: []string{"/api/", ""}, hasStar: true},
		{pattern: "/static", parts: []string{"/static"}, hasStar: false},
		{pattern: "/exact/path", parts: []string{"/exact/path"}, hasStar: false},
	}
	
	watcher.Order.compiledAvoid = []*routePattern{
		{pattern: "/api/internal/*", parts: []string{"/api/internal/", ""}, hasStar: true},
	}

	sh := &SubstrateHandler{}

	tests := []struct {
		path     string
		expected bool
		expectedPath string
	}{
		{"/api/v1", true, "/api/v1"},
		{"/api/v2/users", true, "/api/v2/users"},
		{"/static", true, "/static"},
		{"/exact/path", true, "/exact/path"},
		{"/api/internal/secrets", false, "/api/internal/secrets"}, // Should be avoided
		{"/other/path", false, "/other/path"},           // No match
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			matched, matchedPath := sh.matchRoute(tc.path, watcher)

			if matched != tc.expected {
				t.Errorf("matchRoute(%q) = %v, want %v", tc.path, matched, tc.expected)
			}
			
			if matchedPath != tc.expectedPath {
				t.Errorf("matchRoute(%q) returned path %q, want %q", tc.path, matchedPath, tc.expectedPath)
			}
		})
	}

	// Test with nil watcher
	matched, _ := sh.matchRoute("/api/v1", nil)
	if matched {
		t.Error("matchRoute with nil watcher should return false")
	}

	// Test with nil order
	matched, _ = sh.matchRoute("/api/v1", &Watcher{Order: nil})
	if matched {
		t.Error("matchRoute with nil order should return false")
	}

	// Test with empty routes (should match everything except avoided)
	emptyOrder := &Order{
		Routes: []string{},
		Avoid:  []string{"/private/*"},
		compiledRoutes: []*routePattern{},
		compiledAvoid: []*routePattern{
			{pattern: "/private/*", parts: []string{"/private/", ""}, hasStar: true},
		},
	}
	emptyWatcher := &Watcher{
		Order: emptyOrder,
	}
	
	matched, _ = sh.matchRoute("/public/path", emptyWatcher)
	if !matched {
		t.Error("matchRoute with empty routes should match non-avoided paths")
	}
	
	matched, _ = sh.matchRoute("/private/path", emptyWatcher)
	if matched {
		t.Error("matchRoute with empty routes should not match avoided paths")
	}
	
	// Test route order preservation
	orderedOrder := &Order{
		Routes: []string{"/first/*", "/second/*"},
		compiledRoutes: []*routePattern{
			{pattern: "/first/*", parts: []string{"/first/", ""}, hasStar: true},
			{pattern: "/second/*", parts: []string{"/second/", ""}, hasStar: true},
		},
	}
	orderedWatcher := &Watcher{
		Order: orderedOrder,
	}
	
	matched, _ = sh.matchRoute("/first/path", orderedWatcher)
	if !matched {
		t.Error("Should match first route")
	}
	
	matched, _ = sh.matchRoute("/second/path", orderedWatcher)
	if !matched {
		t.Error("Should match second route")
	}
}

func TestMWMatchRouteWithFilePatterns(t *testing.T) {
	// Create a test filesystem with some files
	testFS := fstest.MapFS{
		"index.md":      &fstest.MapFile{Data: []byte("index")},
		"about.md":      &fstest.MapFile{Data: []byte("about")},
		"blog/index.md": &fstest.MapFile{Data: []byte("blog index")},
		"blog/post1.md": &fstest.MapFile{Data: []byte("post1")},
	}

	order := &Order{
		Routes: []string{"/?.md", "/blog/?.md"},
	}

	watcher := &Watcher{
		Order: order,
	}
	
	// Manually compile the patterns for testing
	watcher.Order.compiledRoutes = []*routePattern{
		{
			pattern: "/?.md", 
			fileMatchParts: []string{"/", ".md"}, 
			hasFileMatch: true,
		},
		{
			pattern: "/blog/?.md", 
			fileMatchParts: []string{"/blog/", ".md"}, 
			hasFileMatch: true,
		},
	}

	sh := &SubstrateHandler{
		fs: testFS,
	}

	tests := []struct {
		path     string
		expected bool
		expectedPath string
	}{
		{"/", true, "/index.md"},
		{"/about", true, "/about.md"},
		{"/about.md", true, "/about.md"},
		{"/blog", true, "/blog/index.md"},
		{"/blog/post1", true, "/blog/post1.md"},
		{"/nonexistent", false, "/nonexistent"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			matched, matchedPath := sh.matchRoute(tc.path, watcher)

			if matched != tc.expected {
				t.Errorf("matchRoute(%q) = %v, want %v", tc.path, matched, tc.expected)
			}
			
			if matched && matchedPath != tc.expectedPath {
				t.Errorf("matchRoute(%q) returned path %q, want %q", tc.path, matchedPath, tc.expectedPath)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
