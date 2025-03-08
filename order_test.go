package substrate

import (
	"strings"
	"testing"
	"testing/fstest"

	"go.uber.org/zap"
)

func TestRoutePatternCompilation(t *testing.T) {
	watcher := &Watcher{
		log: zap.NewNop(),
	}

	tests := []struct {
		name     string
		patterns []string
		expected int
	}{
		{
			name:     "Empty patterns",
			patterns: []string{},
			expected: 0,
		},
		{
			name:     "Valid patterns",
			patterns: []string{"/foo/*", "/bar/*/baz", "/*.html"},
			expected: 3,
		},
		{
			name:     "Patterns with empty strings",
			patterns: []string{"/foo/*", "", "/bar"},
			expected: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := watcher.compileRoutePatterns(tc.patterns)

			if len(result) != tc.expected {
				t.Fatalf("Expected %d compiled patterns, got %d", tc.expected, len(result))
			}

			// Verify patterns are in the same order as input
			for i, pattern := range result {
				if i < len(tc.patterns) && tc.patterns[i] != "" {
					expected := tc.patterns[i]
					if !strings.HasPrefix(expected, "/") {
						expected = "/" + expected
					}
					if pattern.pattern != expected {
						t.Errorf("Pattern at index %d is %q, expected %q", i, pattern.pattern, expected)
					}
				}
			}
		})
	}
}

func TestRoutePatternMatching(t *testing.T) {
	handler := &SubstrateHandler{}
	
	tests := []struct {
		name     string
		path     string
		pattern  *routePattern
		expected bool
	}{
		{
			name:    "Exact match",
			path:    "/foo/bar",
			pattern: &routePattern{pattern: "/foo/bar", parts: []string{"/foo/bar"}, hasStar: false},
			expected: true,
		},
		{
			name:    "Wildcard at end",
			path:    "/foo/bar/baz",
			pattern: &routePattern{pattern: "/foo/*", parts: []string{"/foo/", ""}, hasStar: true},
			expected: true,
		},
		{
			name:    "Wildcard in middle",
			path:    "/foo/bar/baz",
			pattern: &routePattern{pattern: "/foo/*/baz", parts: []string{"/foo/", "/baz"}, hasStar: true},
			expected: true,
		},
		{
			name:    "Multiple wildcards",
			path:    "/foo/bar/baz/qux",
			pattern: &routePattern{pattern: "/foo/*/baz/*", parts: []string{"/foo/", "/baz/", ""}, hasStar: true},
			expected: true,
		},
		{
			name:    "No match - different path",
			path:    "/bar/baz",
			pattern: &routePattern{pattern: "/foo/bar", parts: []string{"/foo/bar"}, hasStar: false},
			expected: false,
		},
		{
			name:    "No match - partial match with wildcard",
			path:    "/foo/bar/qux",
			pattern: &routePattern{pattern: "/foo/*/baz", parts: []string{"/foo/", "/baz"}, hasStar: true},
			expected: false,
		},
		{
			name:    "Wildcard at beginning",
			path:    "/foo/bar",
			pattern: &routePattern{pattern: "*/bar", parts: []string{"", "/bar"}, hasStar: true},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.pathMatchesPattern(tc.path, tc.pattern)
			if result != tc.expected {
				t.Errorf("pathMatchesPattern(%q, %+v) = %v, want %v", 
					tc.path, tc.pattern, result, tc.expected)
			}
		})
	}
}

func TestRouteMatching(t *testing.T) {
	handler := &SubstrateHandler{}
	
	// Create a test watcher with routes and avoid patterns
	watcher := &Watcher{
		Order: &Order{
			Routes: []string{"/api/*", "/static/*", "/exact"},
			Avoid: []string{"/api/internal/*"},
		},
	}
	
	// Manually compile the patterns for testing
	watcher.Order.compiledRoutes = []*routePattern{
		{pattern: "/api/*", parts: []string{"/api/", ""}, hasStar: true},
		{pattern: "/static/*", parts: []string{"/static/", ""}, hasStar: true},
		{pattern: "/exact", parts: []string{"/exact"}, hasStar: false},
	}
	
	watcher.Order.compiledAvoid = []*routePattern{
		{pattern: "/api/internal/*", parts: []string{"/api/internal/", ""}, hasStar: true},
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Exact match",
			path:     "/exact",
			expected: true,
		},
		{
			name:     "Wildcard match",
			path:     "/api/users",
			expected: true,
		},
		{
			name:     "Avoid pattern takes precedence",
			path:     "/api/internal/secrets",
			expected: false,
		},
		{
			name:     "No match",
			path:     "/other/path",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := handler.matchRoute(tc.path, watcher)
			if result != tc.expected {
				t.Errorf("matchRoute(%q, watcher) = %v, want %v", 
					tc.path, result, tc.expected)
			}
		})
	}

	// Test with empty routes (should match everything except avoided paths)
	emptyRoutesWatcher := &Watcher{
		Order: &Order{
			Routes: []string{},
			Avoid: []string{"/private/*"},
			compiledRoutes: []*routePattern{},
			compiledAvoid: []*routePattern{
				{pattern: "/private/*", parts: []string{"/private/", ""}, hasStar: true},
			},
		},
	}

	t.Run("Empty routes matches everything except avoided", func(t *testing.T) {
		matched, _ := handler.matchRoute("/public/file", emptyRoutesWatcher)
		if !matched {
			t.Error("Should match path when routes is empty")
		}
		matched, _ = handler.matchRoute("/private/file", emptyRoutesWatcher)
		if matched {
			t.Error("Should not match avoided path")
		}
	})
	
	// Test route order matters (first match wins)
	orderedWatcher := &Watcher{
		Order: &Order{
			Routes: []string{"/content/*", "/content/special/*"},
			Avoid: []string{},
		},
	}
	
	orderedWatcher.Order.compiledRoutes = []*routePattern{
		{pattern: "/content/*", parts: []string{"/content/", ""}, hasStar: true},
		{pattern: "/content/special/*", parts: []string{"/content/special/", ""}, hasStar: true},
	}
	
	t.Run("First matching route wins", func(t *testing.T) {
		matched, _ := handler.matchRoute("/content/special/page", orderedWatcher)
		if !matched {
			t.Error("Should match path with first matching route")
		}
	})
	
	// Test avoid patterns override routes
	avoidOverrideWatcher := &Watcher{
		Order: &Order{
			Routes: []string{"/content/*"},
			Avoid: []string{"/content/private/*"},
		},
	}
	
	avoidOverrideWatcher.Order.compiledRoutes = []*routePattern{
		{pattern: "/content/*", parts: []string{"/content/", ""}, hasStar: true},
	}
	
	avoidOverrideWatcher.Order.compiledAvoid = []*routePattern{
		{pattern: "/content/private/*", parts: []string{"/content/private/", ""}, hasStar: true},
	}
	
	t.Run("Avoid patterns override routes", func(t *testing.T) {
		matched, _ := handler.matchRoute("/content/public/page", avoidOverrideWatcher)
		if !matched {
			t.Error("Should match non-avoided path")
		}
		matched, _ = handler.matchRoute("/content/private/page", avoidOverrideWatcher)
		if matched {
			t.Error("Should not match avoided path even if route matches")
		}
	})
}

func TestFilePatternMatching(t *testing.T) {
	// Create a test filesystem with some files
	testFS := fstest.MapFS{
		"index.md":      &fstest.MapFile{Data: []byte("index")},
		"about.md":      &fstest.MapFile{Data: []byte("about")},
		"blog/index.md": &fstest.MapFile{Data: []byte("blog index")},
		"blog/post1.md": &fstest.MapFile{Data: []byte("post1")},
		"docs/guide.md": &fstest.MapFile{Data: []byte("guide")},
	}

	handler := &SubstrateHandler{
		fs: testFS,
	}
	
	tests := []struct {
		name     string
		path     string
		pattern  *routePattern
		expected bool
	}{
		{
			name:    "Root path with index file",
			path:    "/",
			pattern: &routePattern{
				pattern: "/?.md", 
				fileMatchParts: []string{"/", ".md"}, 
				hasFileMatch: true,
			},
			expected: true,
		},
		{
			name:    "Path with existing file",
			path:    "/about",
			pattern: &routePattern{
				pattern: "/?.md", 
				fileMatchParts: []string{"/", ".md"}, 
				hasFileMatch: true,
			},
			expected: true,
		},
		{
			name:    "Path with exact file match",
			path:    "/about.md",
			pattern: &routePattern{
				pattern: "/?.md", 
				fileMatchParts: []string{"/", ".md"}, 
				hasFileMatch: true,
			},
			expected: true,
		},
		{
			name:    "Nested path with index file",
			path:    "/blog",
			pattern: &routePattern{
				pattern: "/blog/?.md", 
				fileMatchParts: []string{"/blog/", ".md"}, 
				hasFileMatch: true,
			},
			expected: true,
		},
		{
			name:    "Nested path with specific file",
			path:    "/blog/post1",
			pattern: &routePattern{
				pattern: "/blog/?.md", 
				fileMatchParts: []string{"/blog/", ".md"}, 
				hasFileMatch: true,
			},
			expected: true,
		},
		{
			name:    "Non-existent file",
			path:    "/nonexistent",
			pattern: &routePattern{
				pattern: "/?.md", 
				fileMatchParts: []string{"/", ".md"}, 
				hasFileMatch: true,
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.fileMatchesPattern(tc.path, tc.pattern)
			if result != tc.expected {
				t.Errorf("fileMatchesPattern(%q, %+v) = %v, want %v", 
					tc.path, tc.pattern, result, tc.expected)
			}
		})
	}
}
