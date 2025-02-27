package substrate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

type orderMatcher struct {
	path string
	ext  string
}

type Order struct {
	Host     string   `json:"host,omitempty"`
	Match    []string `json:"match,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	CatchAll []string `json:"catch_all,omitempty"`

	matchers []orderMatcher `json:"-"`
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
	Prefix         string            `json:"prefix,omitempty"`
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

func (s *execCmd) Submit(o *Order) {
	o.matchers = make([]orderMatcher, 0, len(o.Match))
	for _, m := range o.Match {
		dir := filepath.Join("/", filepath.Dir(m))
		name := filepath.Base(m)
		if name[0] != '*' || name[1] != '.' {
			continue
		}
		ext := name[1:]
		if dir[len(dir)-1] != '/' {
			dir += "/"
		}

		o.matchers = append(o.matchers, orderMatcher{dir, ext})
	}

	sort.Slice(o.matchers, func(i, j int) bool {
		if len(o.matchers[i].path) != len(o.matchers[j].path) {
			return len(o.matchers[i].path) > len(o.matchers[j].path)
		}
		if o.matchers[i].path != o.matchers[j].path {
			return o.matchers[i].path < o.matchers[j].path
		}

		if len(o.matchers[i].ext) != len(o.matchers[j].ext) {
			return len(o.matchers[i].ext) > len(o.matchers[j].ext)
		}
		return o.matchers[i].ext < o.matchers[j].ext
	})

	sort.Slice(o.CatchAll, func(i, j int) bool {
		if len(o.CatchAll[i]) != len(o.CatchAll[j]) {
			return len(o.CatchAll[i]) > len(o.CatchAll[j])
		}
		return o.CatchAll[i] < o.CatchAll[j]
	})

	s.Order = o
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

	configureExecutingUser(cmd, s.User)

	env := os.Environ()
	for key, value := range s.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.host, s.Key()))
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

