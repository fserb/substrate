package substrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
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
	Host string `json:"-"`

	cmds map[string]*execCmd

	addr caddy.NetworkAddress
	log  *zap.Logger
	salt []byte
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
	h.cmds = make(map[string]*execCmd)

	bi, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return err
	}
	h.salt = []byte(hex.EncodeToString(bi.Bytes()) + ":")

	return nil
}

func (h *App) addSub(sub *execCmd) error {
	key := sub.Key()

	if _, ok := h.cmds[key]; ok {
		return fmt.Errorf("substrate with key %s already exists", key)
	}

	h.cmds[key] = sub
	return nil
}

func (h *App) getSub(key string) *execCmd {
	return h.cmds[key]
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

	if len(h.cmds) == 0 {
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
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	}

	key := r.URL.Path[1:]

	sub, ok := h.cmds[key]
	if !ok {
		http.Error(w, "Not Found", http.StatusNotFound)
		h.log.Error("Substrate not found", zap.String("key", key))
		return
	}

	h.log.Info("Substrate", zap.Any("sub", sub), zap.String("key", key))

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		h.log.Error("Error unmarshalling order", zap.Error(err))
		return
	}

	h.log.Info("Received order", zap.Any("order", order))
	sub.UpdateOrder(order)
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")
	err := h.startServer()
	if err != nil {
		return err
	}

	for _, sub := range h.cmds {
		go sub.Run()
	}
	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")
	for _, sub := range h.cmds {
		sub.Stop()
	}
	return nil
}

