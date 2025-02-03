package substrate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"math"
	"math/big"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
)

var (
	salt []byte
	pool *caddy.UsagePool
	cmds *caddy.UsagePool
)

func init() {
	pool = caddy.NewUsagePool()
	cmds = caddy.NewUsagePool()

	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("substrate", parseGlobalSubstrate)

	// set up salt
	bi, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		log.Fatalln(err)
	}
	salt = []byte(hex.EncodeToString(bi.Bytes()) + ":")
}

// Interface guards
var (
	_ caddy.Module      = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
	_ caddy.App         = (*App)(nil)
)

type App struct {
	cmds   map[string]*execCmd
	server *Server
	log    *zap.Logger
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
	if h.log == nil {
		h.log = ctx.Logger(h)
	}
	h.log.Info("Provisioning substrate")

	h.cmds = make(map[string]*execCmd)

	obj, _ := pool.LoadOrStore("server", &Server{})
	h.server = obj.(*Server)
	h.server.log = h.log.Named("substrate server")

	h.server.Start()

	return nil
}

func (h *App) registerCmd(c *execCmd) *execCmd {
	key := c.Key()
	if h.cmds[key] != nil {
		return h.cmds[key]
	}

	out, _ := cmds.LoadOrStore(key, c)
	outcmd := out.(*execCmd)
	h.cmds[key] = outcmd
	return outcmd
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
	h.server.WaitForStart(h)

	for _, c := range h.cmds {
		c.host = h.server.Host
		go c.Run()
	}
	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stopping substrate")

	pool.Delete("server")

	for _, c := range h.cmds {
		cmds.Delete(c.Key())
	}

	pool.Delete("cmds")

	return nil
}

