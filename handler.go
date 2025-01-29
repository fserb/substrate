package substrate

import (
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

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
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
)

type SubstrateHandler struct {
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	User    string            `json:"user,omitempty"`

	log *zap.Logger
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	return nil
	// return next.ServeHTTP(w, r)
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s)

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}
	sub := app.(*App)
	sub.Substrates = append(sub.Substrates, s)

	return nil
}

// Syntax:
//
//		substrate {
//		  command <cmdline>
//	    env <key> <value>
//	    user <username>
//		 	restart_policy always
//			redirect_stdout stdout
//		  redirect_stderr stderr
//		}
func (s *SubstrateHandler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	var h httpcaddyfile.Helper = httpcaddyfile.Helper{Dispenser: d}

	h.Next() // consume directive name

	for h.NextBlock(0) {
		switch h.Val() {
		case "command":
			if !h.NextArg() {
				return h.ArgErr()
			}
			s.Command = append([]string{h.Val()}, h.RemainingArgs()...)

		case "env":
			var envKey, envValue string
			if !h.Args(&envKey, &envValue) {
				return h.ArgErr()
			}
			if s.Env == nil {
				s.Env = map[string]string{}
			}
			s.Env[envKey] = envValue
		case "user":
			var user string

			if !h.Args(&user) {
				return h.ArgErr()
			}

			s.User = user
		}
	}
	return nil
}

func parseSubstrateHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var sm SubstrateHandler
	return &sm, sm.UnmarshalCaddyfile(h.Dispenser)
}

