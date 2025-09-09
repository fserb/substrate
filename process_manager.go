package substrate

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

// ProcessManagerConfig holds configuration for the process manager
type ProcessManagerConfig struct {
	IdleTimeout    caddy.Duration
	StartupTimeout caddy.Duration
	Logger         *zap.Logger
}

// ProcessManager manages the lifecycle of substrate processes
type ProcessManager struct {
	config    ProcessManagerConfig
	processes map[string]*ProcessInfo
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// ProcessInfo holds information about a running process
type ProcessInfo struct {
	Process *ManagedProcess
	Host    string
	Port    int
}

// ManagedProcess represents a single managed process
type ManagedProcess struct {
	Key      string
	Config   ProcessConfig
	Cmd      *exec.Cmd
	LastUsed time.Time
	running  bool
	mu       sync.RWMutex
	logger   *zap.Logger
}

// ProcessConfig contains the configuration for starting a process
type ProcessConfig struct {
	Command string
	Args    []string
}


// NewProcessManager creates a new process manager
func NewProcessManager(config ProcessManagerConfig) (*ProcessManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	pm := &ProcessManager{
		config:    config,
		processes: make(map[string]*ProcessInfo),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start the cleanup goroutine
	pm.wg.Add(1)
	go pm.cleanupLoop()

	return pm, nil
}

// getOrCreateHost gets or creates a host:port for the given file
func (pm *ProcessManager) getOrCreateHost(file string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if process already exists and is running
	if info, exists := pm.processes[file]; exists && info.Process.IsRunning() {
		info.Process.mu.Lock()
		info.Process.LastUsed = time.Now()
		info.Process.mu.Unlock()
		return fmt.Sprintf("%s:%d", info.Host, info.Port), nil
	}

	// Get a free port
	port, err := getFreePort()
	if err != nil {
		return "", fmt.Errorf("failed to get free port: %w", err)
	}

	host := "localhost"

	// Create process config
	config := ProcessConfig{
		Command: file,
		Args:    []string{host, fmt.Sprintf("%d", port)},
	}

	// Create new managed process
	process := &ManagedProcess{
		Key:      file,
		Config:   config,
		LastUsed: time.Now(),
		running:  false,
		logger:   pm.config.Logger,
	}

	// Start the process
	if err := process.start(); err != nil {
		return "", fmt.Errorf("failed to start process: %w", err)
	}

	// Store the process info
	info := &ProcessInfo{
		Process: process,
		Host:    host,
		Port:    port,
	}
	pm.processes[file] = info

	pm.config.Logger.Info("started process",
		zap.String("file", file),
		zap.String("host:port", fmt.Sprintf("%s:%d", host, port)),
		zap.Int("pid", process.Cmd.Process.Pid),
	)

	// Wait for startup
	time.Sleep(time.Duration(pm.config.StartupTimeout))

	return fmt.Sprintf("%s:%d", host, port), nil
}


// Stop stops the process manager and all managed processes
func (pm *ProcessManager) Stop() error {
	pm.cancel()
	pm.wg.Wait()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errors []error
	for key, info := range pm.processes {
		if err := info.Process.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop process %s: %w", key, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping processes: %v", errors)
	}

	return nil
}

// cleanupLoop runs periodically to clean up idle processes
func (pm *ProcessManager) cleanupLoop() {
	defer pm.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.cleanupIdleProcesses()
		}
	}
}

// cleanupIdleProcesses stops processes that have been idle for too long
func (pm *ProcessManager) cleanupIdleProcesses() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	idleTimeout := time.Duration(pm.config.IdleTimeout)
	now := time.Now()

	for key, info := range pm.processes {
		info.Process.mu.RLock()
		lastUsed := info.Process.LastUsed
		isRunning := info.Process.IsRunning()
		info.Process.mu.RUnlock()

		if isRunning && now.Sub(lastUsed) > idleTimeout {
			pm.config.Logger.Info("stopping idle process",
				zap.String("key", key),
				zap.Duration("idle_time", now.Sub(lastUsed)),
			)

			if err := info.Process.Stop(); err != nil {
				pm.config.Logger.Error("failed to stop idle process",
					zap.String("key", key),
					zap.Error(err),
				)
			} else {
				delete(pm.processes, key)
			}
		}
	}
}

// start starts the managed process
func (mp *ManagedProcess) start() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Create the command with args
	mp.Cmd = exec.Command(mp.Config.Command, mp.Config.Args...)

	// Configure process security to run with file owner's permissions
	if err := configureProcessSecurity(mp.Cmd, mp.Config.Command); err != nil {
		return fmt.Errorf("failed to configure process security: %w", err)
	}

	// Start the process
	if err := mp.Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	mp.running = true

	// Monitor the process in a goroutine
	go mp.monitor()

	return nil
}

// monitor monitors the process and updates running state when it exits
func (mp *ManagedProcess) monitor() {
	// Wait for the process to complete
	mp.Cmd.Wait()

	mp.mu.Lock()
	mp.running = false
	mp.mu.Unlock()
}

// IsRunning returns true if the process is currently running
func (mp *ManagedProcess) IsRunning() bool {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.running
}

// Stop stops the managed process
func (mp *ManagedProcess) Stop() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.Cmd == nil || mp.Cmd.Process == nil {
		return nil
	}

	if !mp.running {
		return nil
	}

	mp.logger.Info("stopping process",
		zap.String("key", mp.Key),
		zap.Int("pid", mp.Cmd.Process.Pid),
	)

	// Send SIGTERM first
	if err := mp.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Give the process time to shut down gracefully
	done := make(chan error, 1)
	go func() {
		done <- mp.Cmd.Wait()
	}()

	select {
	case <-time.After(10 * time.Second):
		// Force kill if it doesn't shut down gracefully
		mp.logger.Warn("process did not shut down gracefully, force killing",
			zap.String("key", mp.Key),
			zap.Int("pid", mp.Cmd.Process.Pid),
		)
		if err := mp.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
		<-done // Wait for process to actually exit
	case err := <-done:
		if err != nil && !isProcessAlreadyFinished(err) {
			return fmt.Errorf("process exit error: %w", err)
		}
	}

	mp.running = false
	return nil
}


// isProcessAlreadyFinished checks if the error indicates the process already finished
func isProcessAlreadyFinished(err error) bool {
	if err == nil {
		return true // No error means successful termination
	}
	
	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.Exited()
	}
	
	// Handle common process termination scenarios
	errStr := err.Error()
	// Accept any signal-based termination as expected
	if errStr == "signal: terminated" || 
	   errStr == "signal: killed" ||
	   errStr == "wait: no child processes" {
		return true
	}
	
	return false
}

// getFreePort finds an available port on localhost
func getFreePort() (int, error) {
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