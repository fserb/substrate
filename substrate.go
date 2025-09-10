package substrate

import (
	"fmt"
	"net/http"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"go.uber.org/zap"
)

var processPool = caddy.NewUsagePool()

func init() {
	caddy.RegisterModule(SubstrateTransport{})
}

type SubstrateTransport struct {
	IdleTimeout    caddy.Duration `json:"idle_timeout,omitempty"`
	StartupTimeout caddy.Duration `json:"startup_timeout,omitempty"`

	ctx       caddy.Context
	transport http.RoundTripper
	manager   *ProcessManager
	logger    *zap.Logger
}

func (SubstrateTransport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "http.reverse_proxy.transport.substrate",
		New: func() caddy.Module {
			return &SubstrateTransport{
				IdleTimeout:    caddy.Duration(1 * time.Hour),
				StartupTimeout: caddy.Duration(30 * time.Second),
			}
		},
	}
}

func (t *SubstrateTransport) Provision(ctx caddy.Context) error {
	t.ctx = ctx
	t.logger = ctx.Logger()

	httpTransport := new(reverseproxy.HTTPTransport)
	if err := httpTransport.Provision(ctx); err != nil {
		return fmt.Errorf("failed to provision HTTP transport: %w", err)
	}
	t.transport = httpTransport.Transport

	manager, loaded, err := processPool.LoadOrNew("process_manager", func() (caddy.Destructor, error) {
		return NewProcessManager(t.IdleTimeout, t.StartupTimeout, t.logger)
	})
	if err != nil {
		return fmt.Errorf("failed to get process manager: %w", err)
	}
	t.manager = manager.(*ProcessManager)

	if loaded {
		t.logger.Info("reusing existing process manager from pool")
	} else {
		t.logger.Info("created new process manager")
	}

	t.logger.Info("substrate transport provisioned",
		zap.Duration("idle_timeout", time.Duration(t.IdleTimeout)),
		zap.Duration("startup_timeout", time.Duration(t.StartupTimeout)),
	)

	return nil
}

func (t *SubstrateTransport) Validate() error {
	if t.IdleTimeout < 0 {
		return fmt.Errorf("idle_timeout cannot be negative")
	}

	if t.StartupTimeout < 0 {
		return fmt.Errorf("startup_timeout cannot be negative")
	}

	if t.StartupTimeout == 0 {
		return fmt.Errorf("startup_timeout cannot be zero")
	}

	if t.StartupTimeout > caddy.Duration(5*time.Minute) {
		return fmt.Errorf("startup_timeout is very long (%v), this seems unusual", time.Duration(t.StartupTimeout))
	}

	return nil
}

func (t *SubstrateTransport) Cleanup() error {
	if t.manager != nil {
		_, err := processPool.Delete("process_manager")
		return err
	}
	return nil
}

func (t *SubstrateTransport) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "idle_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				dur, err := time.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("parsing idle_timeout: %v", err)
				}
				t.IdleTimeout = caddy.Duration(dur)
			case "startup_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				dur, err := time.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("parsing startup_timeout: %v", err)
				}
				t.StartupTimeout = caddy.Duration(dur)
			default:
				return d.Errf("unknown directive: %s", d.Val())
			}
		}
	}
	return nil
}

func (t *SubstrateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	repl := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)

	filePath, _ := repl.GetString("http.matchers.file.absolute")
	if filePath == "" {
		filePath = req.URL.Path
	}

	hostPort, err := t.manager.getOrCreateHost(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get host for file %s: %w", filePath, err)
	}

	originalHost := req.URL.Host
	req.URL.Host = hostPort
	req.URL.Scheme = "http"

	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		req.URL.Host = originalHost
		return nil, fmt.Errorf("request to process failed: %w", err)
	}

	return resp, nil
}

var (
	_ caddy.Module          = (*SubstrateTransport)(nil)
	_ caddy.Provisioner     = (*SubstrateTransport)(nil)
	_ caddy.Validator       = (*SubstrateTransport)(nil)
	_ caddy.CleanerUpper    = (*SubstrateTransport)(nil)
	_ http.RoundTripper     = (*SubstrateTransport)(nil)
	_ caddyfile.Unmarshaler = (*SubstrateTransport)(nil)
)
