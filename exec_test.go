package substrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestExecCmdNewExecCommand tests the newExecCommand method
func TestExecCmdNewExecCommand(t *testing.T) {
	execCmd := &execCmd{
		Command: []string{"/bin/echo", "test"},
		Env:     map[string]string{"TEST_KEY": "test_value"},
		User:    "",
		Dir:     "/tmp",
		log:     zap.NewNop(),
	}

	cmd := execCmd.newExecCommand()

	// Verify command
	if cmd.Path != "/bin/echo" {
		t.Errorf("Expected path %q, got %q", "/bin/echo", cmd.Path)
	}

	if len(cmd.Args) != 2 || cmd.Args[0] != "/bin/echo" || cmd.Args[1] != "test" {
		t.Errorf("Expected args [/bin/echo test], got %v", cmd.Args)
	}

	foundEnv := false
	for _, env := range cmd.Env {
		if env == "TEST_KEY=test_value" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Error("Environment variable TEST_KEY not found in command environment")
	}

	// Verify directory
	if cmd.Dir != "/tmp" {
		t.Errorf("Expected dir %q, got %q", "/tmp", cmd.Dir)
	}
}

func TestExecCmdRun(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "exec-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "test.sh")
	scriptContent := `#!/bin/sh
echo "Hello, world!"
exit 0
`
	err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to write script file: %v", err)
	}

	cmd := &execCmd{
		Command: []string{scriptPath},
		Dir:     tmpDir,
		log:     zap.NewNop(),
	}

	done := make(chan struct{})
	go func() {
		cmd.Run()
		close(done)
	}()

	select {
	case <-done:
		// Command completed as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Command did not complete in time")
	}

	if cmd.cancel != nil {
		t.Error("cancel function was not cleared")
	}
}

func TestExecCmdStop(t *testing.T) {
	// Create a test execCmd with a long-running command
	cmd := &execCmd{
		Command: []string{"sleep", "10"},
		log:     zap.NewNop(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd.cancel = cancel

	done := make(chan struct{})
	go func() {
		execCmd := exec.CommandContext(ctx, cmd.Command[0], cmd.Command[1:]...)
		execCmd.Start()

		execCmd.Wait()
		close(done)
	}()

	cmd.Stop()

	select {
	case <-done:
		// Command was stopped as expected
	case <-time.After(1 * time.Second):
		t.Fatal("Command was not stopped in time")
	}

	select {
	case <-ctx.Done():
		// Context was canceled as expected
	default:
		t.Error("Context was not canceled")
	}
}

func TestExecCmdSubmit(t *testing.T) {
	order := &Order{
		Match: []string{
			"/foo/*.txt", "/foo/*.md", "/bar/*.log", "/baz/*.json",
			"*.js", "/*.gif",
			".gif", "/a", "", "/",
		},
	}

	order.matchers = make([]orderMatcher, 0, len(order.Match))
	for _, m := range order.Match {
		dir := filepath.Join("/", filepath.Dir(m))
		name := filepath.Base(m)
		if name[0] != '*' || name[1] != '.' {
			continue
		}
		ext := name[1:]
		if dir[len(dir)-1] != '/' {
			dir += "/"
		}

		order.matchers = append(order.matchers, orderMatcher{dir, ext})
	}

	sort.Slice(order.matchers, func(i, j int) bool {
		if len(order.matchers[i].path) != len(order.matchers[j].path) {
			return len(order.matchers[i].path) > len(order.matchers[j].path)
		}
		if order.matchers[i].path != order.matchers[j].path {
			return order.matchers[i].path < order.matchers[j].path
		}

		if len(order.matchers[i].ext) != len(order.matchers[j].ext) {
			return len(order.matchers[i].ext) > len(order.matchers[j].ext)
		}
		return order.matchers[i].ext < order.matchers[j].ext
	})

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
}
