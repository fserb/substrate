package substrate

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

const (
	minRestartDelay   = 1 * time.Second
	maxRestartDelay   = 5 * time.Minute
	resetRestartDelay = 10 * time.Minute
)

func init() {
	caddy.RegisterModule(SubstrateHandler{})
}

// Interface guards
var (
	_ caddy.Module                = (*SubstrateHandler)(nil)
	_ caddy.Provisioner           = (*SubstrateHandler)(nil)
	_ caddyfile.Unmarshaler       = (*SubstrateHandler)(nil)
	_ caddyhttp.MiddlewareHandler = (*SubstrateHandler)(nil)
)

// Those come from the child process.
type SubstrateHandler struct {
	Cmd *execCmd `json:"cmd,omitempty"`
	log *zap.Logger
	app *App
	fs  fs.FS
}

func (s SubstrateHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.substrate",
		New: func() caddy.Module { return new(SubstrateHandler) },
	}
}

func (s *SubstrateHandler) Provision(ctx caddy.Context) error {
	s.log = ctx.Logger(s)

	fs, ok := ctx.Filesystems().Get("")
	if !ok {
		return fmt.Errorf("no filesystem available")
	}
	s.fs = fs

	app, err := ctx.App("substrate")
	if err != nil {
		return err
	}

	if s.Cmd != nil {
		s.Cmd = s.Cmd.Register(app.(*App))
	}

	return nil
}

