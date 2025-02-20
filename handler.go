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
//			substrate {
//			  command <cmdline>
//		    env <key> <value>
//		    user <username>
//				dir <directory>
//	     prefix <url-prefix>
//
//			 	restart_policy always|never|on_failure
//				redirect_stdout stdout|stderr|null|file <filename>
//			  redirect_stderr stderr
//			}
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

	for h.NextBlock(0) {
		switch h.Val() {
		case "command":
			if !h.NextArg() {
				return h.ArgErr()
			}
			s.Cmd.Command = append([]string{h.Val()}, h.RemainingArgs()...)

		case "env":
			var envKey, envValue string
			if !h.Args(&envKey, &envValue) {
				return h.ArgErr()
			}
			if s.Cmd.Env == nil {
				s.Cmd.Env = map[string]string{}
			}
			s.Cmd.Env[envKey] = envValue
		case "user":
			var user string

			if !h.Args(&user) {
				return h.ArgErr()
			}

			s.Cmd.User = user
		case "dir":
			var dir string
			if !h.Args(&dir) {
				return h.ArgErr()
			}
			s.Cmd.Dir = dir
		case "prefix":
			var prefix string
			if !h.Args(&prefix) {
				return h.ArgErr()
			}
			s.Cmd.Prefix = prefix
		case "redirect_stdout":
			target, err := parseRedirect(h)
			if err != nil {
				return err
			}
			s.Cmd.RedirectStdout = target
		case "redirect_stderr":
			target, err := parseRedirect(h)
			if err != nil {
				return err
			}
			s.Cmd.RedirectStderr = target
		case "restart_policy":
			var p string
			if !h.Args(&p) {
				return h.ArgErr()
			}
			if p != "always" && p != "never" && p != "on_failure" {
				return h.Errf("Invalid restart policy: %s", p)
			}
			s.Cmd.RestartPolicy = p
		}
	}

	return nil
}

func parseRedirect(h httpcaddyfile.Helper) (*outputTarget, error) {
	if !h.NextArg() {
		return nil, h.ArgErr()
	}

	var target outputTarget
	target.Type = h.Val()

	switch target.Type {
	case "stdout", "null", "stderr":
		return &target, nil
	case "file":
		if !h.NextArg() {
			return nil, h.ArgErr()
		}
		target.File = h.Val()
		return &target, nil
	}

	return nil, h.Errf("Invalid redirect target: %s", target.Type)
}

func parseSubstrateHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var sm SubstrateHandler
	return &sm, sm.UnmarshalCaddyfile(h.Dispenser)
}

