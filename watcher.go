package substrate

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
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
	watcher *fsnotify.Watcher  // File system watcher
	log     *zap.Logger        // Logger
	cancel  context.CancelFunc // Function to cancel the watch goroutine
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
	w.startLoading()

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

			substPath := filepath.Join(w.Root, "substrate")
			if event.Name != substPath {
				continue
			}

			w.log.Debug("Substrate file event", zap.String("event", event.Op.String()))
			w.startLoading()

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
		w.cmd.Stop()
		w.cmd = nil
	}
	w.Order = nil
}

func (w *Watcher) startLoading() {
	substFile := filepath.Join(w.Root, "substrate")
	if _, err := os.Stat(substFile); err != nil {
		w.log.Info("No substrate file found")
		w.stopCommand()
		return
	}

	w.log.Info("Executing substrate")

	cmd := &execCmd{
		Command: []string{substFile},
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

	if cmd.RedirectStdout == nil {
		cmd.RedirectStdout = w.app.RedirectStdout
	}

	if cmd.RedirectStderr == nil {
		cmd.RedirectStderr = w.app.RedirectStderr
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

// IsReady returns true if the watcher has a command with an order
func (w *Watcher) IsReady() bool {
	return w.cmd != nil && w.Order != nil
}

// WaitUntilReady waits for the watcher to be ready or determines it has no substrate
// Returns true if the watcher is ready, false if there's no substrate or timeout occurs
func (w *Watcher) WaitUntilReady(timeout time.Duration) bool {
	// We have an active order working or no substrate.
	if w.cmd == nil || w.Order != nil {
		return true
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.cmd == nil || w.Order != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond) // Short sleep to avoid busy waiting
	}
	return false
}

// GetCmd returns the current command
func (w *Watcher) GetCmd() *execCmd {
	return w.cmd
}

// Submit processes an order from a substrate process
func (w *Watcher) Submit(o *Order) {
	if o == nil {
		w.log.Error("Received nil order")
		return
	}

	w.log.Info("Processing new order",
		zap.String("host", o.Host),
		zap.Int("match_patterns", len(o.Match)),
		zap.Int("paths", len(o.Paths)),
		zap.Int("catch_all", len(o.CatchAll)))

	// Process matchers outside the lock to minimize lock time
	o.matchers = w.processMatchers(o.Match)

	// Sort catch-all patterns by length (longest first) then alphabetically
	if len(o.CatchAll) > 0 {
		sort.Slice(o.CatchAll, func(i, j int) bool {
			if len(o.CatchAll[i]) != len(o.CatchAll[j]) {
				return len(o.CatchAll[i]) > len(o.CatchAll[j])
			}
			return o.CatchAll[i] < o.CatchAll[j]
		})
	}

	w.Order = o
	w.log.Info("New substrate ready and processed")
	clearCache()
}

// processMatchers processes match patterns and returns sorted matchers
func (w *Watcher) processMatchers(patterns []string) []orderMatcher {
	if len(patterns) == 0 {
		w.log.Info("No match patterns to process")
		return nil
	}

	matchers := make([]orderMatcher, 0, len(patterns))

	for _, m := range patterns {
		dir := filepath.Join("/", filepath.Dir(m))
		name := filepath.Base(m)

		// Skip invalid patterns
		if len(name) < 2 || name[0] != '*' || name[1] != '.' {
			w.log.Warn("Skipping invalid match pattern", zap.String("pattern", m))
			continue
		}

		ext := name[1:]
		if dir[len(dir)-1] != '/' {
			dir += "/"
		}

		matchers = append(matchers, orderMatcher{dir, ext})
	}

	// Sort matchers by:
	// 1. Path length (longest first for most specific match)
	// 2. Path name (alphabetically)
	// 3. Extension length (longest first)
	// 4. Extension name (alphabetically)
	sort.Slice(matchers, func(i, j int) bool {
		if len(matchers[i].path) != len(matchers[j].path) {
			return len(matchers[i].path) > len(matchers[j].path)
		}
		if matchers[i].path != matchers[j].path {
			return matchers[i].path < matchers[j].path
		}

		if len(matchers[i].ext) != len(matchers[j].ext) {
			return len(matchers[i].ext) > len(matchers[j].ext)
		}
		return matchers[i].ext < matchers[j].ext
	})

	return matchers
}

