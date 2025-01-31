package substrate

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
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
	_ caddyhttp.MiddlewareHandler = (*SubstrateHandler)(nil)
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
)

type Order struct {
	Host     string   `json:"host,omitempty"`
	TryFiles []string `json:"try_files,omitempty"`
	Match    []string `json:"match,omitempty"`
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

	N int `json:"n,omitempty"`

	Order *Order `json:"-"`

	rph         *reverseproxy.Handler
	keepRunning bool
	cmd         *exec.Cmd
	log         *zap.Logger
	app         *App
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) Key() string {
	out, err := json.Marshal(s)
	if err != nil {
		s.log.Error("Error marshalling child", zap.Error(err))
		return ""
	}

	hash := sha1.Sum(out)
	return hex.EncodeToString(hash[:])
}

func (s *SubstrateHandler) Run() {
	s.keepRunning = true
	restartDelay := minRestartDelay

	// Originally based on candy-supervisor
	for s.keepRunning {
		s.cmd = exec.Command(s.Command[0], s.Command[1:]...)
		configureSysProcAttr(s.cmd)

		env := os.Environ()
		for key, value := range s.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.app.Host, s.Key()))
		s.cmd.Env = env

		configureExecutingUser(s.cmd, s.User)

		s.cmd.Dir = s.Dir

		// TODO: stdout stderr
		var openFiles []*os.File

		outFile, err := getRedirectFile(s.RedirectStdout)
		if err != nil {
			s.log.Error("Error opening process stdout", zap.Error(err))
			outFile = nil
		}
		errFile, err := getRedirectFile(s.RedirectStderr)
		if err != nil {
			s.log.Error("Error opening process stderr", zap.Error(err))
			errFile = nil
		}

		s.cmd.Stdout = outFile
		s.cmd.Stderr = errFile
		openFiles = append(openFiles, outFile, errFile)

		start := time.Now()
		err = s.cmd.Start()

		if err != nil {
			s.log.Error("Error starting command", zap.Error(err))
			break
		}

		s.log.Info("Started command", zap.String("command", s.Command[0]), zap.Int("pid", s.cmd.Process.Pid))

		err = s.cmd.Wait()

		duration := time.Now().Sub(start)

		for _, f := range openFiles {
			if f != nil && f != os.Stdout && f != os.Stderr {
				f.Close()
			}
		}

		if err != nil {
			s.log.Error("Process exited", zap.Error(err))
		}

		if s.RestartPolicy == "never" {
			break
		}

		if s.RestartPolicy == "on_failure" && err == nil {
			break
		}

		if err == nil || duration > resetRestartDelay {
			restartDelay = minRestartDelay
		}
		if err != nil && s.keepRunning {
			s.log.Info("Restarting in", zap.Duration("delay", restartDelay))
			time.Sleep(restartDelay)
			restartDelay = 2 * restartDelay
			if restartDelay > maxRestartDelay {
				restartDelay = maxRestartDelay
			}
		}
	}
}

func getRedirectFile(target *outputTarget) (*os.File, error) {
	switch target.Type {
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
	s.keepRunning = false
	if !s.cmdRunning() {
		return
	}

	err := s.cmd.Process.Signal(os.Interrupt)
	if err != nil {
		s.log.Error("Error sending interrupt signal, killing.")
		s.cmd.Process.Kill()
		return
	}

	go func() {
		time.Sleep(5 * time.Second)
		if s.cmdRunning() {
			s.log.Error("Process did not respond to interrupt, killing")
			s.cmd.Process.Kill()
		}
	}()
	s.cmd.Wait()
}

func (s *SubstrateHandler) cmdRunning() bool {
	return s.cmd != nil && s.cmd.Process != nil && (s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited())
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
	s.Order = &order
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s.Order == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		s.log.Error("No order")
		return nil
	}

	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	repl.Map(func(key string) (any, bool) {

		if key == "substrate.host" {
			return s.Order.Host, true
		}

		var outmap *[]string
		if strings.HasPrefix(key, "substrate.match_files.") {
			outmap = &s.Order.TryFiles
			key = key[22:]
		} else if strings.HasPrefix(key, "substrate.match_path.") {
			outmap = &s.Order.Match
			key = key[21:]
		}

		if outmap == nil {
			return nil, false
		}
		number, err := strconv.Atoi(key)

		if err != nil || number < 0 || number >= len(*outmap) {
			return nil, false
		}
		return (*outmap)[number], true
	})

	return next.ServeHTTP(w, r)
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s).With(zap.Strings("cmd", s.Command))

	bi, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt32))
	if err != nil {
		return err
	}
	s.N = int(bi.Int64())

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}
	s.app = app.(*App)
	s.app.Substrates[s.Key()] = s

	return nil
}

