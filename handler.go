package substrate

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"go.uber.org/zap"
)

const (
	minRestartDelay   = 1 * time.Second
	maxRestartDelay   = 5 * time.Minute
	resetRestartDelay = 10 * time.Minute
)

// Syntax:
//
//	substrate
func init() {
	caddy.RegisterModule(SubstrateHandler{})
	httpcaddyfile.RegisterHandlerDirective("substrate", parseSubstrateHandler)
	httpcaddyfile.RegisterDirectiveOrder("substrate", httpcaddyfile.Before, "invoke")
}

// Interface guards
var (
	_ caddy.Module                = (*SubstrateHandler)(nil)
	_ caddy.Provisioner           = (*SubstrateHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*SubstrateHandler)(nil)
)

type HostReverseProxy interface {
	caddyhttp.MiddlewareHandler
	caddy.Provisioner

	SetHost(string)
}

type ReverseProxy struct{ *reverseproxy.Handler }

func (s *ReverseProxy) SetHost(host string) {
	s.Upstreams[0].Dial = host
}

// SubstrateHandler handles requests by proxying to a substrate process
type SubstrateHandler struct {
	log     *zap.Logger
	app     *App
	fs      fs.FS
	proxy   HostReverseProxy
	watcher *Watcher
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s)

	fs, ok := ctx.Filesystems().Get("")
	if !ok {
		return fmt.Errorf("no filesystem available")
	}
	s.fs = fs

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}
	s.app = app.(*App)

	mod, err := caddy.GetModule("http.handlers.reverse_proxy")
	if err != nil {
		return fmt.Errorf("error getting reverse_proxy module: %w", err)
	}

	handler := mod.New().(*reverseproxy.Handler)
	handler.Upstreams = reverseproxy.UpstreamPool{
		&reverseproxy.Upstream{
			Dial: "",
		},
	}
	s.proxy = &ReverseProxy{handler}

	err = s.proxy.Provision(ctx)
	if err != nil {
		return fmt.Errorf("error provisioning reverse_proxy: %w", err)
	}

	return nil
}

func parseSubstrateHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	return &SubstrateHandler{}, nil
}

