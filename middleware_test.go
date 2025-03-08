package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

type mockApp struct {
	getWatcherFunc func(string) *Watcher
}

func (m *mockApp) GetWatcher(root string) *Watcher {
	if m.getWatcherFunc != nil {
		return m.getWatcherFunc(root)
	}
	return nil
}

func TestSubstrateHandlerServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		prefix         string
		watcherExists  bool
		cmdExists      bool
		port           int
		expectedStatus int
		expectedPath   string
		expectProxy    bool
	}{
		{
			name:           "non-matching path",
			path:           "/other/path",
			prefix:         "/app",
			watcherExists:  true,
			cmdExists:      true,
			expectedStatus: http.StatusOK,
			expectedPath:   "/other/path",
			expectProxy:    false,
		},
		{
			name:           "substrate file path",
			path:           "/app/substrate",
			prefix:         "/app",
			watcherExists:  true,
			cmdExists:      true,
			expectedStatus: http.StatusNotFound,
			expectProxy:    false,
		},
		{
			name:           "matching path with no cmd",
			path:           "/app/index.html",
			prefix:         "/app",
			watcherExists:  true,
			cmdExists:      false,
			expectedStatus: http.StatusOK,
			expectedPath:   "/app/index.html",
			expectProxy:    false,
		},
		{
			name:           "matching path with cmd",
			path:           "/app/index.html",
			prefix:         "/app",
			watcherExists:  true,
			cmdExists:      true,
			port:           8080,
			expectedStatus: http.StatusOK,
			expectedPath:   "/index.html",
			expectProxy:    true,
		},
		{
			name:           "encoded path",
			path:           "/app/some%20file.html",
			prefix:         "/app",
			watcherExists:  true,
			cmdExists:      true,
			port:           8080,
			expectedStatus: http.StatusOK,
			expectedPath:   "/some file.html",
			expectProxy:    true,
		},
		{
			name:           "root prefix",
			path:           "/index.html",
			prefix:         "",
			watcherExists:  true,
			cmdExists:      true,
			port:           8080,
			expectedStatus: http.StatusOK,
			expectedPath:   "/index.html",
			expectProxy:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock proxy
			mockProxy := &mockReverseProxy{}

			// Setup mock next handler
			nextHandler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				if tt.expectedPath != r.URL.Path {
					t.Errorf("Expected path %s, got %s", tt.expectedPath, r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
				return nil
			})

			// Setup mock app
			mockAppImpl := &mockApp{
				getWatcherFunc: func(string) *Watcher {
					return nil
				},
			}

			// Setup handler
			handler := &SubstrateHandler{
				Prefix: tt.prefix,
				log:    zap.NewNop(),
				proxy:  mockProxy,
				app:    mockAppImpl,
			}

			// Setup watcher if needed
			if tt.watcherExists {
				watcher := &Watcher{
					Port: tt.port,
					log:  zap.NewNop(),
				}
				if tt.cmdExists {
					watcher.cmd = &execCmd{
						log: zap.NewNop(),
					}
				}
				handler.watcher = watcher
			}

			// Create request
			req := httptest.NewRequest("GET", tt.path, nil)
			req = req.WithContext(context.WithValue(req.Context(), caddyhttp.VarsCtxKey, map[string]any{
				"root": ".",
			}))

			// Create response recorder
			w := httptest.NewRecorder()

			// Call handler
			err := handler.ServeHTTP(w, req, caddyhttp.HandlerFunc(nextHandler))
			if err != nil {
				t.Fatalf("ServeHTTP returned error: %v", err)
			}

			// Check status
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Check if proxy was called
			if tt.expectProxy && !mockProxy.called {
				t.Error("Expected proxy to be called, but it wasn't")
			} else if !tt.expectProxy && mockProxy.called {
				t.Error("Expected proxy not to be called, but it was")
			}

			// Check proxy host if applicable
			if tt.expectProxy {
				expectedHost := "http://localhost:8080"
				if mockProxy.host != expectedHost {
					t.Errorf("Expected proxy host %s, got %s", expectedHost, mockProxy.host)
				}
			}
		})
	}
}

func TestSubstrateHandlerNoWatcher(t *testing.T) {
	// Setup handler with no watcher
	handler := &SubstrateHandler{
		Prefix: "/app",
		log:    zap.NewNop(),
		proxy:  &mockReverseProxy{},
	}

	// Create a mock app that returns nil for GetWatcher
	mockAppImpl := &mockApp{
		getWatcherFunc: func(string) *Watcher {
			return nil
		},
	}

	handler.app = mockAppImpl

	// Create request
	req := httptest.NewRequest("GET", "/app/index.html", nil)
	req = req.WithContext(context.WithValue(req.Context(), caddyhttp.VarsCtxKey, map[string]any{
		"root": ".",
	}))

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler
	err := handler.ServeHTTP(w, req, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		t.Error("Next handler should not be called")
		return nil
	}))

	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	// Check status
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestSubstrateHandlerHeaders(t *testing.T) {
	// Setup mock proxy
	mockProxy := &mockReverseProxy{}

	// Setup handler
	handler := &SubstrateHandler{
		Prefix: "/app",
		log:    zap.NewNop(),
		proxy:  mockProxy,
		app:    &mockApp{},
	}

	// Setup watcher
	handler.watcher = &Watcher{
		Port: 8080,
		log:  zap.NewNop(),
		cmd: &execCmd{
			log: zap.NewNop(),
		},
	}

	// Create request
	req := httptest.NewRequest("GET", "/app/index.html?query=value", nil)
	req.Host = "example.com"

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler
	err := handler.ServeHTTP(w, req, nil)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	// Check headers
	if req.Header.Get("X-Forwarded-Path") != "/app/index.html?query=value" {
		t.Errorf("Expected X-Forwarded-Path header to be '/app/index.html?query=value', got '%s'",
			req.Header.Get("X-Forwarded-Path"))
	}

	if req.Header.Get("X-Forwarded-BaseURL") != "http://example.com" {
		t.Errorf("Expected X-Forwarded-BaseURL header to be 'http://example.com', got '%s'",
			req.Header.Get("X-Forwarded-BaseURL"))
	}
}

