package core

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/pidgen"
	bootpkg "github.com/ponyruntime/pony/boot"
)

func TestCorePlugins(t *testing.T) {
	loader := bootpkg.New()

	ctx, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if logapi.GetLogger(ctx) == nil {
		t.Error("logger not available in context")
	}

	if event.GetBus(ctx) == nil {
		t.Error("event bus not available in context")
	}

	if pidgen.GetGenerator(ctx) == nil {
		t.Error("PID generator not available in context")
	}
}
