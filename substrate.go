package substrate

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(SubstrateTransport{})
}

type SubstrateTransport struct {
	IdleTimeout    caddy.Duration    `json:"idle_timeout,omitempty"`
	StartupTimeout caddy.Duration    `json:"startup_timeout,omitempty"`
	Env            map[string]string `json:"env,omitempty"`

	ctx       caddy.Context
	transport http.RoundTripper
	manager   *ProcessManager
	logger    *zap.Logger
}

// oneShotBodyWrapper wraps a response body to trigger cleanup after body is fully read
type oneShotBodyWrapper struct {
	io.ReadCloser
	onClose func()
}

func (w *oneShotBodyWrapper) Close() error {
	err := w.ReadCloser.Close()
	if w.onClose != nil {
		w.onClose()
		w.onClose = nil // Ensure cleanup only happens once
	}
	return err
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
		zap.Any("env", t.Env),
	)

	// Create HTTP transport with Unix socket support
	httpTransport := new(reverseproxy.HTTPTransport)
	if err := httpTransport.Provision(ctx); err != nil {
		t.logger.Error("failed to provision HTTP transport", zap.Error(err))
		return fmt.Errorf("failed to provision HTTP transport: %w", err)
	}

	t.transport = httpTransport
	t.logger.Debug("HTTP transport provisioned successfully")

	manager, err := NewProcessManager(t.IdleTimeout, t.StartupTimeout, t.Env, t.logger)
	if err != nil {
		t.logger.Error("failed to create process manager", zap.Error(err))
		return fmt.Errorf("failed to create process manager: %w", err)
	}
	t.manager = manager
	t.logger.Debug("process manager created successfully")

	t.logger.Info("substrate transport provisioned",
		zap.Duration("idle_timeout", time.Duration(t.IdleTimeout)),
		zap.Duration("startup_timeout", time.Duration(t.StartupTimeout)),
		zap.Any("env", t.Env),
	)

	return nil
}

func (t *SubstrateTransport) Validate() error {
	if t.IdleTimeout < -1 {
		return fmt.Errorf("idle_timeout must be >= -1 (use -1 for close-after-request, 0 to disable cleanup, or positive duration)")
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
				val := d.Val()
				// Handle special cases for unitless values
				if val == "0" {
					t.IdleTimeout = caddy.Duration(0)
				} else if val == "-1" {
					t.IdleTimeout = caddy.Duration(-1)
				} else {
					dur, err := time.ParseDuration(val)
					if err != nil {
						return d.Errf("parsing idle_timeout: %v", err)
					}
					t.IdleTimeout = caddy.Duration(dur)
				}
			case "startup_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				dur, err := time.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("parsing startup_timeout: %v", err)
				}
				t.StartupTimeout = caddy.Duration(dur)
			case "env":
				if t.Env == nil {
					t.Env = make(map[string]string)
				}
				for d.NextBlock(1) {
					key := d.Val()
					if !d.NextArg() {
						return d.Errf("env directive requires key-value pairs")
					}
					value := d.Val()
					t.Env[key] = value
				}
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

	// Convert to absolute path for consistent process tracking
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		t.logger.Error("failed to get absolute path",
			zap.String("file_path", filePath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	t.logger.Info("routing request to subprocess",
		zap.String("method", req.Method),
		zap.String("url", req.URL.Path),
		zap.String("file_path", absFilePath),
		zap.String("remote_addr", req.RemoteAddr),
	)

	socketPath, err := t.manager.getOrCreateHost(absFilePath)
	if err != nil {
		t.logger.Error("failed to get or create socket for file",
			zap.String("file_path", filePath),
			zap.Error(err),
		)

		// Return HTTP 502 response instead of error
		responseBody := "Bad Gateway"

		// If this is a startup error and request is from internal IP, include details
		if startupErr, ok := err.(*ProcessStartupError); ok && isInternalIP(req.RemoteAddr) {
			var details strings.Builder
			details.WriteString(fmt.Sprintf("Process startup failed: %s\n\n", startupErr.Err.Error()))
			details.WriteString(fmt.Sprintf("Command: %s\n", startupErr.Command))
			details.WriteString(fmt.Sprintf("Exit code: %d\n\n", startupErr.ExitCode))
			if startupErr.Stdout != "" {
				details.WriteString("Stdout:\n")
				details.WriteString(startupErr.Stdout)
				details.WriteString("\n\n")
			}
			if startupErr.Stderr != "" {
				details.WriteString("Stderr:\n")
				details.WriteString(startupErr.Stderr)
				details.WriteString("\n")
			}
			responseBody = details.String()
		}

		return &http.Response{
			StatusCode:    http.StatusBadGateway,
			Status:        "502 Bad Gateway",
			Body:          io.NopCloser(strings.NewReader(responseBody)),
			ContentLength: int64(len(responseBody)),
			Header: http.Header{
				"Content-Type": []string{"text/plain; charset=utf-8"},
			},
			Request: req,
		}, nil
	}

	t.logger.Debug("proxying request to process",
		zap.String("file_path", filePath),
		zap.String("socket_path", socketPath),
	)

	// Create a unique host for each socket to enable proper connection pooling.
	// http.Transport keys connections by req.URL.Host, so different sockets need different hosts.
	// We use {socketname}.localhost format (e.g., "substrate-123456.localhost").
	// The .localhost TLD ensures no external DNS lookups per RFC.
	socketName := strings.TrimSuffix(filepath.Base(socketPath), ".sock")
	req.URL.Host = socketName + ".localhost"

	// Set dial info in the request context so HTTPTransport knows to use Unix socket
	dialInfo := reverseproxy.DialInfo{
		Network: "unix",
		Address: socketPath,
	}
	caddyhttp.SetVar(req.Context(), "reverse_proxy.dial_info", dialInfo)

	start := time.Now()
	resp, err := t.transport.RoundTrip(req)
	duration := time.Since(start)

	if err != nil {
		t.logger.Error("process request failed",
			zap.String("file_path", filePath),
			zap.String("socket_path", socketPath),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return nil, fmt.Errorf("request to process failed: %w", err)
	}

	// In one-shot mode, wrap response body to trigger cleanup after body is fully transmitted
	if t.IdleTimeout == -1 {
		resp.Body = &oneShotBodyWrapper{
			ReadCloser: resp.Body,
			onClose: func() {
				// Use goroutine so body close isn't blocked waiting for process to stop
				go t.manager.closeProcessAfterRequest(absFilePath)
			},
		}
	}

	t.logger.Info("request completed successfully",
		zap.String("file_path", filePath),
		zap.String("socket_path", socketPath),
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
