package substrate

import (
	"bufio"
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
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	User    string            `json:"user,omitempty"`
	Dir     string            `json:"dir,omitempty"`

	cancel  context.CancelFunc
	log     *zap.Logger
	watcher *Watcher
}

var (
	_ caddy.Destructor = (*execCmd)(nil)
)

// newExecCommand creates and configures an exec.Cmd instance.
func (s *execCmd) newExecCommand() *exec.Cmd {
	cmd := exec.Command(s.Command[0], s.Command[1:]...)
	configureSysProcAttr(cmd)
	configureExecutingUser(cmd, s.User)

	env := os.Environ()
	for key, value := range s.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
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

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		if s.log != nil {
			s.log.Error("Error creating stdout pipe", zap.Error(err))
		}
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		if s.log != nil {
			s.log.Error("Error creating stderr pipe", zap.Error(err))
		}
	}

	if s.watcher != nil && s.watcher.app != nil && stdoutPipe != nil {
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				line := scanner.Text()
				s.watcher.WriteStatusLog("S", line)
			}
		}()
	}

	if s.watcher != nil && s.watcher.app != nil && stderrPipe != nil {
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				s.watcher.WriteStatusLog("E", line)
			}
		}()
	}

	return cmd
}

// Run executes the command with automatic restart capabilities.
// It implements an exponential backoff strategy for restarts and
// handles graceful termination of processes. The function continuously
// monitors the command's execution, restarting it when it exits and
// managing the restart delay based on execution duration.
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

	if s.watcher != nil && s.watcher.app != nil {
		s.watcher.WriteStatusLog("A", "Starting command")
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
			if s.watcher != nil && s.watcher.app != nil {
				s.watcher.WriteStatusLog("A", fmt.Sprintf("Failed to start command: %v", err))
			}
			break cmdLoop
		}

		if s.watcher != nil && s.watcher.app != nil {
			s.watcher.WriteStatusLog("A", "Command started successfully")
		}

		errCh := make(chan error, 1)
		go func() { errCh <- cmd.Wait() }()

		cancelctx, cancel := context.WithCancel(outerctx)

		select {
		case err := <-errCh:
			cancel()
			duration := time.Since(start)
			if err != nil {
				cmdLogger.Error("Command exited with error", zap.Error(err))
				if s.watcher != nil && s.watcher.app != nil {
					s.watcher.WriteStatusLog("A", fmt.Sprintf("Command exited with error: %v", err))
				}
			} else if s.watcher != nil && s.watcher.app != nil {
				s.watcher.WriteStatusLog("A", "Command completed successfully")
			}

			if err == nil || duration > resetRestartDelay {
				delay = minRestartDelay
			}

			cmdLogger.Info("Restarting command", zap.Duration("delay", delay))
			if s.watcher != nil && s.watcher.app != nil {
				s.watcher.WriteStatusLog("A", fmt.Sprintf("Restarting command in %v", delay))
			}

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
			if s.watcher != nil && s.watcher.app != nil {
				s.watcher.WriteStatusLog("A", "Command cancelled")
			}
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
	if s.watcher != nil && s.watcher.app != nil {
		s.watcher.WriteStatusLog("A", "Sending interrupt signal to process")
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		logger.Error("Interrupt failed, killing process", zap.Error(err))
		if s.watcher != nil && s.watcher.app != nil {
			s.watcher.WriteStatusLog("A", fmt.Sprintf("Interrupt failed, killing process: %v", err))
		}
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
		if s.watcher != nil && s.watcher.app != nil {
			s.watcher.WriteStatusLog("A", "Process exited gracefully")
		}
	case <-time.After(5 * time.Second):
		logger.Error("Process did not exit in time, killing")
		if s.watcher != nil && s.watcher.app != nil {
			s.watcher.WriteStatusLog("A", "Process did not exit in time, killing")
		}
		cmd.Process.Kill()
	}
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
