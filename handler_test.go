package substrate

import (
	"net/http"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Mock implementation of HostReverseProxy for testing
type mockReverseProxy struct {
	host   string
	called bool
}

func (m *mockReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	m.called = true
	return nil
}

func (m *mockReverseProxy) Provision(ctx caddy.Context) error {
	return nil
}

func (m *mockReverseProxy) SetHost(host string) {
	m.host = host
}

func TestHandlerCaddyModule(t *testing.T) {
	handler := SubstrateHandler{}

	info := handler.CaddyModule()

	if info.ID != "http.handlers.substrate" {
		t.Errorf("Expected module ID 'http.handlers.substrate', got '%s'", info.ID)
	}

	// Test that the New function returns a new SubstrateHandler
	module := info.New()
	_, ok := module.(*SubstrateHandler)
	if !ok {
		t.Errorf("Expected New() to return *SubstrateHandler, got %T", module)
	}
}

func TestReverseProxySetHost(t *testing.T) {
	// Test the ReverseProxy.SetHost method
	proxy := &mockReverseProxy{}

	proxy.SetHost("http://example.com")

	if proxy.host != "http://example.com" {
		t.Errorf("Expected host to be set to 'http://example.com', got '%s'", proxy.host)
	}
}
