package core

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/boot"
	contextapi "github.com/ponyruntime/pony/api/context"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"go.uber.org/zap"
)

func TestLoader(t *testing.T) {
	ctx := context.Background()

	// Setup AppContext
	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	// Setup dependencies
	logger, _ := zap.NewDevelopment()
	ctx = logapi.WithLogger(ctx, logger)

	dtt := transcoder.GlobalTranscoder()
	ctx = payload.WithTranscoder(ctx, dtt)

	// Load component
	component := Loader()

	if component.Name() != string(LoaderName) {
		t.Errorf("expected name %q, got %q", LoaderName, component.Name())
	}

	if component.Phase() != boot.Init {
		t.Errorf("expected phase Init, got %v", component.Phase())
	}

	ctx, err := component.Load(ctx)
	if err != nil {
		t.Fatalf("failed to load component: %v", err)
	}

	// Verify loader is in context
	ldr := boot.GetLoader(ctx)
	if ldr == nil {
		t.Fatal("loader not found in context")
	}
}

func TestGetLoader_NoContext(t *testing.T) {
	ctx := context.Background()
	ldr := boot.GetLoader(ctx)
	if ldr != nil {
		t.Error("expected nil loader with no AppContext")
	}
}

func TestGetLoader_NoLoader(t *testing.T) {
	ctx := context.Background()
	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	ldr := boot.GetLoader(ctx)
	if ldr != nil {
		t.Error("expected nil loader when not set")
	}
}
