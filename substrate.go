package substrate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"math/big"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
)

var (
	salt []byte
	pool *caddy.UsagePool
	cmds *caddy.UsagePool
)

func init() {
	pool = caddy.NewUsagePool()
	cmds = caddy.NewUsagePool()

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
	cmds           map[string]*execCmd
	server         *Server
	log            *zap.Logger
	Env            map[string]string `json:"env,omitempty"`
	RestartPolicy  string            `json:"restart_policy,omitempty"`
	RedirectStdout *outputTarget     `json:"redirect_stdout,omitempty"`
	RedirectStderr *outputTarget     `json:"redirect_stderr,omitempty"`
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

	h.cmds = make(map[string]*execCmd)

	obj, _ := pool.LoadOrStore("server", &Server{})
	h.server = obj.(*Server)
	h.server.log = h.log.Named("substrate server")

	h.server.Start()

	return nil
}

func (h *App) registerCmd(c *execCmd) *execCmd {
	key := c.Key()
	if h.cmds[key] != nil {
		return h.cmds[key]
	}

	out, _ := cmds.LoadOrStore(key, c)
	outcmd := out.(*execCmd)
	h.cmds[key] = outcmd
	return outcmd
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")
	h.server.WaitForStart(h)

	for _, c := range h.cmds {
		c.host = h.server.Host
		go c.Run()
	}
	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")

	pool.Delete("server")

	for _, c := range h.cmds {
		cmds.Delete(c.Key())
	}

	pool.Delete("cmds")

	return nil
}

