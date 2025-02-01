package substrate

import (
	"encoding/json"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/rewrite"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("_substrate", parseSubstrateHandler)
	httpcaddyfile.RegisterDirectiveOrder("_substrate", httpcaddyfile.Before, "invoke")
	httpcaddyfile.RegisterDirective("substrate", parseSubstrateDirective)
	httpcaddyfile.RegisterDirectiveOrder("substrate", httpcaddyfile.Before, "invoke")
}

const (
	maxTryFiles  = 32
	maxMatchExts = 32
)

// Syntax:
//
//		substrate {
//		  command <cmdline>
//	    env <key> <value>
//	    user <username>
//			dir <directory>
//
//		 	restart_policy always|never|on_failure
//			redirect_stdout stdout|stderr|null|file <filename>
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
		case "dir":
			var dir string
			if !h.Args(&dir) {
				return h.ArgErr()
			}
			s.Dir = dir
		case "redirect_stdout":
			target, err := parseRedirect(h)
			if err != nil {
				return err
			}
			s.RedirectStdout = target
		case "redirect_stderr":
			target, err := parseRedirect(h)
			if err != nil {
				return err
			}
			s.RedirectStderr = target
		case "restart_policy":
			var p string
			if !h.Args(&p) {
				return h.ArgErr()
			}
			if p != "always" && p != "never" && p != "on_failure" {
				return h.Errf("Invalid restart policy: %s", p)
			}
			s.RestartPolicy = p
		}
	}

	if s.RedirectStdout == nil {
		s.RedirectStdout = &outputTarget{Type: "stdout"}
	}
	if s.RedirectStderr == nil {
		s.RedirectStderr = &outputTarget{Type: "stderr"}
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

func parseSubstrateDirective(h httpcaddyfile.Helper) ([]httpcaddyfile.ConfigValue, error) {
	routes := caddyhttp.RouteList{}

	substrateHandler := SubstrateHandler{}
	substrateHandler.UnmarshalCaddyfile(h.Dispenser)
	substrateRoute := caddyhttp.Route{
		HandlersRaw: []json.RawMessage{caddyconfig.JSONModuleObject(substrateHandler, "handler", "substrate", nil)},
	}
	routes = append(routes, substrateRoute)

	files := []string{"{http.request.uri.path}"}
	for i := range maxTryFiles {
		files = append(files, fmt.Sprintf("{http.request.uri.path}{substrate.match_files.%d}", i))
	}
	rewriteMatcherSet := caddy.ModuleMap{
		"file": h.JSON(fileserver.MatchFile{
			TryFiles:  files,
			TryPolicy: "first_exist",
		}),
	}
	rewriteHandler := rewrite.Rewrite{
		URI: "{http.matchers.file.relative}",
	}
	rewriteRoute := caddyhttp.Route{
		MatcherSetsRaw: []caddy.ModuleMap{rewriteMatcherSet},
		HandlersRaw: []json.RawMessage{caddyconfig.JSONModuleObject(rewriteHandler,
			"handler", "rewrite", nil)},
	}
	routes = append(routes, rewriteRoute)

	paths := []string{}
	for i := range maxMatchExts {
		paths = append(paths, fmt.Sprintf("{substrate.match_path.%d}", i))
	}
	reverseProxyMatcherSet := caddy.ModuleMap{
		"path": h.JSON(paths),
	}
	reverseProxyHandler := reverseproxy.Handler{
		Upstreams: reverseproxy.UpstreamPool{
			&reverseproxy.Upstream{
				Dial: "{substrate.host}",
			},
		},
	}
	reverseProxyRoute := caddyhttp.Route{
		MatcherSetsRaw: []caddy.ModuleMap{reverseProxyMatcherSet},
		HandlersRaw: []json.RawMessage{caddyconfig.JSONModuleObject(reverseProxyHandler,
			"handler", "reverse_proxy", nil)},
	}
	routes = append(routes, reverseProxyRoute)

	return []httpcaddyfile.ConfigValue{
		{
			Class: "route",
			Value: caddyhttp.Subroute{Routes: routes},
		},
	}, nil
}

