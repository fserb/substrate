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

type outputTarget struct {
	// Type can be null, stdout, stderr, or file.
	Type string `json:"type,omitempty"`
	File string `json:"file,omitempty"`
}

type execCmd struct {
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	User           string            `json:"user,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	RedirectStdout *outputTarget     `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget     `json:"redirect_stderr,omitempty"`
	RestartPolicy  string            `json:"restart_policy,omitempty"`

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
	env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.watcher.server.Host, s.watcher.key))
	u, err := user.Lookup(s.User)
	if err == nil {
		env = append(env, fmt.Sprintf("HOME=%s", u.HomeDir))
		env = append(env, fmt.Sprintf("USER=%s", u.Username))
	}
	cmd.Env = env

	cmd.Dir = s.Dir

	outFile, err := getRedirectFile(s.RedirectStdout, "stdout")
	if err != nil {
		s.log.Error("Error opening process stdout", zap.Error(err))
		outFile = nil
	}
	errFile, err := getRedirectFile(s.RedirectStderr, "stderr")
	if err != nil {
		s.log.Error("Error opening process stderr", zap.Error(err))
		errFile = nil
	}

	cmd.Stdout = outFile
	cmd.Stderr = errFile

	return cmd
}

func (s *execCmd) Run() {
	if s.cancel != nil {
		return
	}
	if s.Command == nil || len(s.Command) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	delay := minRestartDelay
	var cmd *exec.Cmd

cmdLoop:
	for {
		cmd = s.newExecCommand()
		s.log.Info("Starting command", zap.String("command", s.Command[0]))
		start := time.Now()

		if err := cmd.Start(); err != nil {
			s.log.Error("Failed to start command", zap.Error(err))
			break cmdLoop
		}

		errCh := make(chan error, 1)
		go func() { errCh <- cmd.Wait() }()

		select {
		case err := <-errCh:
			duration := time.Since(start)
			if err != nil {
				s.log.Error("Command exited with error", zap.Error(err))
			}
			if s.RestartPolicy == "never" || (s.RestartPolicy == "on_failure" && err == nil) {
				break cmdLoop
			}
			if err == nil || duration > resetRestartDelay {
				delay = minRestartDelay
			}
			s.log.Info("Restarting in", zap.Duration("delay", delay))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				break cmdLoop
			}
			delay *= 2
			if delay > maxRestartDelay {
				delay = maxRestartDelay
			}
		case <-ctx.Done():
			break cmdLoop
		}
	}

	s.cancel = nil

	if cmd == nil || cmd.Process == nil ||
		(cmd.ProcessState != nil && cmd.ProcessState.Exited()) {
		return
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		s.log.Error("Interrupt failed, killing process", zap.Error(err))
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
	case <-time.After(5 * time.Second):
		s.log.Error("Process did not exit in time, killing")
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

