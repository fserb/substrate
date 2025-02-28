package substrate

import (
	"crypto/rand"
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

type App struct {
	Env            map[string]string `json:"env,omitempty"`
	RestartPolicy  string            `json:"restart_policy,omitempty"`
	RedirectStdout *outputTarget     `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget     `json:"redirect_stderr,omitempty"`

	log   *zap.Logger
	mutex sync.Mutex
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

func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "substrate",
		New: func() caddy.Module { return new(App) },
	}
}

func (h *App) Provision(ctx caddy.Context) error {
	if h.log == nil {
		h.log = ctx.Logger(h)
	}
	h.log.Info("Provisioning substrate")

	// Get the server from the pool or create a new one
	obj, _ := pool.LoadOrStore("server", &Server{})
	server := obj.(*Server)
	server.log = h.log.Named("substrate server")
	server.app = h

	server.Start()

	return nil
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")

	// Get the server and wait for it to start
	obj, _ := pool.LoadOrStore("server", &Server{})
	server := obj.(*Server)
	server.WaitForStart(h)

	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")

	// Get the server before deleting it from the pool
	obj, loaded := pool.LoadOrStore("server", nil)
	if loaded && obj != nil {
		server, ok := obj.(*Server)
		if ok && server != nil {
			server.Stop()
		}
	}

	// Clean up the server from the pool
	pool.Delete("server")

	// Clean up all watchers in the pool
	watcherPool.Range(func(key, value any) bool {
		if value != nil {
			watcher, ok := value.(*Watcher)
			if ok && watcher != nil {
				watcher.Close()
			}
		}
		watcherPool.Delete(key)
		return true
	})

	return nil
}
