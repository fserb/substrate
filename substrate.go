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
				StartupTimeout: caddy.Duration(3 * time.Second),
			}
		},
	}
}

func (t *SubstrateTransport) Provision(ctx caddy.Context) error {
	t.ctx = ctx
	t.logger = ctx.Logger()

	t.logger.Debug("provisioning substrate transport",
		zap.Duration("idle_timeout", time.Duration(t.IdleTimeout)),
		zap.Duration("startup_timeout", time.Duration(t.StartupTimeout)),
	)

	httpTransport := new(reverseproxy.HTTPTransport)
	if err := httpTransport.Provision(ctx); err != nil {
		t.logger.Error("failed to provision HTTP transport", zap.Error(err))
		return fmt.Errorf("failed to provision HTTP transport: %w", err)
	}
	t.transport = httpTransport.Transport
	t.logger.Debug("HTTP transport provisioned successfully")

	manager, err := NewProcessManager(t.IdleTimeout, t.StartupTimeout, t.logger)
	if err != nil {
		t.logger.Error("failed to create process manager", zap.Error(err))
		return fmt.Errorf("failed to create process manager: %w", err)
	}
	t.manager = manager
	t.logger.Debug("process manager created successfully")

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

	return nil
}

func (t *SubstrateTransport) Cleanup() error {
	t.logger.Info("cleaning up substrate transport")
	if t.manager != nil {
		if err := t.manager.Stop(); err != nil {
			t.logger.Error("error during process manager cleanup", zap.Error(err))
			return err
		}
		t.logger.Debug("process manager stopped successfully")
	}
	t.logger.Info("substrate transport cleanup complete")
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
	t.logger.Debug("handling request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.String("remote_addr", req.RemoteAddr),
	)

	repl := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)

	filePath, _ := repl.GetString("http.matchers.file.absolute")
	if filePath == "" {
		filePath = req.URL.Path
		t.logger.Debug("no file matcher found, using URL path",
			zap.String("path", filePath),
		)
	} else {
		t.logger.Debug("resolved file path from matcher",
			zap.String("file_path", filePath),
		)
	}

	t.logger.Info("routing request to subprocess",
		zap.String("method", req.Method),
		zap.String("url", req.URL.Path),
		zap.String("file_path", filePath),
		zap.String("remote_addr", req.RemoteAddr),
	)

	hostPort, err := t.manager.getOrCreateHost(filePath)
	if err != nil {
		t.logger.Error("failed to get or create host for file",
			zap.String("file_path", filePath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get host for file %s: %w", filePath, err)
	}

	t.logger.Debug("proxying request to process",
		zap.String("file_path", filePath),
		zap.String("target_host_port", hostPort),
		zap.String("original_host", req.URL.Host),
	)

	originalHost := req.URL.Host
	originalScheme := req.URL.Scheme
	req.URL.Host = hostPort
	req.URL.Scheme = "http"

	start := time.Now()
	resp, err := t.transport.RoundTrip(req)
	duration := time.Since(start)

	// Restore original URL in case of error
	if err != nil {
		req.URL.Host = originalHost
		req.URL.Scheme = originalScheme
		t.logger.Error("process request failed",
			zap.String("file_path", filePath),
			zap.String("target_host_port", hostPort),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("request to process failed: %w", err)
	}

	t.logger.Info("request completed successfully",
		zap.String("file_path", filePath),
		zap.String("target_host_port", hostPort),
		zap.Duration("duration", duration),
		zap.Int("status_code", resp.StatusCode),
		zap.Int64("content_length", resp.ContentLength),
	)

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
