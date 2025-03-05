package substrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

// execCmd represents a command to be executed by the substrate system
type execCmd struct {
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	User           string            `json:"user,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	RedirectStdout *outputTarget     `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget     `json:"redirect_stderr,omitempty"`
	// how the command is restarted: "always", "never", "on_failure"
	RestartPolicy string `json:"restart_policy,omitempty"`

	cancel  context.CancelFunc
	log     *zap.Logger
	watcher *Watcher
}

var (
	_ caddy.Destructor = (*execCmd)(nil)
)

func (s *execCmd) newExecCommand() *exec.Cmd {
	cmd := exec.Command(s.Command[0], s.Command[1:]...)
	configureSysProcAttr(cmd)

	configureExecutingUser(cmd, s.User)

	env := os.Environ()
	for key, value := range s.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Check if watcher and server are not nil before accessing
	if s.watcher != nil && s.watcher.app.server != nil {
		env = append(env, fmt.Sprintf("SUBSTRATE=%s", s.watcher.suburl))
	}

	if s.User != "" {
		u, err := user.Lookup(s.User)
		if err == nil {
			env = append(env, fmt.Sprintf("HOME=%s", u.HomeDir))
			env = append(env, fmt.Sprintf("USER=%s", u.Username))
		}
	}
	cmd.Env = env

	cmd.Dir = s.Dir

	outFile, err := getRedirectFile(s.RedirectStdout, "stdout")
	if err != nil {
		if s.log != nil {
			s.log.Error("Error opening process stdout", zap.Error(err))
		}
		outFile = nil
	}
	errFile, err := getRedirectFile(s.RedirectStderr, "stderr")
	if err != nil {
		if s.log != nil {
			s.log.Error("Error opening process stderr", zap.Error(err))
		}
		errFile = nil
	}

	cmd.Stdout = outFile
	cmd.Stderr = errFile

	return cmd
}

// Run executes the command with proper restart policy handling
// It manages the lifecycle of the process according to the configured restart policy
func (s *execCmd) Run() {
	if s.cancel != nil {
		return
	}
	logger := s.log
	if logger == nil {
		logger = zap.NewNop()
	}

	if s.Command == nil || len(s.Command) == 0 {
		logger.Error("Cannot run empty command")
		return
	}

	outerctx, ocancel := context.WithCancel(context.Background())
	s.cancel = ocancel
	delay := minRestartDelay
	var cmd *exec.Cmd

cmdLoop:
	for {
		cmd = s.newExecCommand()
		cmdLogger := logger.With(
			zap.String("command", s.Command[0]),
			zap.Strings("args", s.Command[1:]),
			zap.String("dir", s.Dir),
		)

		cmdLogger.Info("Starting command")
		start := time.Now()

		if err := cmd.Start(); err != nil {
			cmdLogger.Error("Failed to start command", zap.Error(err))
			break cmdLoop
		}

		errCh := make(chan error, 1)
		go func() { errCh <- cmd.Wait() }()

		cancelctx, cancel := context.WithCancel(outerctx)

		// Wait for command completion or cancellation
		select {
		case err := <-errCh:
			cancel()
			duration := time.Since(start)
			if err != nil {
				cmdLogger.Error("Command exited with error", zap.Error(err))
			}

			if s.RestartPolicy == "never" || (s.RestartPolicy == "on_failure" && err == nil) {
				break cmdLoop
			}

			if err == nil || duration > resetRestartDelay {
				delay = minRestartDelay
			}

			cmdLogger.Info("Restarting command", zap.Duration("delay", delay))

			select {
			case <-time.After(delay):
			case <-cancelctx.Done():
				break cmdLoop
			}

			delay *= 2
			if delay > maxRestartDelay {
				delay = maxRestartDelay
			}

		case <-cancelctx.Done():
			cmdLogger.Info("Command cancelled")
			cancel()
			break cmdLoop
		}
	}

	s.cancel = nil

	if cmd == nil || cmd.Process == nil ||
		(cmd.ProcessState != nil && cmd.ProcessState.Exited()) {
		return
	}

	logger.Info("Sending interrupt signal to process")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		logger.Error("Interrupt failed, killing process", zap.Error(err))
		cmd.Process.Kill()
		return
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("Process exited gracefully")
	case <-time.After(5 * time.Second):
		logger.Error("Process did not exit in time, killing")
		cmd.Process.Kill()
	}
}

func getRedirectFile(target *outputTarget, default_type string) (*os.File, error) {
	t := default_type
	if target != nil {
		t = target.Type
	}

	switch t {
	case "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "null":
		return nil, nil
	case "file":
		return os.OpenFile(target.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	}
	return nil, fmt.Errorf("Invalid redirect target: %s", target.Type)
}

func (s *execCmd) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *execCmd) Destruct() error {
	s.Stop()
	return nil
}
