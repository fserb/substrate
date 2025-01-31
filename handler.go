package substrate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/rewrite"
	"go.uber.org/zap"
)

const (
	minRestartDelay   = 1 * time.Second
	maxRestartDelay   = 5 * time.Minute
	resetRestartDelay = 10 * time.Minute
)

func init() {
	rand.Seed(time.Now().UnixNano())
	caddy.RegisterModule(SubstrateHandler{})

	httpcaddyfile.RegisterHandlerDirective("_substrate", parseSubstrateHandler)
	httpcaddyfile.RegisterDirectiveOrder("_substrate", httpcaddyfile.Before, "invoke")
	httpcaddyfile.RegisterDirective("substrate", parseSubstrateDirective)
	httpcaddyfile.RegisterDirectiveOrder("substrate", httpcaddyfile.Before, "invoke")

}

// Interface guards
var (
	_ caddy.Module                = (*SubstrateHandler)(nil)
	_ caddy.Provisioner           = (*SubstrateHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*SubstrateHandler)(nil)
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
)

type Order struct {
	Host     string   `json:"host,omitempty"`
	TryFiles []string `json:"try_files,omitempty"`
	Match    []string `json:"match,omitempty"`
}

type SubstrateHandler struct {
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	User    string            `json:"user,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	N       int               `json:"n,omitempty"`

	Order Order `json:"-"`

	rph         *reverseproxy.Handler
	route       caddyhttp.RouteList
	keepRunning bool
	cmd         *exec.Cmd
	log         *zap.Logger
	app         *App
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) Key() string {
	out, err := json.Marshal(s)
	if err != nil {
		s.log.Error("Error marshalling child", zap.Error(err))
		return ""
	}

	hash := sha1.Sum(out)
	return hex.EncodeToString(hash[:])
}

func (s *SubstrateHandler) Run() {
	s.keepRunning = true
	restartDelay := minRestartDelay

	// Originally based on candy-supervisor
	for s.keepRunning {
		s.cmd = exec.Command(s.Command[0], s.Command[1:]...)
		configureSysProcAttr(s.cmd)

		env := os.Environ()
		for key, value := range s.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		env = append(env, fmt.Sprintf("SUBSTRATE=%s/%s", s.app.Host, s.Key()))
		s.cmd.Env = env

		configureExecutingUser(s.cmd, s.User)

		s.cmd.Dir = s.Dir

		// TODO: stdout stderr

		start := time.Now()
		err := s.cmd.Start()

		if err != nil {
			s.log.Error("Error starting command", zap.Error(err))
			break
		}

		s.log.Info("Started command", zap.String("command", s.Command[0]), zap.Int("pid", s.cmd.Process.Pid))

		err = s.cmd.Wait()

		duration := time.Now().Sub(start)

		// TODO: stdout stderr

		if err != nil {
			s.log.Error("Process exited", zap.Error(err))
		}

		if err == nil || duration > resetRestartDelay {
			restartDelay = minRestartDelay
		}
		if err != nil && s.keepRunning {
			s.log.Info("Restarting in", zap.Duration("delay", restartDelay))
			time.Sleep(restartDelay)
			restartDelay = 2 * restartDelay
			if restartDelay > maxRestartDelay {
				restartDelay = maxRestartDelay
			}
		}
	}
}

func (s *SubstrateHandler) Stop() {
	s.keepRunning = false
	if !s.cmdRunning() {
		return
	}

	err := s.cmd.Process.Signal(os.Interrupt)
	if err != nil {
		s.log.Error("Error sending interrupt signal, killing.")
		s.cmd.Process.Kill()
		return
	}

	go func() {
		time.Sleep(5 * time.Second)
		if s.cmdRunning() {
			s.log.Error("Process did not respond to interrupt, killing")
			s.cmd.Process.Kill()
		}
	}()
	s.cmd.Wait()
}

func (s *SubstrateHandler) cmdRunning() bool {
	return s.cmd != nil && s.cmd.Process != nil && (s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited())
}

func (s *SubstrateHandler) JSON(val any) json.RawMessage {
	out, err := json.Marshal(val)
	if err != nil {
		s.log.Warn("Error marshalling", zap.Error(err))
		return nil
	}
	return out
}

func (s *SubstrateHandler) UpdateOrder(order Order) {
	s.Order = order

	routes := caddyhttp.RouteList{}

	if len(order.TryFiles) > 0 {
		fmt.Printf("TRYFILES %v\n", order.TryFiles)

		files := []string{"{http.request.uri.path}"}

		for _, file := range order.TryFiles {
			files = append(files, "{http.request.uri.path}"+file)
		}

		fmt.Printf("FILES %v\n", files)

		rewriteMatcherSet := caddy.ModuleMap{
			"file": s.JSON(fileserver.MatchFile{
				TryFiles: []string{"{http.request.uri.path}",
					"{http.request.uri.path}/index.md",
					"index.md"},
				TryPolicy: "first_exist_fallback",
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
	}

	s.route = routes
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	repl.Map(func(key string) (any, bool) {
		if len(key) < 22 || key[:22] != "substrate.match_files." {
			return nil, false
		}
		number, err := strconv.Atoi(key[22:])
		if err != nil || number < 0 || number >= len(s.Order.TryFiles) {
			return nil, false
		}
		return s.Order.TryFiles[number], true
	})

	// return s.route.Compile(next).ServeHTTP(w, r)
	return next.ServeHTTP(w, r)
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s).With(zap.Strings("cmd", s.Command))

	s.N = rand.Int()

	// rp, err := caddy.GetModule("http.handlers.reverse_proxy")
	// if err != nil {
	// return err
	// }
	// obj := rp.New()

	// ctx := caddy.NewContext(r.Context())
	// rph := obj.(*reverseproxy.Handler)
	// rph.Provision(ctx)
	// s.rph = rph
	// fmt.Println("RPH", rph)

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}
	s.app = app.(*App)
	s.app.Substrates[s.Key()] = s

	return nil
}

// Syntax:
//
//		substrate {
//		  command <cmdline>
//	    env <key> <value>
//	    user <username>
//			dir <directory>
//
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
		case "dir":
			var dir string
			if !h.Args(&dir) {
				return h.ArgErr()
			}
			s.Dir = dir
		}
	}
	return nil
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

	for i := range 64 {
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

	subroute := caddyhttp.Subroute{
		Routes: routes,
	}

	return []httpcaddyfile.ConfigValue{
		{
			Class: "route",
			Value: subroute,
		},
	}, nil
}

