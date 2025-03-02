package substrate

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// orderMatcher helps match file extensions to paths
type orderMatcher struct {
	path string // Directory path to match
	ext  string // File extension to match (including the dot)
}

// Order represents a command from a substrate process
type Order struct {
	// Host is the upstream server to proxy requests to
	Host string `json:"host,omitempty"`

	// Match contains patterns for matching files by extension
	// Format: "/path/*.ext" where path is a directory and ext is a file extension
	Match []string `json:"match,omitempty"`

	// Paths contains exact paths that should be proxied to the upstream
	Paths []string `json:"paths,omitempty"`

	// CatchAll contains fallback paths to use when no other match is found for a path
	CatchAll []string `json:"catch_all,omitempty"`

	// matchers contains compiled matchers from the Match patterns
	matchers []orderMatcher `json:"-"`
}

// Watcher watches for a substrate file in a root directory and manages
// the lifecycle of substrate processes.
type Watcher struct {
	// Root is the directory to watch for a substrate file
	Root string

	// Order is the current active order from the substrate process
	Order *Order

	cmd     *execCmd           // Current command answering queries
	newCmd  *execCmd           // New command being loaded
	watcher *fsnotify.Watcher  // File system watcher
	log     *zap.Logger        // Logger
	cancel  context.CancelFunc // Function to cancel the watch goroutine
	mutex   sync.Mutex         // Protects access to cmd, newCmd, and Order
	app     *App               // Reference to the parent App
	suburl  string             // URL for the substrate process to communicate with
}

func (w *Watcher) init() error {
	if w.Root == "" {
		return fmt.Errorf("root directory not specified")
	}

	if !path.IsAbs(w.Root) {
		return fmt.Errorf("root directory must be an absolute path")
	}

	// Verify the root directory exists
	if _, err := os.Stat(w.Root); os.IsNotExist(err) {
		return fmt.Errorf("root directory does not exist: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	w.watcher = watcher

	// Watch the root directory
	if err := watcher.Add(w.Root); err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch directory: %w", err)
	}

	// Check if substrate file already exists
	substFile := filepath.Join(w.Root, "substrate")
	if _, err := os.Stat(substFile); err == nil {
		w.startLoading()
	} else {
		if !os.IsNotExist(err) {
			w.log.Warn("Error checking substrate file", zap.Error(err))
		}
		w.mutex.Lock()
		defer w.mutex.Unlock()
		if w.cmd != nil {
			w.cmd.Stop()
			w.cmd = nil
			if w.newCmd != nil {
				w.newCmd.Stop()
				w.newCmd = nil
			}
		}

		w.log.Info("No substrate file found")
	}

	// Start watching for changes
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.watch(ctx)

	return nil
}

// watch monitors the substrate file for changes
func (w *Watcher) watch(ctx context.Context) {
	// Guard against nil watcher at the beginning

	watcher := w.watcher

	if watcher == nil {
		return
	}

	w.log.Debug("Starting file watcher")
	defer w.log.Debug("File watcher stopped")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				w.log.Debug("Watcher events channel closed")
				return
			}

			// Guard against nil watcher with mutex protection
			if w.watcher == nil {
				return
			}

			substPath := filepath.Join(w.Root, "substrate")
			if event.Name != substPath {
				continue
			}

			w.log.Info("Substrate file event", zap.String("event", event.Op.String()))

			switch {
			case event.Op&(fsnotify.Create|fsnotify.Write) != 0:
				// File was created or modified
				w.startLoading()
			case event.Op&fsnotify.Remove != 0:
				// File was removed
				w.mutex.Lock()
				if w.cmd != nil {
					w.cmd.Stop()
					w.cmd = nil
				}
				if w.newCmd != nil {
					w.newCmd.Stop()
					w.newCmd = nil
				}
				w.mutex.Unlock()
				w.log.Info("Substrate file removed")
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

func (w *Watcher) startLoading() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.newCmd != nil {
		w.newCmd.Stop()
	}

	w.log.Info("Executing substrate")

	cmd := &execCmd{
		Command: []string{filepath.Join(w.Root, "substrate")},
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
				}
			}
		}
	}

	// Apply global configuration from app
	if w.app.Env != nil && (cmd.Env == nil || len(cmd.Env) == 0) {
		cmd.Env = w.app.Env
	}

	if cmd.RestartPolicy == "" {
		cmd.RestartPolicy = w.app.RestartPolicy
	}

	if cmd.RedirectStdout == nil {
		cmd.RedirectStdout = w.app.RedirectStdout
	}

	if cmd.RedirectStderr == nil {
		cmd.RedirectStderr = w.app.RedirectStderr
	}

	w.newCmd = cmd

	go w.newCmd.Run()
}

// Close stops watching and cleans up resources
func (w *Watcher) Close() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}

	if w.watcher != nil {
		w.watcher.Close()
		w.watcher = nil
	}

	if w.cmd != nil {
		w.cmd.Stop()
		w.cmd = nil
	}

	if w.newCmd != nil {
		w.newCmd.Stop()
		w.newCmd = nil
	}
}

// IsReady returns true if the watcher has a command with an order
func (w *Watcher) IsReady() bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.cmd != nil && w.Order != nil
}

// WaitUntilReady waits for the watcher to be ready or determines it has no substrate
// Returns true if the watcher is ready, false if there's no substrate
func (w *Watcher) WaitUntilReady(timeout time.Duration) bool {
	// Use mutex to safely check if ready
	w.mutex.Lock()
	if w.cmd != nil && w.Order != nil {
		w.mutex.Unlock()
		return true
	}
	w.mutex.Unlock()

	// If there's no substrate file at all, don't wait
	if _, err := os.Stat(filepath.Join(w.Root, "substrate")); os.IsNotExist(err) {
		return false
	}

	// Wait for the watcher to be ready with timeout
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.mutex.Lock()
		ready := w.cmd != nil && w.Order != nil
		w.mutex.Unlock()

		if ready {
			return true
		}
		time.Sleep(50 * time.Millisecond) // Short sleep to avoid busy waiting
	}

	return false
}

// GetCmd returns the current command
func (w *Watcher) GetCmd() *execCmd {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.cmd
}

// Submit processes an order from a substrate process
func (w *Watcher) Submit(o *Order) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	o.matchers = make([]orderMatcher, 0, len(o.Match))
	for _, m := range o.Match {
		dir := filepath.Join("/", filepath.Dir(m))
		name := filepath.Base(m)
		if name[0] != '*' || name[1] != '.' {
			continue
		}
		ext := name[1:]
		if dir[len(dir)-1] != '/' {
			dir += "/"
		}

		o.matchers = append(o.matchers, orderMatcher{dir, ext})
	}

	sort.Slice(o.matchers, func(i, j int) bool {
		if len(o.matchers[i].path) != len(o.matchers[j].path) {
			return len(o.matchers[i].path) > len(o.matchers[j].path)
		}
		if o.matchers[i].path != o.matchers[j].path {
			return o.matchers[i].path < o.matchers[j].path
		}

		if len(o.matchers[i].ext) != len(o.matchers[j].ext) {
			return len(o.matchers[i].ext) > len(o.matchers[j].ext)
		}
		return o.matchers[i].ext < o.matchers[j].ext
	})

	sort.Slice(o.CatchAll, func(i, j int) bool {
		if len(o.CatchAll[i]) != len(o.CatchAll[j]) {
			return len(o.CatchAll[i]) > len(o.CatchAll[j])
		}
		return o.CatchAll[i] < o.CatchAll[j]
	})

	w.log.Info("New substrate ready")
	w.Order = o
	// Always promote the newCmd to cmd when receiving an order
	// This ensures the watcher is ready as soon as the server receives an order

	if w.newCmd == nil {
		return
	}

	oldCmd := w.cmd
	w.cmd = w.newCmd
	w.newCmd = nil
	if oldCmd != nil {
		w.log.Info("Stopping old substrate process")
		oldCmd.Stop()
	}
}
