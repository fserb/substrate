package substrate

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
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
//	substrate {}
func init() {
	caddy.RegisterModule(SubstrateHandler{})
	httpcaddyfile.RegisterHandlerDirective("substrate", parseSubstrateHandler)
	httpcaddyfile.RegisterDirectiveOrder("substrate", httpcaddyfile.Before, "invoke")
}

// Interface guards
var (
	_ caddy.Module                = (*SubstrateHandler)(nil)
	_ caddy.Provisioner           = (*SubstrateHandler)(nil)
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
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

// Those come from the child process.
type SubstrateHandler struct {
	Cmd   *execCmd `json:"cmd,omitempty"`
	log   *zap.Logger
	app   *App
	fs    fs.FS
	proxy HostReverseProxy
	ctx   *caddy.Context
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.ctx = &ctx
	s.log = ctx.Logger(s)

	fs, ok := ctx.Filesystems().Get("")
	if !ok {
		return fmt.Errorf("no filesystem available")
	}
	s.fs = fs

	repl, ok := ctx.Context.Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	if ok {
		for i, c := range s.Cmd.Command {
			s.Cmd.Command[i] = repl.ReplaceAll(c, "")
		}

		for k, v := range s.Cmd.Env {
			s.Cmd.Env[k] = repl.ReplaceAll(v, "")
		}

		s.Cmd.Dir = repl.ReplaceAll(s.Cmd.Dir, "")
	}

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}

	if s.Cmd != nil {
		s.Cmd = s.Cmd.Register(app.(*App))
	}

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

	err = s.proxy.Provision(*s.ctx)
	if err != nil {
		return fmt.Errorf("error provisioning reverse_proxy: %w", err)
	}

	return nil
}
func (s *SubstrateHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	var h httpcaddyfile.Helper = httpcaddyfile.Helper{Dispenser: d}

	h.Next()
	s.Cmd = &execCmd{}

	// Skip any blocks - we don't need any configuration
	for h.NextBlock(0) {
		// Ignore all directives
	}

	return nil
}

func parseSubstrateHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var sm SubstrateHandler
	return &sm, sm.UnmarshalCaddyfile(h.Dispenser)
}

