package substrate

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"math/big"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
)

var (
	salt []byte
	pool *caddy.UsagePool
)

func init() {
	pool = caddy.NewUsagePool()

	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("substrate", parseGlobalSubstrate)

	// set up salt
	bi, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		log.Fatalln(err)
	}
	salt = []byte(hex.EncodeToString(bi.Bytes()) + ":")
}

// Interface guards
var (
	_ caddy.Module      = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
	_ caddy.App         = (*App)(nil)
)

// outputTarget defines where command output should be directed
type outputTarget struct {
	// Type can be "null", "stdout", "stderr", or "file"
	Type string `json:"type,omitempty"`
	// File is the path to write output to when Type is "file"
	File string `json:"file,omitempty"`
}

// App is the main substrate application that manages the substrate server
// and provides configuration for substrate processes.
type App struct {
	Env map[string]string `json:"env,omitempty"`
	// How processes are restarted: "always", "never", "on_failure"
	RestartPolicy  string        `json:"restart_policy,omitempty"`
	RedirectStdout *outputTarget `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget `json:"redirect_stderr,omitempty"`

	log    *zap.Logger
	mutex  sync.Mutex
	server *Server
}

func parseGlobalSubstrate(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}

	cur, ok := existingVal.(*App)
	if ok {
		*app = *cur
	}

	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "env":
				var envKey, envValue string
				if !d.Args(&envKey, &envValue) {
					return nil, d.ArgErr()
				}
				if app.Env == nil {
					app.Env = map[string]string{}
				}
				app.Env[envKey] = envValue
			case "restart_policy":
				var p string
				if !d.Args(&p) {
					return nil, d.ArgErr()
				}
				if p != "always" && p != "never" && p != "on_failure" {
					return nil, fmt.Errorf("Invalid restart policy: %s", p)
				}
				app.RestartPolicy = p
			case "redirect_stdout":
				target, err := parseRedirectGlobal(d)
				if err != nil {
					return nil, err
				}
				app.RedirectStdout = target
			case "redirect_stderr":
				target, err := parseRedirectGlobal(d)
				if err != nil {
					return nil, err
				}
				app.RedirectStderr = target
			}
		}
	}

	return httpcaddyfile.App{
		Name:  "substrate",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

func parseRedirectGlobal(d *caddyfile.Dispenser) (*outputTarget, error) {
	if !d.NextArg() {
		return nil, d.ArgErr()
	}

	var target outputTarget
	target.Type = d.Val()

	switch target.Type {
	case "stdout", "null", "stderr":
		return &target, nil
	case "file":
		if !d.NextArg() {
			return nil, d.ArgErr()
		}
		target.File = d.Val()
		return &target, nil
	}

	return nil, fmt.Errorf("Invalid redirect target: %s", target.Type)
}

func (h App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "substrate",
		New: func() caddy.Module { return new(App) },
	}
}

func (h *App) Provision(ctx caddy.Context) error {
	if h.log == nil {
		h.log = ctx.Logger(h)
	}

	return nil
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")

	// Get the server and wait for it to start
	obj, loaded := pool.LoadOrStore("server", &Server{})
	h.server = obj.(*Server)
	if !loaded {
		h.server.log = h.log.Named("substrate server")
		h.server.app = h
		h.server.Start()
	}

	h.server.WaitForStart(h)

	for _, watcher := range h.server.watchers {
		watcher.Close()
	}
	h.server.watchers = make(map[string]*Watcher)

	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")
	pool.Delete("server")
	return nil
}

func (h *App) GetWatcher(root string) *Watcher {
	hash := sha1.Sum(append(salt, []byte(root)...))
	key := hex.EncodeToString(hash[:])

	got, ok := h.server.watchers[key]
	if ok {
		return got
	}

	watcher := &Watcher{
		Root:   root,
		app:    h,
		log:    h.log.With(zap.String("root", root)),
		suburl: fmt.Sprintf("%s/%s", h.server.Host, key),
	}

	if err := watcher.init(); err != nil {
		h.log.Error("Failed to initialize watcher", zap.Error(err))
		return nil
	}

	h.server.watchers[key] = watcher
	return watcher
}
