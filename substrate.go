package substrate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("substrate", parseGlobalSubstrate)
}

// Interface guards
var (
	_ caddy.Module      = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
	_ caddy.App         = (*App)(nil)
)

var (
	localSubstrateServer *http.Server
)

type App struct {
	Substrates map[string]*SubstrateHandler `json:"substrates,omitempty"`
	Host       string                       `json:"-"`

	addr caddy.NetworkAddress
	log  *zap.Logger
}

func parseGlobalSubstrate(d *caddyfile.Dispenser, existingVal any) (any, error) {
	app := &App{}

	cur, ok := existingVal.(*App)
	if ok {
		*app = *cur
	}

	return httpcaddyfile.App{
		Name:  "substrate",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "substrate",
		New: func() caddy.Module { return new(App) },
	}
}

func (h *App) Provision(ctx caddy.Context) error {
	h.log = ctx.Logger(h)
	h.log.Info("Provisioning substrate")
	h.Substrates = make(map[string]*SubstrateHandler)
	return nil
}

func (h *App) startServer() error {
	oldServer := localSubstrateServer
	defer func() {
		if oldServer == nil {
			return
		}

		go func(old *http.Server) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := old.Shutdown(ctx)
			if err != nil {
				caddy.Log().Named("substrate").Error("Error shutting down old server", zap.Error(err))
			}
			caddy.Log().Named("substrate").Info("Stopped previous server")
		}(oldServer)
	}()

	localSubstrateServer = nil

	if len(h.Substrates) == 0 {
		return nil
	}

	defaultPort := uint(0)
	// if oldServer != nil {
	// 	_, _, port, err := caddy.SplitNetworkAddress(oldServer.Addr)
	// 	if err != nil {
	// 		return err
	// 	}
	//
	// 	p, err := strconv.ParseUint(port, 10, 16)
	// 	defaultPort = uint(p)
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	addr, err := caddy.ParseNetworkAddressWithDefaults("localhost", "tcp", defaultPort)
	if err != nil {
		return err
	}

	ln, err := addr.Listen(context.TODO(), 0, net.ListenConfig{})
	if err != nil {
		return err
	}

	localSubstrateServer = &http.Server{
		Addr:    addr.String(),
		Handler: h,
	}

	go func() {
		if err := localSubstrateServer.Serve(ln.(net.Listener)); !errors.Is(err, http.ErrServerClosed) {
			h.log.Error("Server shutdown for unknown reason", zap.Error(err))
		}
		h.log.Info("Server stopped")
	}()

	port := ln.(net.Listener).Addr().(*net.TCPAddr).Port
	localSubstrateServer.Addr = fmt.Sprintf("localhost:%d", port)

	h.Host = fmt.Sprintf("http://%s", localSubstrateServer.Addr)

	h.log.Info("Serving substrate:", zap.String("host", h.Host))

	return nil
}

func (h *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.log.Info("Serving HTTP", zap.String("path", r.URL.Path))
	for _, sub := range h.Substrates {
		h.log.Info("Substrate", zap.Any("sub", sub), zap.String("key", sub.Key()))
	}

}

func (h *App) Start() error {
	h.log.Info("Starting substrate")
	err := h.startServer()
	if err != nil {
		return err
	}

	for _, sub := range h.Substrates {
		go sub.Run()
	}
	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")
	for _, sub := range h.Substrates {
		sub.Stop()
	}
	return nil
}

