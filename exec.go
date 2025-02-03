package substrate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

type Order struct {
	Host     string   `json:"host,omitempty"`
	TryFiles []string `json:"try_files,omitempty"`
	Match    []string `json:"match,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	CatchAll []string `json:"catch_all,omitempty"`
}

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

	Order *Order `json:"-"`

	key    string
	cancel context.CancelFunc
	host   string
	log    *zap.Logger
}

var (
	_ caddy.Destructor = (*execCmd)(nil)
)

func (s *execCmd) UpdateOrder(order Order) {
	// Sort TryFiles by reverse length, then lexicographically
	sort.Slice(order.TryFiles, func(i, j int) bool {
		if len(order.TryFiles[i]) != len(order.TryFiles[j]) {
			return len(order.TryFiles[i]) > len(order.TryFiles[j])
		}
		return order.TryFiles[i] < order.TryFiles[j]
	})

	s.Order = &order
}

// After you create an execCmd, we check if there's one with the same hash
// in store. If there is, we replace your cmd with the live one.
func (s *execCmd) Register(app *App) *execCmd {
	s.log = app.log.With(zap.String("key", s.Key()))
	return app.registerCmd(s)
}

func (s *execCmd) Key() string {
	if s.key != "" {
		return s.key
	}

	out, err := json.Marshal(s)
	if err != nil {
		s.log.Error("Failed to marshal substrate", zap.Error(err))
		return ""
	}

	hash := sha1.Sum(append(salt, out...))
	s.key = hex.EncodeToString(hash[:])
	return s.key
}

func (s *execCmd) newExecCommand() *exec.Cmd {
	cmd := exec.Command(s.Command[0], s.Command[1:]...)
	configureSysProcAttr(cmd)

	env := os.Environ()
	for key, value := range s.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.host, s.Key()))
	cmd.Env = env

	configureExecutingUser(cmd, s.User)

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

