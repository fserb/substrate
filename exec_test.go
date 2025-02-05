package substrate

import (
	"reflect"
	"testing"
)

func TestOrderSubmit(t *testing.T) {
	order := &Order{
		Match: []string{
			"/foo/*.txt", "/foo/*.md", "/bar/*.log", "/baz/*.json",
			"*.js", "/*.gif",
			".gif", "/a", "", "/",
		},
	}

	cmd := &execCmd{}
	order.Submit(cmd)

	// Verify matchers
	expectedMatchers := []orderMatcher{
		{"/bar/", ".log"},
		{"/baz/", ".json"},
		{"/foo/", ".txt"},
		{"/foo/", ".md"},
		{"/", ".gif"},
		{"/", ".js"},
	}

	if !reflect.DeepEqual(order.matchers, expectedMatchers) {
		t.Errorf("Expected matchers: %+v, got: %+v", expectedMatchers, order.matchers)
	}

	if cmd.Order != order {
		t.Errorf("Expected cmd.Order to be set, got nil")
	}
}

