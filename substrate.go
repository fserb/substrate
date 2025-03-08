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
)

func init() {
	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("substrate", parseGlobalSubstrate)

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

	watchers    map[string]*Watcher
	log         *zap.Logger
	mutex       sync.Mutex
	statusLogFD *os.File
}

// parseGlobalSubstrate parses the global substrate configuration from the Caddyfile.
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

// parseRedirectGlobal parses the status_log directive configuration.
// It supports "stdout", "stderr", "null", and "file" output targets.
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

// Provision sets up the app.
func (h *App) Provision(ctx caddy.Context) error {
	if h.log == nil {
		h.log = ctx.Logger(h)
	}

	return nil
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")

	if err := h.initStatusLog(); err != nil {
		h.log.Error("Failed to initialize status log", zap.Error(err))
	}

	h.watchers = make(map[string]*Watcher)

	return nil
}

// initStatusLog initializes the status log based on the configured target.
func (h *App) initStatusLog() error {
	switch h.StatusLog.Type {
	case "stdout", "stderr", "null":
		return nil
	case "file":
		if h.StatusLog.File == "" {
			return fmt.Errorf("status_log file path is empty")
		}

		f, err := os.OpenFile(h.StatusLog.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open status log file: %w", err)
		}
		h.statusLogFD = f

		timestamp := time.Now().Format(time.RFC3339)
		fmt.Fprintf(f, "=== Substrate Status Log Started at %s ===\n", timestamp)
		return nil
	default:
		return fmt.Errorf("invalid status log type: %s", h.StatusLog.Type)
	}
}

// Stop gracefully shuts down the substrate app.
func (h *App) Stop() error {
	h.log.Info("Stopping substrate")

	if h.statusLogFD != nil {
		timestamp := time.Now().Format(time.RFC3339)
		fmt.Fprintf(h.statusLogFD, "=== Substrate Status Log Stopped at %s ===\n", timestamp)
		h.statusLogFD.Close()
		h.statusLogFD = nil
	}

	for _, watcher := range h.watchers {
		watcher.Close()
	}

	return nil
}

// GetWatcher retrieves an existing watcher for the given root directory
// or creates a new one if it doesn't exist.
func (h *App) GetWatcher(root string) *Watcher {
	hash := sha1.Sum(append(salt, []byte(root)...))
	key := hex.EncodeToString(hash[:])

	got, ok := h.watchers[key]
	if ok {
		return got
	}

	watcher := &Watcher{
		Root: root,
		app:  h,
		log:  h.log.With(zap.String("root", root)),
	}

	if err := watcher.init(); err != nil {
		h.log.Error("Failed to initialize watcher", zap.Error(err))
		return nil
	}

	h.watchers[key] = watcher
	return watcher
}

