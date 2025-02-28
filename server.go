package substrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	httpcache "github.com/caddyserver/cache-handler"
	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

type Server struct {
	http.Server
	Host string

	app     *App
	readyCh chan struct{}
	log     *zap.Logger
}

var (
	_ caddy.Destructor = (*Server)(nil)
)

func (s *Server) Start() error {
	if s.readyCh != nil {
		return nil
	}

	s.readyCh = make(chan struct{})

	addr, err := caddy.ParseNetworkAddressWithDefaults("localhost", "tcp", 0)
	if err != nil {
		return fmt.Errorf("failed to parse network address: %w", err)
	}

	ln, err := addr.Listen(context.Background(), 0, net.ListenConfig{})
	if err != nil {
		return fmt.Errorf("failed to listen on address: %w", err)
	}
	listener, ok := ln.(net.Listener)
	if !ok {
		return fmt.Errorf("unexpected listener type: %T", ln)
	}

	s.Server = http.Server{Handler: s}

	go func() {
		if err := s.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("Server shutdown unexpectedly", zap.Error(err))
		}
		s.log.Info("Server stopped")
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	s.Host = fmt.Sprintf("http://localhost:%d", port)

	s.log.Info("Substrate server running", zap.String("host", s.Host))

	close(s.readyCh)

	return nil
}

func (s *Server) WaitForStart(app *App) {
	s.app = app
	<-s.readyCh
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.Shutdown(ctx)
	if err != nil {
		s.log.Named("substrate").Error("Error shutting down old server", zap.Error(err))
	}
	s.log.Named("substrate").Info("Stopped previous server")
	s.readyCh = nil
	s.Host = ""
	s.app = nil
}

func (s *Server) Destruct() error {
	s.Stop()

	// Clean up all watchers
	watcherPool.Range(func(key, value any) bool {
		watcher := value.(*Watcher)
		watcher.Close()
		return true
	})

	return nil
}

func clearCache() error {
	currentCtx := caddy.ActiveContext()

	app, err := currentCtx.AppIfConfigured("cache")
	if err != nil {
		return nil
	}

	handler, ok := app.(*httpcache.SouinApp)
	if !ok {
		return nil
	}

	cache := &httpcache.SouinCaddyMiddleware{}
	cache.FromApp(handler)

	cache.Cleanup()

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	if r.Method == "GET" && r.URL.Path == "/reset" {
		err := clearCache()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			if s.log != nil {
				s.log.Error("Error clearing cache", zap.Error(err))
			}
			return
		}
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	}

	key := r.URL.Path[1:]

	// Find the watcher with this key
	watcher := GetWatcher(key)
	if watcher == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		if s.log != nil {
			s.log.Error("Substrate not found", zap.String("key", key))
		}
		return
	}

	// Get the command from the watcher
	watcher.mutex.Lock()
	var cmd *execCmd
	if watcher.cmd != nil {
		cmd = watcher.cmd
	} else if watcher.newCmd != nil {
		cmd = watcher.newCmd
	}
	watcher.mutex.Unlock()

	if cmd == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		if s.log != nil {
			s.log.Error("Substrate not found", zap.String("key", key))
		}
		return
	}

	if s.log != nil {
		s.log.Info("Substrate", zap.String("key", key))
	}

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		if s.log != nil {
			s.log.Error("Error unmarshalling order", zap.Error(err))
		}
		return
	}

	if s.log != nil {
		s.log.Info("Received order", zap.String("key", key), zap.Any("order", order))
	}

	watcher.Submit(&order)
}

