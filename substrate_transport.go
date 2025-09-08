package substrate

import (
	"fmt"
	"net"
	"net/http"
	"sync"
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
	ctx          caddy.Context
	processMap   map[string]*ProcessInstance
	processMapMu *sync.RWMutex
	manager      *ProcessManager
	logger       *zap.Logger
}

// ProcessInstance represents a running process managed by the transport
type ProcessInstance struct {
	Process *ManagedProcess
	Port    int
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
	t.processMap = make(map[string]*ProcessInstance)
	t.processMapMu = new(sync.RWMutex)

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

	// Generate a key for this process based on the request
	processKey := t.generateProcessKey(req)

	// Get or start the process for this request
	instance, err := t.getOrStartProcess(processKey, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get process for request: %w", err)
	}

	// Update the request URL to point to the managed process
	originalHost := req.URL.Host
	req.URL.Host = fmt.Sprintf("localhost:%d", instance.Port)
	req.URL.Scheme = "http"

	// Perform the actual request using the underlying HTTP transport
	resp, err := t.HTTPTransport.Transport.RoundTrip(req)
	if err != nil {
		// If the request failed, the process might be dead - mark it for restart
		t.markProcessForRestart(processKey)
		
		// Restore original host for error reporting
		req.URL.Host = originalHost
		return nil, fmt.Errorf("request to process failed: %w", err)
	}

	// Update process last used time
	t.manager.UpdateLastUsed(processKey)

	return resp, nil
}

// generateProcessKey creates a unique key for process identification.
// This uses the request path to determine which file should be executed.
func (t *SubstrateTransport) generateProcessKey(req *http.Request) string {
	// Use the request path as the process key - each file gets its own process
	return req.URL.Path
}

// getOrStartProcess retrieves an existing process or starts a new one
func (t *SubstrateTransport) getOrStartProcess(key string, req *http.Request) (*ProcessInstance, error) {
	// Check if we already have this process running
	t.processMapMu.RLock()
	instance, exists := t.processMap[key]
	t.processMapMu.RUnlock()

	if exists && instance.Process.IsRunning() {
		return instance, nil
	}

	// Need to start a new process
	t.processMapMu.Lock()
	defer t.processMapMu.Unlock()

	// Double-check in case another goroutine started it
	if instance, exists := t.processMap[key]; exists && instance.Process.IsRunning() {
		return instance, nil
	}

	// Get the replacer from the request context
	repl := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	
	// Get the absolute file path from the file matcher (if it was used)
	filePath, _ := repl.GetString("http.matchers.file.absolute")
	if filePath == "" {
		// Fallback: use the request path if no file matcher was used
		filePath = req.URL.Path
	}
	
	// Get a free port for the process to listen on
	processPort, err := t.getFreePort()
	if err != nil {
		return nil, fmt.Errorf("failed to get free port: %w", err)
	}
	
	// Start the process with host and port as arguments
	processConfig := ProcessConfig{
		Command: filePath,
		Args:    []string{"localhost", fmt.Sprintf("%d", processPort)},
	}

	managedProcess, err := t.manager.StartProcess(key, processConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	// Give the process time to start listening
	time.Sleep(time.Duration(t.StartupTimeout))

	// Create the process instance
	instance = &ProcessInstance{
		Process: managedProcess,
		Port:    processPort,
	}

	t.processMap[key] = instance

	t.logger.Info("started new process",
		zap.String("key", key),
		zap.Int("port", processPort),
		zap.String("file_path", filePath),
	)

	return instance, nil
}

// getFreePort finds an available port on localhost
func (t *SubstrateTransport) getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find free port: %w", err)
	}
	defer listener.Close()
	
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to get TCP address")
	}
	
	return addr.Port, nil
}

// markProcessForRestart marks a process as needing restart
func (t *SubstrateTransport) markProcessForRestart(key string) {
	t.processMapMu.Lock()
	defer t.processMapMu.Unlock()

	if instance, exists := t.processMap[key]; exists {
		// Stop the process - it will be restarted on next request
		instance.Process.Stop()
		delete(t.processMap, key)
		
		t.logger.Warn("marked process for restart due to failure",
			zap.String("key", key),
		)
	}
}

// Compile-time check that SubstrateTransport implements necessary interfaces
var (
	_ caddy.Module           = (*SubstrateTransport)(nil)
	_ caddy.Provisioner      = (*SubstrateTransport)(nil)
	_ caddy.Validator        = (*SubstrateTransport)(nil)
	_ caddy.CleanerUpper     = (*SubstrateTransport)(nil)
	_ http.RoundTripper      = (*SubstrateTransport)(nil)
)