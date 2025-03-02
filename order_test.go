package substrate

import (
	"reflect"
	"sort"
	"testing"

	"go.uber.org/zap"
)

func TestOrderMatcherSorting(t *testing.T) {
	matchers := []orderMatcher{
		{"/", ".js"},
		{"/foo/", ".md"},
		{"/foo/", ".txt"},
		{"/bar/", ".log"},
		{"/baz/", ".json"},
		{"/", ".gif"},
	}

	expectedOrder := []orderMatcher{
		{"/bar/", ".log"},
		{"/baz/", ".json"},
		{"/foo/", ".txt"},
		{"/foo/", ".md"},
		{"/", ".gif"},
		{"/", ".js"},
	}

	// Sort using the same logic as in watcher.go
	sort.Slice(matchers, func(i, j int) bool {
		if len(matchers[i].path) != len(matchers[j].path) {
			return len(matchers[i].path) > len(matchers[j].path)
		}
		if matchers[i].path != matchers[j].path {
			return matchers[i].path < matchers[j].path
		}

		if len(matchers[i].ext) != len(matchers[j].ext) {
			return len(matchers[i].ext) > len(matchers[j].ext)
		}
		return matchers[i].ext < matchers[j].ext
	})

	if !reflect.DeepEqual(matchers, expectedOrder) {
		t.Errorf("Matcher sorting failed.\nExpected: %v\nGot: %v", expectedOrder, matchers)
	}
}

func TestOrderProcessMatchers(t *testing.T) {
	watcher := &Watcher{
		log: zap.NewNop(),
	}

	tests := []struct {
		name     string
		patterns []string
		expected []orderMatcher
	}{
		{
			name:     "Empty patterns",
			patterns: []string{},
			expected: nil,
		},
		{
			name:     "Valid patterns",
			patterns: []string{"/foo/*.txt", "/bar/*.log", "/*.html"},
			expected: []orderMatcher{
				{"/bar/", ".log"},
				{"/foo/", ".txt"},
				{"/", ".html"},
			},
		},
		{
			name:     "Invalid patterns are skipped",
			patterns: []string{"/foo/*.txt", "invalid", "no-asterisk.html", "/bar/*.log"},
			expected: []orderMatcher{
				{"/bar/", ".log"},
				{"/foo/", ".txt"},
			},
		},
		{
			name:     "Nested directories",
			patterns: []string{"/foo/bar/*.txt", "/foo/*.md", "/*.html"},
			expected: []orderMatcher{
				{"/foo/bar/", ".txt"},
				{"/foo/", ".md"},
				{"/", ".html"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := watcher.processMatchers(tc.patterns)

			if len(result) != len(tc.expected) {
				t.Fatalf("Expected %d matchers, got %d", len(tc.expected), len(result))
			}

			for i, expected := range tc.expected {
				if result[i].path != expected.path || result[i].ext != expected.ext {
					t.Errorf("Matcher %d: expected {%s, %s}, got {%s, %s}",
						i, expected.path, expected.ext, result[i].path, result[i].ext)
				}
			}
		})
	}
}

func TestOrderCatchAllSorting(t *testing.T) {
	order := &Order{
		CatchAll: []string{
			"/index.html",
			"/foo/index.html",
			"/foo/bar/index.html",
			"/404.html",
		},
	}

	watcher := &Watcher{
		log: zap.NewNop(),
	}
	watcher.Submit(order)

	expected := []string{
		"/foo/bar/index.html",
		"/foo/index.html",
		"/index.html",
		"/404.html",
	}

	if !reflect.DeepEqual(order.CatchAll, expected) {
		t.Errorf("CatchAll sorting failed.\nExpected: %v\nGot: %v", expected, order.CatchAll)
	}
}
