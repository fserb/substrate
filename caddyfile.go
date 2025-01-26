package substrate

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	httpcaddyfile.RegisterDirective("substrate", parseSubstrate)
}

// Syntax:
//
//		substrate <match> {
//		  command <cmdline>
//	    env <key> <value>
//	    user <username>
//		 	restart_policy always
//			redirect_stdout stdout
//		  redirect_stderr stderr
//		}
func parseSubstrate(h httpcaddyfile.Helper) ([]httpcaddyfile.ConfigValue, error) {
	h.Next() // consume directive name
	// matcherSet, err := h.ExtractMatcherSet()
	// if err != nil {
	// return nil, err
	// }
	// h.Next() // consume the directive name again (matcher parsing resets)

	var server App

	for h.NextBlock(0) {
		switch h.Val() {
		case "command":
			if !h.NextArg() {
				return nil, h.ArgErr()
			}
			server.Command = append([]string{h.Val()}, h.RemainingArgs()...)

		case "env":
			var envKey, envValue string
			if !h.Args(&envKey, &envValue) {
				return nil, h.ArgErr()
			}
			if server.Env == nil {
				server.Env = map[string]string{}
			}
			server.Env[envKey] = envValue
		case "user":
			var user string

			if !h.Args(&user) {
				return nil, h.ArgErr()
			}

			server.User = user
		}
	}

	return nil, nil
}

