package core

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
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

	if component.Name() != LoaderName {
		t.Errorf("expected name %q, got %q", LoaderName, component.Name())
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

func TestLoader_NoTranscoder(t *testing.T) {
	ctx := context.Background()

	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	logger, _ := zap.NewDevelopment()
	ctx = logapi.WithLogger(ctx, logger)

	component := Loader()
	_, err := component.Load(ctx)
	if err == nil {
		t.Fatal("expected error when transcoder is missing")
	}
	if !errors.Is(err, ErrTranscoderNotAvailable) {
		t.Fatalf("expected ErrTranscoderNotAvailable, got %v", err)
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
