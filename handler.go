package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

const (
	minRestartDelay   = 1 * time.Second
	maxRestartDelay   = 5 * time.Minute
	resetRestartDelay = 10 * time.Minute
)

func init() {
	caddy.RegisterModule(SubstrateHandler{})
}

// Interface guards
var (
	_ caddy.Module                = (*SubstrateHandler)(nil)
	_ caddy.Provisioner           = (*SubstrateHandler)(nil)
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*SubstrateHandler)(nil)
)

// Those come from the child process.
type Order struct {
	Host     string   `json:"host,omitempty"`
	TryFiles []string `json:"try_files,omitempty"`
	Match    []string `json:"match,omitempty"`
	CatchAll []string `json:"catch_all,omitempty"`
}

type outputTarget struct {
	// Type can be null, stdout, stderr, or file.
	Type string `json:"type,omitempty"`
	File string `json:"file,omitempty"`
}

type SubstrateHandler struct {
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	User           string            `json:"user,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	RedirectStdout *outputTarget     `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget     `json:"redirect_stderr,omitempty"`
	RestartPolicy  string            `json:"restart_policy,omitempty"`

	Order *Order `json:"-"`
	Key   string `json:"-"`

	cancel context.CancelFunc
	log    *zap.Logger
	app    *App
	fs     fs.FS
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) newCmd() *exec.Cmd {
	cmd := exec.Command(s.Command[0], s.Command[1:]...)
	configureSysProcAttr(cmd)

	env := os.Environ()
	for key, value := range s.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.app.Host, s.Key))
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

func (s *SubstrateHandler) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	delay := minRestartDelay
	var cmd *exec.Cmd

cmdLoop:
	for {
		cmd = s.newCmd()
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

func (s *SubstrateHandler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *SubstrateHandler) JSON(val any) json.RawMessage {
	out, err := json.Marshal(val)
	if err != nil {
		s.log.Warn("Error marshalling", zap.Error(err))
		return nil
	}
	return out
}

func (s *SubstrateHandler) UpdateOrder(order Order) {

	if len(order.TryFiles) > maxTryFiles {
		s.log.Error("Number of TryFiles exceeds maximum", zap.Int("count", len(order.TryFiles)),
			zap.Int("max", maxTryFiles))
	}
	if len(order.Match) > maxMatchExts {
		s.log.Error("Number of Matches exceeds maximum", zap.Int("count", len(order.Match)),
			zap.Int("max", maxMatchExts))
	}

	// Sort TryFiles by reverse length, then lexicographically
	sort.Slice(order.TryFiles, func(i, j int) bool {
		if len(order.TryFiles[i]) != len(order.TryFiles[j]) {
			return len(order.TryFiles[i]) > len(order.TryFiles[j])
		}
		return order.TryFiles[i] < order.TryFiles[j]
	})

	s.Order = &order
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s).With(zap.Strings("cmd", s.Command))

	fs, ok := ctx.Filesystems().Get("")
	if !ok {
		return fmt.Errorf("no filesystem available")
	}
	s.fs = fs

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}
	s.app = app.(*App)
	err = s.app.addSub(s)
	if err != nil {
		return err
	}

	return nil
}

