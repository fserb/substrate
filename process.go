package substrate

import (
	"context"
	"fmt"
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
	if err := validateFilePath(file); err != nil {
		return "", err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if process, exists := pm.processes[file]; exists {
		process.mu.Lock()
		process.LastUsed = time.Now()
		process.mu.Unlock()
		return fmt.Sprintf("%s:%d", process.Host, process.Port), nil
	}

	host := "localhost"
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		port, err := getFreePort()
		if err != nil {
			return "", fmt.Errorf("failed to get free port: %w", err)
		}

		process := &Process{
			Command:  file,
			Host:     host,
			Port:     port,
			LastUsed: time.Now(),
			onExit:   func() { pm.removeProcess(file) },
			logger:   pm.logger,
		}

		if err := process.start(); err != nil {
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

		if err := pm.waitForPortReady(host, port, time.Duration(pm.startupTimeout)); err != nil {
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
			pm.logger.Warn("process may not be ready to accept connections",
				zap.String("file", file),
				zap.String("host:port", fmt.Sprintf("%s:%d", host, port)),
				zap.Error(err),
			)
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
			errors = append(errors, fmt.Errorf("failed to stop process %s: %w", command, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping processes: %v", errors)
	}

	return nil
}

func (pm *ProcessManager) cleanupLoop() {
	defer pm.wg.Done()

	ticker := time.NewTicker(time.Hour)
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

	if err := configureProcessSecurity(p.Cmd, p.Command); err != nil {
		return fmt.Errorf("failed to configure process security: %w", err)
	}

	if err := p.Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	go p.monitor()

	return nil
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

func (pm *ProcessManager) waitForPortReady(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	hostPort := fmt.Sprintf("%s:%d", host, port)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-time.After(time.Until(deadline)):
			return fmt.Errorf("timeout waiting for port %s to become ready after %v", hostPort, timeout)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", hostPort, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				pm.logger.Info("port became ready",
					zap.String("host:port", hostPort),
					zap.Duration("wait_time", time.Since(deadline.Add(-timeout))),
				)
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for port %s to become ready after %v", hostPort, timeout)
			}
		}
	}
}

