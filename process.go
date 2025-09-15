package substrate

import (
	"bufio"
	"context"
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
	logger         *zap.Logger
	processes      map[string]*Process
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

type Process struct {
	Command  string
	Host     string
	Port     int
	Cmd      *exec.Cmd
	LastUsed time.Time
	exitCode int
	onExit   func()
	mu       sync.RWMutex
	logger   *zap.Logger
}

func NewProcessManager(idleTimeout, startupTimeout caddy.Duration, logger *zap.Logger) (*ProcessManager, error) {
	logger.Info("creating new process manager",
		zap.Duration("idle_timeout", time.Duration(idleTimeout)),
		zap.Duration("startup_timeout", time.Duration(startupTimeout)),
	)

	ctx, cancel := context.WithCancel(context.Background())

	pm := &ProcessManager{
		idleTimeout:    idleTimeout,
		startupTimeout: startupTimeout,
		logger:         logger,
		processes:      make(map[string]*Process),
		ctx:            ctx,
		cancel:         cancel,
	}

	pm.wg.Add(1)
	go pm.cleanupLoop()

	logger.Debug("process manager cleanup loop started")

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

func (pm *ProcessManager) getOrCreateHost(file string) (string, error) {
	pm.logger.Debug("getOrCreateHost called", zap.String("file", file))

	if err := validateFilePath(file); err != nil {
		pm.logger.Error("file path validation failed",
			zap.String("file", file),
			zap.Error(err),
		)
		return "", err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if process, exists := pm.processes[file]; exists {
		process.mu.Lock()
		process.LastUsed = time.Now()
		hostPort := fmt.Sprintf("%s:%d", process.Host, process.Port)
		pid := process.Cmd.Process.Pid
		process.mu.Unlock()

		pm.logger.Debug("reusing existing process",
			zap.String("file", file),
			zap.String("host_port", hostPort),
			zap.Int("pid", pid),
		)
		return hostPort, nil
	}

	pm.logger.Info("creating new process",
		zap.String("file", file),
	)

	host := "localhost"
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		pm.logger.Debug("attempting to create process",
			zap.String("file", file),
			zap.Int("attempt", attempt),
			zap.Int("max_retries", maxRetries),
		)

		port, err := getFreePort()
		if err != nil {
			pm.logger.Error("failed to get free port",
				zap.String("file", file),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			return "", fmt.Errorf("failed to get free port: %w", err)
		}

		pm.logger.Debug("allocated free port",
			zap.String("file", file),
			zap.Int("port", port),
			zap.Int("attempt", attempt),
		)

		process := &Process{
			Command:  file,
			Host:     host,
			Port:     port,
			LastUsed: time.Now(),
			onExit:   func() { pm.removeProcess(file) },
			logger:   pm.logger,
		}

		pm.logger.Debug("starting process",
			zap.String("file", file),
			zap.String("host_port", fmt.Sprintf("%s:%d", host, port)),
		)

		if err := process.start(); err != nil {
			pm.logger.Error("failed to start process",
				zap.String("file", file),
				zap.Int("attempt", attempt),
				zap.Int("port", port),
				zap.Error(err),
			)

			if pm.isPortInUse(host, port) {
				pm.logger.Warn("port race condition detected during process start, retrying",
					zap.Int("attempt", attempt),
					zap.Int("port", port),
					zap.String("file", file),
				)
				if attempt < maxRetries {
					continue
				}
			}
			return "", fmt.Errorf("failed to start process after %d attempts: %w", attempt, err)
		}

		pm.processes[file] = process

		pm.logger.Info("started process",
			zap.String("file", file),
			zap.String("host:port", fmt.Sprintf("%s:%d", host, port)),
			zap.Int("pid", process.Cmd.Process.Pid),
			zap.Int("attempt", attempt),
		)

		if err := pm.waitForPortReady(host, port, time.Duration(pm.startupTimeout), process); err != nil {
			if pm.isPortInUse(host, port) {
				pm.logger.Warn("port stolen after process start, retrying",
					zap.Int("attempt", attempt),
					zap.Int("port", port),
					zap.String("file", file),
				)
				process.Stop()
				delete(pm.processes, file)
				if attempt < maxRetries {
					continue
				}
				return "", fmt.Errorf("port conflicts persist after %d attempts", maxRetries)
			}
			// Process failed to start properly - clean up and return error
			process.Stop()
			delete(pm.processes, file)
			return "", fmt.Errorf("process startup failed: %w", err)
		}
		return fmt.Sprintf("%s:%d", host, port), nil
	}

	return "", fmt.Errorf("failed to create process after %d attempts", maxRetries)
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

	args := []string{p.Host, fmt.Sprintf("%d", p.Port)}
	p.Cmd = exec.Command(p.Command, args...)
	p.Cmd.Dir = filepath.Dir(p.Command)

	p.logger.Debug("configuring process command",
		zap.String("command", p.Command),
		zap.Strings("args", args),
		zap.String("working_dir", p.Cmd.Dir),
		zap.String("host_port", fmt.Sprintf("%s:%d", p.Host, p.Port)),
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
		zap.String("host_port", fmt.Sprintf("%s:%d", p.Host, p.Port)),
	)

	if err := p.Cmd.Start(); err != nil {
		p.logger.Error("failed to start command",
			zap.String("command", p.Command),
			zap.Error(err),
		)
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Start output logging goroutines after successful process start
	if stdout != nil {
		go p.logOutput(stdout, "stdout", zap.InfoLevel)
	}
	if stderr != nil {
		go p.logOutput(stderr, "stderr", zap.ErrorLevel)
	}

	p.logger.Info("process started successfully",
		zap.String("command", p.Command),
		zap.Int("pid", p.Cmd.Process.Pid),
		zap.String("host_port", fmt.Sprintf("%s:%d", p.Host, p.Port)),
	)

	go p.monitor()

	return nil
}

func (p *Process) logOutput(pipe io.ReadCloser, streamType string, logLevel zapcore.Level) {
	defer pipe.Close()

	scanner := bufio.NewScanner(pipe)
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

	crashed := p.exitCode != 0
	command := p.Command
	p.mu.Unlock()

	if crashed {
		p.logger.Error("process crashed",
			zap.String("command", command),
			zap.Int("exit_code", p.exitCode),
			zap.Error(err),
		)
	} else {
		p.logger.Info("process exited normally",
			zap.String("command", command),
		)
	}

	p.onExit()
}

func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Cmd == nil || p.Cmd.Process == nil {
		return nil
	}

	p.logger.Info("stopping process",
		zap.String("command", p.Command),
		zap.Int("pid", p.Cmd.Process.Pid),
	)

	if err := p.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- p.Cmd.Wait()
	}()

	select {
	case <-time.After(10 * time.Second):
		p.logger.Warn("process did not shut down gracefully, force killing",
			zap.String("command", p.Command),
			zap.Int("pid", p.Cmd.Process.Pid),
		)
		if err := p.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
		<-done
	case err := <-done:
		if err != nil && !isProcessAlreadyFinished(err) {
			return fmt.Errorf("process exit error: %w", err)
		}
	}

	return nil
}

func isProcessAlreadyFinished(err error) bool {
	if err == nil {
		return true
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.Exited()
	}

	errStr := err.Error()
	if errStr == "signal: terminated" ||
		errStr == "signal: killed" ||
		errStr == "wait: no child processes" {
		return true
	}

	return false
}

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

func (pm *ProcessManager) isPortInUse(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (pm *ProcessManager) waitForPortReady(host string, port int, timeout time.Duration, process *Process) error {
	deadline := time.Now().Add(timeout)
	hostPort := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()

	pm.logger.Info("waiting for port to become ready",
		zap.String("host_port", hostPort),
		zap.Duration("timeout", timeout),
		zap.String("command", process.Command),
	)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	attemptCount := 0
	for {
		// Simple timeout check at the start of each iteration
		if time.Now().After(deadline) {
			pm.logger.Error("timeout waiting for port to become ready",
				zap.String("host_port", hostPort),
				zap.Duration("timeout", timeout),
				zap.Duration("elapsed", time.Since(start)),
				zap.Int("attempts", attemptCount),
				zap.String("command", process.Command),
			)
			return fmt.Errorf("timeout waiting for port %s to become ready after %v", hostPort, timeout)
		}

		select {
		case <-time.After(time.Until(deadline)):
			pm.logger.Error("timeout waiting for port to become ready (select case)",
				zap.String("host_port", hostPort),
				zap.Duration("timeout", timeout),
				zap.Duration("elapsed", time.Since(start)),
				zap.Int("attempts", attemptCount),
				zap.String("command", process.Command),
			)
			return fmt.Errorf("timeout waiting for port %s to become ready after %v", hostPort, timeout)
		case <-ticker.C:
			attemptCount++

			// Check if process is still alive before trying to connect
			if process.Cmd.ProcessState != nil && process.Cmd.ProcessState.Exited() {
				pm.logger.Error("process exited before port became ready",
					zap.String("host_port", hostPort),
					zap.Int("exit_code", process.Cmd.ProcessState.ExitCode()),
					zap.String("command", process.Command),
					zap.Int("attempts", attemptCount),
				)
				return fmt.Errorf("process exited before port became ready (exit code: %d)", process.Cmd.ProcessState.ExitCode())
			}

			conn, err := net.DialTimeout("tcp", hostPort, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				waitTime := time.Since(start)
				pm.logger.Info("port became ready",
					zap.String("host_port", hostPort),
					zap.Duration("wait_time", waitTime),
					zap.Int("attempts", attemptCount),
					zap.String("command", process.Command),
				)
				return nil
			}

			// Log connection failures more frequently for debugging
			if attemptCount%50 == 0 {
				pm.logger.Info("still waiting for port to become ready",
					zap.String("host_port", hostPort),
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

