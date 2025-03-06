package substrate

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"sync"
	"time"

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
	Env       map[string]string `json:"env,omitempty"`
	StatusLog outputTarget      `json:"status_log,omitempty"`

	log         *zap.Logger
	mutex       sync.Mutex
	server      *Server
	statusLogFD *os.File
}

func parseGlobalSubstrate(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{StatusLog: outputTarget{Type: "stdout"}}

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
			case "status_log":
				target, err := parseRedirectGlobal(d)
				if err != nil {
					return nil, err
				}
				app.StatusLog = *target
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

	// Initialize status log if configured
	if err := h.initStatusLog(); err != nil {
		h.log.Error("Failed to initialize status log", zap.Error(err))
	}

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

// initStatusLog initializes the status log based on the configured target
func (h *App) initStatusLog() error {
	switch h.StatusLog.Type {
	case "stdout", "stderr", "null":
		// These don't need initialization
		return nil
	case "file":
		if h.StatusLog.File == "" {
			return fmt.Errorf("status_log file path is empty")
		}

		// Create or open the log file
		f, err := os.OpenFile(h.StatusLog.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open status log file: %w", err)
		}
		h.statusLogFD = f

		// Write a header to the log
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Fprintf(f, "=== Substrate Status Log Started at %s ===\n", timestamp)
		return nil
	default:
		return fmt.Errorf("invalid status log type: %s", h.StatusLog.Type)
	}
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")

	// Close status log file if open
	if h.statusLogFD != nil {
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Fprintf(h.statusLogFD, "=== Substrate Status Log Stopped at %s ===\n", timestamp)
		h.statusLogFD.Close()
		h.statusLogFD = nil
	}

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

