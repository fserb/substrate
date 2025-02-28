package substrate

import (
	"context"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"
)

// TestSubstrateHandlerCaddyModule tests the CaddyModule method
func TestSubstrateHandlerCaddyModule(t *testing.T) {
	sh := SubstrateHandler{}
	info := sh.CaddyModule()

	if info.ID != "http.handlers.substrate" {
		t.Errorf("Expected module ID 'http.handlers.substrate', got '%s'", info.ID)
	}

	// Test that New returns a *SubstrateHandler
	module := info.New()
	_, ok := module.(*SubstrateHandler)
	if !ok {
		t.Errorf("Expected New to return *SubstrateHandler, got %T", module)
	}
}

// TestFileExists tests the fileExists method
func TestFileExists(t *testing.T) {
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
func TestFindBestResource(t *testing.T) {
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
		CatchAll: []string{"blog/index.html"},
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
		{"CatchAll fallback", "/blog/nonexistent", strPtr("blog/index.html")},
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
func TestMatchPath(t *testing.T) {
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
