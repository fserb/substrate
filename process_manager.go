package substrate

import (
	"context"
	"fmt"
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
	processes map[string]*ManagedProcess
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
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
		processes: make(map[string]*ManagedProcess),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start the cleanup goroutine
	pm.wg.Add(1)
	go pm.cleanupLoop()

	return pm, nil
}

// StartProcess starts a new managed process
func (pm *ProcessManager) StartProcess(key string, config ProcessConfig) (*ManagedProcess, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if process already exists
	if existing, exists := pm.processes[key]; exists && existing.IsRunning() {
		return existing, nil
	}

	// Create new managed process
	process := &ManagedProcess{
		Key:      key,
		Config:   config,
		LastUsed: time.Now(),
		running:  false,
		logger:   pm.config.Logger,
	}

	// Start the process
	if err := process.start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	// Store the process
	pm.processes[key] = process

	pm.config.Logger.Info("started process",
		zap.String("key", key),
		zap.String("command", config.Command),
		zap.Int("pid", process.Cmd.Process.Pid),
	)

	return process, nil
}

// UpdateLastUsed updates the last used time for a process
func (pm *ProcessManager) UpdateLastUsed(key string) {
	pm.mu.RLock()
	process, exists := pm.processes[key]
	pm.mu.RUnlock()

	if exists {
		process.mu.Lock()
		process.LastUsed = time.Now()
		process.mu.Unlock()
	}
}

// Stop stops the process manager and all managed processes
func (pm *ProcessManager) Stop() error {
	pm.cancel()
	pm.wg.Wait()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errors []error
	for key, process := range pm.processes {
		if err := process.Stop(); err != nil {
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

	for key, process := range pm.processes {
		process.mu.RLock()
		lastUsed := process.LastUsed
		isRunning := process.IsRunning()
		process.mu.RUnlock()

		if isRunning && now.Sub(lastUsed) > idleTimeout {
			pm.config.Logger.Info("stopping idle process",
				zap.String("key", key),
				zap.Duration("idle_time", now.Sub(lastUsed)),
			)

			if err := process.Stop(); err != nil {
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