package substrate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ProcessManager struct {
	idleTimeout    caddy.Duration
	startupTimeout caddy.Duration
	env            map[string]string
	logger         *zap.Logger
	processes      map[string]*Process
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

type Process struct {
	Command    string
	SocketPath string
	Cmd        *exec.Cmd
	LastUsed   time.Time
	exitCode   int
	onExit     func()
	mu         sync.RWMutex
	logger     *zap.Logger
	env        map[string]string
	// Startup output buffers (only used during startup)
	startupStdout *bytes.Buffer
	startupStderr *bytes.Buffer
	// Track intentional stops to avoid logging them as crashes
	stopping       bool
	exitChan       chan struct{}
	activeRequests int // Reference counting for one-shot mode
}

// ProcessStartupError contains detailed information about process startup failures
type ProcessStartupError struct {
	Err      error
	ExitCode int
	Stdout   string
	Stderr   string
	Command  string
}

func (e *ProcessStartupError) Error() string {
	return e.Err.Error()
}

func NewProcessManager(idleTimeout, startupTimeout caddy.Duration, env map[string]string, logger *zap.Logger) (*ProcessManager, error) {
	logger.Info("creating new process manager",
		zap.Duration("idle_timeout", time.Duration(idleTimeout)),
		zap.Duration("startup_timeout", time.Duration(startupTimeout)),
		zap.Any("env", env),
	)

	ctx, cancel := context.WithCancel(context.Background())

	pm := &ProcessManager{
		idleTimeout:    idleTimeout,
		startupTimeout: startupTimeout,
		env:            env,
		logger:         logger,
		processes:      make(map[string]*Process),
		ctx:            ctx,
		cancel:         cancel,
	}

	if idleTimeout > 0 {
		pm.wg.Add(1)
		go pm.cleanupLoop()
		logger.Debug("process manager cleanup loop started")
	} else {
		logger.Debug("process manager cleanup loop disabled",
			zap.Duration("idle_timeout", time.Duration(idleTimeout)))
	}

	return pm, nil
}

func validateFilePath(filePath string) error {
	cleanPath := filepath.Clean(filePath)

	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", filePath)
	}

	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("file path must be absolute: %s", filePath)
	}

	fileInfo, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", cleanPath)
		}
		return fmt.Errorf("failed to stat file %s: %w", cleanPath, err)
	}

	if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", cleanPath)
	}

	return nil
}

// getSocketPath generates a unique Unix domain socket path using random hex strings
func getSocketPath() (string, error) {
	const maxAttempts = 10

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Generate 8 random bytes (16 hex characters)
		randomBytes := make([]byte, 8)
		if _, err := rand.Read(randomBytes); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		hexString := hex.EncodeToString(randomBytes)

		socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("substrate-%s.sock", hexString))

		// Check if file already exists
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return socketPath, nil
		}
		// If file exists, try again with a new random name
	}

	return "", fmt.Errorf("failed to generate unique socket path after %d attempts", maxAttempts)
}

func (pm *ProcessManager) getOrCreateHost(file string) (string, error) {
	if err := validateFilePath(file); err != nil {
		pm.logger.Error("file path validation failed",
			zap.String("file", file),
			zap.Error(err),
		)
		return "", err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Try to reuse existing process (works for all modes including one-shot)
	if process, exists := pm.processes[file]; exists {
		process.mu.Lock()
		process.LastUsed = time.Now()
		process.activeRequests++
		socketPath := process.SocketPath
		pid := process.Cmd.Process.Pid
		activeCount := process.activeRequests
		process.mu.Unlock()

		pm.logger.Debug("reusing existing process",
			zap.String("file", file),
			zap.String("socket_path", socketPath),
			zap.Int("pid", pid),
			zap.Int("active_requests", activeCount),
		)
		return socketPath, nil
	}

	pm.logger.Info("creating new process",
		zap.String("file", file),
	)

	socketPath, err := getSocketPath()
	if err != nil {
		pm.logger.Error("failed to generate socket path",
			zap.String("file", file),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to generate socket path: %w", err)
	}

	pm.logger.Debug("generated socket path",
		zap.String("file", file),
		zap.String("socket_path", socketPath),
	)

	process := &Process{
		Command:        file,
		SocketPath:     socketPath,
		LastUsed:       time.Now(),
		onExit:         func() { pm.removeProcess(file) },
		logger:         pm.logger,
		env:            pm.env,
		startupStdout:  &bytes.Buffer{},
		startupStderr:  &bytes.Buffer{},
		activeRequests: 1, // Start with 1 active request
		exitChan:       make(chan struct{}),
	}

	pm.logger.Debug("starting process",
		zap.String("file", file),
		zap.String("socket_path", socketPath),
	)

	if err := process.start(); err != nil {
		pm.logger.Error("failed to start process",
			zap.String("file", file),
			zap.String("socket_path", socketPath),
			zap.Error(err),
		)
		return "", &ProcessStartupError{
			Err:      fmt.Errorf("failed to start process: %w", err),
			ExitCode: -1,
			Stdout:   process.startupStdout.String(),
			Stderr:   process.startupStderr.String(),
			Command:  file,
		}
	}

	pm.processes[file] = process

	pm.logger.Info("started process",
		zap.String("file", file),
		zap.String("socket_path", socketPath),
		zap.Int("pid", process.Cmd.Process.Pid),
	)

	if err := pm.waitForSocketReady(socketPath, time.Duration(pm.startupTimeout), process); err != nil {
		// Check if process already exited before we try to stop it
		exitCode := -1
		processAlreadyExited := false
		if process.Cmd != nil && process.Cmd.ProcessState != nil && process.Cmd.ProcessState.Exited() {
			exitCode = process.Cmd.ProcessState.ExitCode()
			processAlreadyExited = true
			pm.logger.Info("process already exited during startup",
				zap.Int("exit_code", exitCode),
				zap.String("file", file),
			)
		}

		// Process failed to start properly - clean up and return error
		if !processAlreadyExited {
			// Process is still running but failed to bind socket in time
			process.Stop()
			// Get exit code after Stop() completes
			exitCode = process.getExitCode()
		}

		delete(pm.processes, file)

		return "", &ProcessStartupError{
			Err:      fmt.Errorf("process startup failed: %w", err),
			ExitCode: exitCode,
			Stdout:   process.startupStdout.String(),
			Stderr:   process.startupStderr.String(),
			Command:  file,
		}
	}
	return socketPath, nil
}

func (pm *ProcessManager) Stop() error {
	pm.cancel()
	pm.wg.Wait()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errors []error
	for command, process := range pm.processes {
		if err := process.Stop(); err != nil {
			pm.logger.Warn("process stop returned error (may be expected during shutdown)",
				zap.String("command", command),
				zap.Error(err),
			)
			errors = append(errors, fmt.Errorf("failed to stop process %s: %w", command, err))
		}
	}

	// Clear the processes map regardless of errors since we've attempted to stop all processes
	pm.processes = make(map[string]*Process)

	// Don't return an error for process termination issues during shutdown
	// as they are expected and shouldn't prevent Caddy from shutting down cleanly
	if len(errors) > 0 {
		pm.logger.Info("process manager stopped with some process cleanup warnings",
			zap.Int("process_count", len(errors)),
		)
	} else {
		pm.logger.Info("process manager stopped cleanly")
	}

	return nil
}

func (pm *ProcessManager) cleanupLoop() {
	defer pm.wg.Done()

	idleTimeout := time.Duration(pm.idleTimeout)
	cleanupInterval := time.Hour
	if idleTimeout < cleanupInterval {
		cleanupInterval = idleTimeout
	}
	pm.logger.Debug("cleanup loop started",
		zap.Duration("cleanup_interval", cleanupInterval),
		zap.Duration("idle_timeout", idleTimeout),
	)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			pm.logger.Debug("cleanup loop stopped")
			return
		case <-ticker.C:
			pm.logger.Debug("running periodic cleanup")
			pm.cleanupIdleProcesses()
		}
	}
}

func (pm *ProcessManager) removeProcess(command string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.processes[command]; exists {
		pm.logger.Info("removing exited process from pool",
			zap.String("command", command),
		)
		delete(pm.processes, command)
	}
}

func (pm *ProcessManager) closeProcessAfterRequest(file string) {
	pm.mu.Lock()
	process, exists := pm.processes[file]
	if !exists {
		pm.mu.Unlock()
		return
	}

	process.mu.Lock()
	process.activeRequests--
	remaining := process.activeRequests
	process.mu.Unlock()

	// Remove from map immediately if last request
	if remaining == 0 {
		delete(pm.processes, file)
	}
	pm.mu.Unlock()

	// Kill process outside the lock
	if remaining == 0 {
		process.Stop()
	}
}

func (pm *ProcessManager) cleanupIdleProcesses() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	idleTimeout := time.Duration(pm.idleTimeout)
	now := time.Now()

	for command, process := range pm.processes {
		process.mu.RLock()
		lastUsed := process.LastUsed
		process.mu.RUnlock()

		if now.Sub(lastUsed) > idleTimeout {
			pm.logger.Info("stopping idle process",
				zap.String("command", command),
				zap.Duration("idle_time", now.Sub(lastUsed)),
			)

			if err := process.Stop(); err != nil {
				pm.logger.Error("failed to stop idle process",
					zap.String("command", command),
					zap.Error(err),
				)
			} else {
				delete(pm.processes, command)
			}
		}
	}
}

func (p *Process) start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	args := []string{p.SocketPath}
	p.Cmd = exec.Command(p.Command, args...)
	p.Cmd.Dir = filepath.Dir(p.Command)

	// Set up environment variables
	p.Cmd.Env = os.Environ() // Start with parent environment
	for key, value := range p.env {
		p.Cmd.Env = append(p.Cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	p.logger.Debug("configuring process command",
		zap.String("command", p.Command),
		zap.Strings("args", args),
		zap.String("working_dir", p.Cmd.Dir),
		zap.String("socket_path", p.SocketPath),
		zap.Any("env", p.env),
	)

	if err := configureProcessSecurity(p.Cmd, p.Command); err != nil {
		p.logger.Error("failed to configure process security",
			zap.String("command", p.Command),
			zap.Error(err),
		)
		return fmt.Errorf("failed to configure process security: %w", err)
	}

	// Set up output capture before starting the process
	stdout, err := p.Cmd.StdoutPipe()
	if err != nil {
		p.logger.Warn("failed to create stdout pipe, output will not be logged",
			zap.String("command", p.Command),
			zap.Error(err),
		)
	}

	stderr, err := p.Cmd.StderrPipe()
	if err != nil {
		p.logger.Warn("failed to create stderr pipe, error output will not be logged",
			zap.String("command", p.Command),
			zap.Error(err),
		)
	}

	p.logger.Debug("starting process command",
		zap.String("command", p.Command),
		zap.String("socket_path", p.SocketPath),
	)

	if err := p.Cmd.Start(); err != nil {
		p.logger.Error("failed to start command",
			zap.String("command", p.Command),
			zap.Error(err),
		)
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Start output logging and buffering goroutines after successful process start
	if stdout != nil {
		go p.logAndBufferOutput(stdout, "stdout", zap.InfoLevel, p.startupStdout)
	}
	if stderr != nil {
		go p.logAndBufferOutput(stderr, "stderr", zap.ErrorLevel, p.startupStderr)
	}

	p.logger.Info("process started successfully",
		zap.String("command", p.Command),
		zap.Int("pid", p.Cmd.Process.Pid),
		zap.String("socket_path", p.SocketPath),
	)

	go p.monitor()

	return nil
}

func (p *Process) logAndBufferOutput(pipe io.ReadCloser, streamType string, logLevel zapcore.Level, buffer *bytes.Buffer) {
	defer pipe.Close()

	// Create a tee reader to both log and buffer the output
	teeReader := io.TeeReader(pipe, buffer)
	scanner := bufio.NewScanner(teeReader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			p.logger.Log(logLevel, "process output",
				zap.String("command", p.Command),
				zap.Int("pid", p.Cmd.Process.Pid),
				zap.String("stream", streamType),
				zap.String("output", line),
			)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		p.logger.Error("error reading process output",
			zap.String("command", p.Command),
			zap.Int("pid", p.Cmd.Process.Pid),
			zap.String("stream", streamType),
			zap.Error(err),
		)
	}
}

// getExitCode returns the current exit code of the process, or -1 if not available
func (p *Process) getExitCode() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitCode
}

// clearStartupBuffers clears the startup output buffers to free memory after successful startup
func (p *Process) clearStartupBuffers() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startupStdout.Reset()
	p.startupStderr.Reset()
}

func (p *Process) monitor() {
	err := p.Cmd.Wait()

	p.mu.Lock()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitError.ExitCode()
		} else {
			p.exitCode = -1
		}
	} else {
		p.exitCode = 0
	}

	stopping := p.stopping
	command := p.Command
	exitCode := p.exitCode
	p.mu.Unlock()

	close(p.exitChan)

	// Only log unexpected exits as errors
	if exitCode != 0 && !stopping {
		p.logger.Error("process crashed",
			zap.String("command", command),
			zap.Int("exit_code", exitCode),
			zap.Error(err),
		)
	} else if exitCode == 0 && !stopping {
		p.logger.Info("process exited normally",
			zap.String("command", command),
		)
	}

	p.onExit()
}

func (p *Process) Stop() error {
	p.mu.Lock()
	if p.Cmd == nil || p.Cmd.Process == nil {
		p.mu.Unlock()
		return nil
	}

	p.stopping = true
	pid := p.Cmd.Process.Pid
	exitChan := p.exitChan
	p.mu.Unlock()

	p.logger.Info("stopping process",
		zap.String("command", p.Command),
		zap.Int("pid", pid),
	)

	// Send SIGTERM
	p.mu.Lock()
	proc := p.Cmd.Process
	p.mu.Unlock()

	if proc != nil {
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM: %w", err)
		}
	}

	// Wait for exit with timeout
	select {
	case <-time.After(10 * time.Second):
		p.logger.Warn("process did not exit, force killing",
			zap.String("command", p.Command),
			zap.Int("pid", pid),
		)
		p.mu.Lock()
		proc := p.Cmd.Process
		p.mu.Unlock()
		if proc != nil {
			proc.Kill()
		}
		<-exitChan
	case <-exitChan:
	}

	// Clean up socket
	os.Remove(p.SocketPath)
	return nil
}


func (pm *ProcessManager) waitForSocketReady(socketPath string, timeout time.Duration, process *Process) error {
	deadline := time.Now().Add(timeout)
	start := time.Now()

	pm.logger.Info("waiting for socket to become ready",
		zap.String("socket_path", socketPath),
		zap.Duration("timeout", timeout),
		zap.String("command", process.Command),
	)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	attemptCount := 0
	for {
		// Simple timeout check at the start of each iteration
		if time.Now().After(deadline) {
			pm.logger.Error("timeout waiting for socket to become ready",
				zap.String("socket_path", socketPath),
				zap.Duration("timeout", timeout),
				zap.Duration("elapsed", time.Since(start)),
				zap.Int("attempts", attemptCount),
				zap.String("command", process.Command),
			)
			return fmt.Errorf("timeout waiting for socket %s to become ready after %v", socketPath, timeout)
		}

		select {
		case <-time.After(time.Until(deadline)):
			pm.logger.Error("timeout waiting for socket to become ready (select case)",
				zap.String("socket_path", socketPath),
				zap.Duration("timeout", timeout),
				zap.Duration("elapsed", time.Since(start)),
				zap.Int("attempts", attemptCount),
				zap.String("command", process.Command),
			)
			return fmt.Errorf("timeout waiting for socket %s to become ready after %v", socketPath, timeout)
		case <-ticker.C:
			attemptCount++

			// Check if process is still alive before trying to connect
			if process.Cmd.ProcessState != nil && process.Cmd.ProcessState.Exited() {
				pm.logger.Error("process exited before socket became ready",
					zap.String("socket_path", socketPath),
					zap.Int("exit_code", process.Cmd.ProcessState.ExitCode()),
					zap.String("command", process.Command),
					zap.Int("attempts", attemptCount),
				)
				return fmt.Errorf("process exited before socket became ready (exit code: %d)", process.Cmd.ProcessState.ExitCode())
			}

			conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				waitTime := time.Since(start)
				pm.logger.Info("socket became ready",
					zap.String("socket_path", socketPath),
					zap.Duration("wait_time", waitTime),
					zap.Int("attempts", attemptCount),
					zap.String("command", process.Command),
				)
				// Clear startup buffers to free memory after successful startup
				process.clearStartupBuffers()
				return nil
			}

			// Log connection failures more frequently for debugging
			if attemptCount%50 == 0 {
				pm.logger.Info("still waiting for socket to become ready",
					zap.String("socket_path", socketPath),
					zap.Duration("elapsed", time.Since(start)),
					zap.Duration("remaining", time.Until(deadline)),
					zap.Int("attempts", attemptCount),
					zap.String("last_error", err.Error()),
				)
			}
		}
	}
}

func (pm *ProcessManager) Destruct() error {
	return pm.Stop()
}
