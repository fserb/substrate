package substrate

import (
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(adminAPI{})
}

// Interface guards
var (
	_ caddy.Module      = (*adminAPI)(nil)
	_ caddy.AdminRouter = (*adminAPI)(nil)
)

type adminAPI struct {
	app *App
	log *zap.Logger
}

func (adminAPI) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "admin.api.substrate",
		New: func() caddy.Module { return new(adminAPI) },
	}
}

func (a *adminAPI) Provision(ctx caddy.Context) error {
	a.log = ctx.Logger(a)

	_, err := ctx.AppIfConfigured("substrate")
	if err != nil {
		return err
	}

	return nil
}

func (h *adminAPI) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{
			Pattern: "/substrate/",
			Handler: caddy.AdminHandlerFunc(h.handleSubstrate),
		},
	}
}

func (h *adminAPI) handleSubstrate(w http.ResponseWriter, r *http.Request) error {
	uri := strings.TrimPrefix(r.URL.Path, "/substrate")
	parts := strings.Split(uri, "/")
	h.log.Info("parts", zap.Any("parts", parts))
	return nil
}

