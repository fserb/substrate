package substrate

import (
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
	// _ caddyfile.Unmarshaler = (*App)(nil)
)

type App struct {
	Substrates [](*SubstrateMiddleware)
	log        *zap.Logger
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

// func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
//
// 	d.Next() // consume directive name
//
// 	// require an argument
// 	if !d.NextArg() {
// 		return d.ArgErr()
// 	}
//
// 	// store the argument
// 	// a.Output = d.Val()
// 	return nil
// }

func (h *App) Provision(ctx caddy.Context) error {
	h.log = ctx.Logger(h)
	h.log.Info("Provisioning substrate")
	h.Substrates = make([]*SubstrateMiddleware, 0)
	return nil
}

func (h *App) Start() error {
	h.log.Info("Starting substrate")

	for _, sub := range h.Substrates {
		h.log.Info("Substrate", zap.Any("sub", sub))
	}

	return nil
}

func (h *App) Stop() error {
	h.log.Info("Stoppping substrate")
	return nil
}

