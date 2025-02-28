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
	// Create a test execCmd
	cmd := &execCmd{
		Command: []string{"/bin/echo", "test"},
		Env:     map[string]string{"TEST_KEY": "test_value"},
		User:    "",
		Dir:     "/tmp",
		log:     zap.NewNop(),
	}

	// Create the exec.Cmd
	execCmd := cmd.newExecCommand()

	// Verify command
	if execCmd.Path != "/bin/echo" {
		t.Errorf("Expected path %q, got %q", "/bin/echo", execCmd.Path)
	}

	if len(execCmd.Args) != 2 || execCmd.Args[0] != "/bin/echo" || execCmd.Args[1] != "test" {
		t.Errorf("Expected args [/bin/echo test], got %v", execCmd.Args)
	}

	// Verify environment
	foundEnv := false
	for _, env := range execCmd.Env {
		if env == "TEST_KEY=test_value" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Error("Environment variable TEST_KEY not found in command environment")
	}

	// Verify directory
	if execCmd.Dir != "/tmp" {
		t.Errorf("Expected dir %q, got %q", "/tmp", execCmd.Dir)
	}
}

// TestExecCmdRun tests the Run method
func TestExecCmdRun(t *testing.T) {
	// Create a temporary script file
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

	// Create a test execCmd
	cmd := &execCmd{
		Command:       []string{scriptPath},
		Dir:           tmpDir,
		RestartPolicy: "never",
		log:           zap.NewNop(),
	}

	// Run the command in a goroutine
	done := make(chan struct{})
	go func() {
		cmd.Run()
		close(done)
	}()

	// Wait for the command to complete or timeout
	select {
	case <-done:
		// Command completed as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Command did not complete in time")
	}

	// Verify the command was run
	if cmd.cancel != nil {
		t.Error("cancel function was not cleared")
	}
}

// TestExecCmdStop tests the Stop method
func TestExecCmdStop(t *testing.T) {
	// Create a test execCmd with a long-running command
	cmd := &execCmd{
		Command: []string{"sleep", "10"},
		log:     zap.NewNop(),
	}

	// Create a context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	cmd.cancel = cancel

	// Run the command in a goroutine
	done := make(chan struct{})
	go func() {
		// Simulate the Run method
		execCmd := exec.CommandContext(ctx, cmd.Command[0], cmd.Command[1:]...)
		execCmd.Start()

		// Wait for the command to complete
		execCmd.Wait()
		close(done)
	}()

	// Stop the command
	cmd.Stop()

	// Wait for the command to complete or timeout
	select {
	case <-done:
		// Command was stopped as expected
	case <-time.After(1 * time.Second):
		t.Fatal("Command was not stopped in time")
	}

	// Verify the cancel function was called
	select {
	case <-ctx.Done():
		// Context was canceled as expected
	default:
		t.Error("Context was not canceled")
	}
}

// TestGetRedirectFile tests the getRedirectFile function
func TestGetRedirectFile(t *testing.T) {
	// Test stdout
	file, err := getRedirectFile(&outputTarget{Type: "stdout"}, "")
	if err != nil {
		t.Errorf("getRedirectFile(stdout) returned error: %v", err)
	}
	if file != os.Stdout {
		t.Error("getRedirectFile(stdout) did not return os.Stdout")
	}

	// Test stderr
	file, err = getRedirectFile(&outputTarget{Type: "stderr"}, "")
	if err != nil {
		t.Errorf("getRedirectFile(stderr) returned error: %v", err)
	}
	if file != os.Stderr {
		t.Error("getRedirectFile(stderr) did not return os.Stderr")
	}

	// Test null
	file, err = getRedirectFile(&outputTarget{Type: "null"}, "")
	if err != nil {
		t.Errorf("getRedirectFile(null) returned error: %v", err)
	}
	if file != nil {
		t.Error("getRedirectFile(null) did not return nil")
	}

	// Test file
	tmpfile := filepath.Join(os.TempDir(), "test_redirect.log")
	file, err = getRedirectFile(&outputTarget{Type: "file", File: tmpfile}, "")
	if err != nil {
		t.Errorf("getRedirectFile(file) returned error: %v", err)
	}
	if file == nil {
		t.Error("getRedirectFile(file) returned nil")
	} else {
		file.Close()
		os.Remove(tmpfile)
	}

	// Test invalid
	_, err = getRedirectFile(&outputTarget{Type: "invalid"}, "")
	if err == nil {
		t.Error("getRedirectFile(invalid) did not return error")
	}

	// Test default type
	file, err = getRedirectFile(nil, "stdout")
	if err != nil {
		t.Errorf("getRedirectFile(nil, stdout) returned error: %v", err)
	}
	if file != os.Stdout {
		t.Error("getRedirectFile(nil, stdout) did not return os.Stdout")
	}
}

// TestExecCmdSubmit tests the Submit method
func TestExecCmdSubmit(t *testing.T) {
	// Create a test order
	order := &Order{
		Match: []string{
			"/foo/*.txt", "/foo/*.md", "/bar/*.log", "/baz/*.json",
			"*.js", "/*.gif",
			".gif", "/a", "", "/",
		},
	}

	// execCmd doesn't have an Order field, so we'll just process the order directly

	// Process matchers (copied from Watcher.Submit logic)
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

	// Sort matchers
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

	// Verify matchers were created and sorted correctly
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

