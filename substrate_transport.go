package substrate

import (
	"fmt"
	"net/http"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(SubstrateTransport{})
}

// SubstrateTransport is a reverse proxy transport that manages processes dynamically.
// It starts processes on demand, manages their lifecycle, and proxies HTTP requests to them.
// Similar to FastCGI but uses HTTP protocol and provides more flexible process management.
type SubstrateTransport struct {
	// Embed HTTPTransport to inherit standard HTTP transport functionality
	*reverseproxy.HTTPTransport

	// How long to keep idle processes alive
	IdleTimeout caddy.Duration `json:"idle_timeout,omitempty"`

	// How long to wait for process startup
	StartupTimeout caddy.Duration `json:"startup_timeout,omitempty"`

	// Internal state
	ctx     caddy.Context
	manager *ProcessManager
	logger  *zap.Logger
}


// CaddyModule returns the Caddy module information.
func (SubstrateTransport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "http.reverse_proxy.transport.substrate",
		New: func() caddy.Module {
			return &SubstrateTransport{
				HTTPTransport:  new(reverseproxy.HTTPTransport),
				IdleTimeout:    caddy.Duration(300000000000), // 5 minutes
				StartupTimeout: caddy.Duration(30000000000),  // 30 seconds
			}
		},
	}
}

// Provision sets up the transport module.
func (t *SubstrateTransport) Provision(ctx caddy.Context) error {
	t.ctx = ctx
	t.logger = ctx.Logger()

	// Initialize the underlying HTTP transport
	if t.HTTPTransport == nil {
		t.HTTPTransport = new(reverseproxy.HTTPTransport)
	}

	// Set up the HTTP transport with our context
	if err := t.HTTPTransport.Provision(ctx); err != nil {
		return fmt.Errorf("failed to provision HTTP transport: %w", err)
	}

	// Initialize process manager
	var err error
	managerConfig := ProcessManagerConfig{
		IdleTimeout:    t.IdleTimeout,
		StartupTimeout: t.StartupTimeout,
		Logger:         t.logger,
	}

	t.manager, err = NewProcessManager(managerConfig)
	if err != nil {
		return fmt.Errorf("failed to create process manager: %w", err)
	}

	t.logger.Info("substrate transport provisioned",
		zap.Duration("idle_timeout", time.Duration(t.IdleTimeout)),
		zap.Duration("startup_timeout", time.Duration(t.StartupTimeout)),
	)

	return nil
}

// Validate ensures the transport configuration is valid.
func (t *SubstrateTransport) Validate() error {
	// HTTPTransport doesn't have a Validate method, so we skip this validation
	// The underlying HTTP transport will be validated during Provision

	return nil
}

// Cleanup stops all managed processes and cleans up resources.
func (t *SubstrateTransport) Cleanup() error {
	if t.manager != nil {
		return t.manager.Stop()
	}
	return nil
}

// RoundTrip implements http.RoundTripper. This is the main method that handles
// incoming requests and routes them to managed processes.
func (t *SubstrateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Set the scheme for the underlying HTTP transport
	t.HTTPTransport.SetScheme(req)

	// Get the replacer from the request context
	repl := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	
	// Get the absolute file path from the file matcher (if it was used)
	filePath, _ := repl.GetString("http.matchers.file.absolute")
	if filePath == "" {
		// Fallback: use the request path if no file matcher was used
		filePath = req.URL.Path
	}

	// Get or create host:port for this file
	hostPort, err := t.manager.getOrCreateHost(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get host for file %s: %w", filePath, err)
	}

	// Update the request URL to point to the managed process
	originalHost := req.URL.Host
	req.URL.Host = hostPort
	req.URL.Scheme = "http"

	// Perform the actual request using the underlying HTTP transport
	resp, err := t.HTTPTransport.Transport.RoundTrip(req)
	if err != nil {
		// Restore original host for error reporting
		req.URL.Host = originalHost
		return nil, fmt.Errorf("request to process failed: %w", err)
	}

	return resp, nil
}





// Compile-time check that SubstrateTransport implements necessary interfaces
var (
	_ caddy.Module           = (*SubstrateTransport)(nil)
	_ caddy.Provisioner      = (*SubstrateTransport)(nil)
	_ caddy.Validator        = (*SubstrateTransport)(nil)
	_ caddy.CleanerUpper     = (*SubstrateTransport)(nil)
	_ http.RoundTripper      = (*SubstrateTransport)(nil)
)