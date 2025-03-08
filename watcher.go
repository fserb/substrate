package substrate

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// Watcher watches for a substrate file in a root directory and manages
// the lifecycle of substrate processes.
type Watcher struct {
	// Root is the directory to watch for a substrate file
	Root string
	Port int

	cmd     *execCmd           // Current command answering queries
	watcher *fsnotify.Watcher  // File system watcher
	log     *zap.Logger        // Logger
	cancel  context.CancelFunc // Function to cancel the watch goroutine
	app     *App               // Reference to the parent App
}

// updateWatcher configures the watcher based on whether the substrate file exists
func (w *Watcher) updateWatcher() error {
	if w.watcher == nil {
		return fmt.Errorf("watcher not initialized")
	}

	substPath := filepath.Join(w.Root, "substrate")

	// Check if substrate file exists
	fileExists := false
	if _, err := os.Stat(substPath); err == nil {
		fileExists = true
	}

	// Remove any existing watches
	w.watcher.Remove(w.Root)
	w.watcher.Remove(substPath)

	if fileExists {
		// Watch the file directly
		if err := w.watcher.Add(substPath); err != nil {
			return fmt.Errorf("failed to watch substrate file: %w", err)
		}
		w.log.Debug("Watching substrate file", zap.String("path", substPath))
		w.startLoading()
	} else {
		// Watch the directory to detect creation
		if err := w.watcher.Add(w.Root); err != nil {
			return fmt.Errorf("failed to watch directory: %w", err)
		}
		w.log.Debug("Watching directory for substrate file", zap.String("dir", w.Root))
		w.stopCommand()
	}

	return nil
}

func (w *Watcher) init() error {
	if w.Root == "" || !path.IsAbs(w.Root) {
		return fmt.Errorf("root directory must be an absolute path (%s)", w.Root)
	}

	if _, err := os.Stat(w.Root); os.IsNotExist(err) {
		return fmt.Errorf("root directory does not exist: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	w.watcher = watcher

	// Configure the watcher based on substrate file existence
	if err := w.updateWatcher(); err != nil {
		watcher.Close()
		return err
	}

	// Start watching for changes
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.watch(ctx)

	return nil
}

// watch monitors the substrate file for changes
func (w *Watcher) watch(ctx context.Context) {
	watcher := w.watcher

	if watcher == nil {
		return
	}

	w.log.Debug("Starting file watcher")
	defer w.log.Debug("File watcher stopped")

	substPath := filepath.Join(w.Root, "substrate")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				w.log.Debug("Watcher events channel closed")
				return
			}

			if w.watcher == nil {
				return
			}

			if event.Name != substPath {
				continue
			}

			w.log.Debug("File event", zap.String("path", event.Name), zap.String("event", event.Op.String()))

			if err := w.updateWatcher(); err != nil {
				w.log.Error("Failed to update watcher", zap.Error(err))
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				w.log.Debug("Watcher errors channel closed")
				return
			}
			// Guard against nil logger
			if w.log != nil {
				w.log.Error("Watcher error", zap.Error(err))
			}

			if w.watcher == nil {
				return
			}

		case <-ctx.Done():
			w.log.Debug("Watcher context cancelled")
			return
		}
	}
}

func (w *Watcher) stopCommand() {
	if w.cmd != nil {
		w.log.Info("Stopping existing substrate process")
		w.WriteStatusLog("A", "Stopping existing substrate process")
		w.cmd.Stop()
		w.cmd = nil
	}
}

func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (w *Watcher) startLoading() {
	substFile := filepath.Join(w.Root, "substrate")
	if _, err := os.Stat(substFile); err != nil {
		w.log.Info("No substrate file found")
		w.WriteStatusLog("A", "No substrate file found")
		w.stopCommand()
		return
	}

	w.log.Info("Executing substrate")
	w.WriteStatusLog("A", "Executing substrate")

	port, err := GetFreePort()
	if err != nil {
		w.log.Error("Failed to get free port", zap.Error(err))
		w.WriteStatusLog("A", "Failed to get free port")
		w.stopCommand()
		return
	}
	w.Port = port

	cmd := &execCmd{
		Command: []string{substFile, fmt.Sprintf("%d", w.Port)},
		Dir:     w.Root,
		watcher: w,
		log:     w.log,
	}

	// If we're root, change to the file owner
	if os.Geteuid() == 0 {
		fileInfo, err := os.Stat(cmd.Command[0])
		if err == nil {
			if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
				uid := stat.Uid
				u, err := user.LookupId(fmt.Sprintf("%d", uid))
				if err == nil {
					cmd.User = u.Username
					w.log.Info("Running as file owner", zap.String("user", cmd.User))
					w.WriteStatusLog("A", fmt.Sprintf("Running as file owner: %s", cmd.User))
				}
			}
		}
	}

	// Apply global configuration from app
	if w.app.Env != nil && (cmd.Env == nil || len(cmd.Env) == 0) {
		cmd.Env = w.app.Env
	}

	w.stopCommand()
	w.cmd = cmd
	go w.cmd.Run()
}

// Close stops watching and cleans up resources
func (w *Watcher) Close() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.watcher != nil {
		w.watcher.Close()
		w.watcher = nil
	}
	w.stopCommand()
}

// GetCmd returns the current command
func (w *Watcher) GetCmd() *execCmd {
	return w.cmd
}

// WriteStatusLog writes a message to the status log with the given message type:
// "S" for stdout, "E" for stderr, or "A" for status/app messages
func (w *Watcher) WriteStatusLog(msgType, message string) {
	if w.app == nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	logLine := fmt.Sprintf("[%s] %s(%s): %s\n", timestamp, w.Root, msgType, message)

	switch w.app.StatusLog.Type {
	case "stdout":
		fmt.Fprint(os.Stdout, logLine)
	case "stderr":
		fmt.Fprint(os.Stderr, logLine)
	case "file":
		if w.app.statusLogFD != nil {
			fmt.Fprint(w.app.statusLogFD, logLine)
		}
	case "null":
		// Do nothing
	}
}

