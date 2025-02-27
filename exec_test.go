package substrate

import (
	"reflect"
	"testing"
)

func TestExecCmdSubmit(t *testing.T) {
	order := &Order{
		Match: []string{
			"/foo/*.txt", "/foo/*.md", "/bar/*.log", "/baz/*.json",
			"*.js", "/*.gif",
			".gif", "/a", "", "/",
		},
	}

	cmd := &execCmd{}
	cmd.Submit(order)

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

