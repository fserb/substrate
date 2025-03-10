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
	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"
)

const (
	LRUCacheSize  = 256
	debounceDelay = 100 * time.Millisecond
)

// Watcher watches for a substrate file in a root directory and manages
// the lifecycle of substrate processes.
type Watcher struct {
	// Root is the directory to watch for a substrate file
	Root string
	Port int

	cmd         *execCmd           // Current command answering queries
	watcher     *fsnotify.Watcher  // File system watcher
	log         *zap.Logger        // Logger
	cancel      context.CancelFunc // Function to cancel the watch goroutine
	app         *App               // Reference to the parent App
	isReady     chan struct{}      // Channel to signal when the substrate process is ready
	statusCache *lru.Cache[string, bool]
}

// updateWatcher configures the watcher based on whether the substrate file exists.
// It sets up appropriate watches on either the file or directory and triggers
// command loading or stopping as needed.
func (w *Watcher) updateWatcher() error {
	if w.watcher == nil {
		return fmt.Errorf("watcher not initialized")
	}

	substPath := filepath.Join(w.Root, "substrate")

	fileExists := false
	if _, err := os.Stat(substPath); err == nil {
		fileExists = true
	}

	w.watcher.Remove(w.Root)
	w.watcher.Remove(substPath)

	if fileExists {
		if err := w.watcher.Add(substPath); err != nil {
			return fmt.Errorf("failed to watch substrate file: %w", err)
		}
		w.log.Debug("Watching substrate file", zap.String("path", substPath))
		w.startLoading()
	} else {
		if err := w.watcher.Add(w.Root); err != nil {
			return fmt.Errorf("failed to watch directory: %w", err)
		}
		w.log.Debug("Watching directory for substrate file", zap.String("dir", w.Root))
		w.stopCommand()
		w.setIsReady()
	}

	return nil
}

// init initializes the watcher by validating the root directory,
// creating a file system watcher, and starting the watch goroutine.
func (w *Watcher) init() error {
	if w.Root == "" || !path.IsAbs(w.Root) {
		return fmt.Errorf("root directory must be an absolute path (%s)", w.Root)
	}

	if _, err := os.Stat(w.Root); os.IsNotExist(err) {
		return fmt.Errorf("root directory does not exist: %w", err)
	}

	cache, err := lru.New[string, bool](LRUCacheSize)
	if err != nil {
		return fmt.Errorf("error creating bypass cache: %w", err)
	}
	w.statusCache = cache

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	w.watcher = watcher

	w.setNotReady()

	if err := w.updateWatcher(); err != nil {
		watcher.Close()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.watch(ctx)

	return nil
}

// watch monitors the substrate file for changes in a separate goroutine.
// It responds to file system events and errors, updating the watcher state
// accordingly. Events are debounced to avoid processing multiple events
// that occur in quick succession.
func (w *Watcher) watch(ctx context.Context) {
	watcher := w.watcher

	if watcher == nil {
		return
	}

	w.log.Debug("Starting file watcher")
	defer w.log.Debug("File watcher stopped")

	substPath := filepath.Join(w.Root, "substrate")

	var debounceTimer *time.Timer

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

			if debounceTimer == nil {
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					w.log.Debug("Processing debounced events")
					if err := w.updateWatcher(); err != nil {
						w.log.Error("Failed to update watcher", zap.Error(err))
					}
				})
			}

			w.statusCache.Purge()
			w.setNotReady()
			debounceTimer.Reset(debounceDelay)
			if w.cmd == nil {
				w.cmd = &execCmd{}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				w.log.Debug("Watcher errors channel closed")
				return
			}
			if w.log != nil {
				w.log.Error("Watcher error", zap.Error(err))
			}

			if w.watcher == nil {
				return
			}

		case <-ctx.Done():
			w.log.Debug("Watcher context cancelled")
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
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

// GetFreePort finds an available TCP port by binding to port 0
// and retrieving the assigned port number.
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

// startLoading creates and starts a new substrate process.
// It allocates a free port, configures the command with appropriate
// environment and user settings, and launches it in a separate goroutine.
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

	if w.app.Env != nil && (cmd.Env == nil || len(cmd.Env) == 0) {
		cmd.Env = w.app.Env
	}

	w.setNotReady()
	w.stopCommand()
	w.cmd = cmd
	w.statusCache.Purge()
	go w.cmd.Run()

	if w.cmd == nil {
		w.setIsReady()
		return
	}

	server := fmt.Sprintf("localhost:%d", w.Port)

	delay := 1 * time.Millisecond
	go func() {
		for {
			conn, err := net.Dial("tcp", server)
			if err == nil {
				conn.Close()
				w.WriteStatusLog("A", "Substrate process is ready")
				w.setIsReady()
				return
			}
			time.Sleep(delay)
			delay *= 2
			if delay > 1*time.Second {
				delay = 1 * time.Second
			}
		}
	}()
}

func (w *Watcher) setNotReady() {
	if w.isReady != nil {
		select {
		case _, ok := <-w.isReady:
			if !ok {
				w.isReady = nil
			}
		default:
		}
	}
	if w.isReady == nil {
		w.isReady = make(chan struct{})
	}
}

func (w *Watcher) setIsReady() {
	if w.isReady != nil {
		close(w.isReady)
	}
}

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

// WriteStatusLog writes a message to the status log
// The output destination is determined by the app's StatusLog configuration.
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
