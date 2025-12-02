package core

import (
	"testing"

	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	bootpkg "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

func TestCorePlugins(t *testing.T) {
	logger := zap.NewExample()
	ctx, err := bootpkg.NewBootstrapContext(logger, nil)
	if err != nil {
		t.Fatalf("NewBootstrapContext() error = %v", err)
	}

	loader, err := bootpkg.NewLoader(All()...)
	if err != nil {
		t.Fatalf("NewLoader() error = %v", err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if logapi.GetLogger(ctx) == nil {
		t.Error("logger not available in context")
	}

	if event.GetBus(ctx) == nil {
		t.Error("event bus not available in context")
	}

	if process.GetPIDGenerator(ctx) == nil {
		t.Error("PID generator not available in context")
	}
}
