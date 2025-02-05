package substrate

import (
	"context"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

func CheckUsagePool(t *testing.T) {
	pool.Range(func(key, value any) bool {
		ref, _ := pool.References(key)
		t.Errorf("Pool missing delete for key '%s' (%d)", key, ref)
		return true
	})
	cmds.Range(func(key, value any) bool {
		ref, _ := pool.References(key)
		t.Errorf("Commands missing delete for key '%s' (%d)", key, ref)
		return true
	})
}

func TestApp_Start_Stop_UsagePool(t *testing.T) {
	// Create a new App instance
	app := &App{}
	app.log = zap.NewNop()

	ctx, _ := caddy.NewContext(caddy.Context{Context: context.Background()})
	err := app.Provision(ctx)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	ex := &execCmd{}
	if err != nil {
		t.Fatalf("execCmd Provision failed: %v", err)
	}
	app.registerCmd(ex)

	err = app.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if n, ok := pool.References("server"); !ok || n != 1 {
		t.Fatal("Expected server to be in the pool, but it was not")
	}

	if len(app.cmds) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(app.cmds))
	}

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	CheckUsagePool(t)
}

