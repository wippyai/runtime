package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/pidgen"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/internal/uniqid"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

const (
	AppContext = "appcontext"
	Logger     = "logger"
	EventBus   = "eventbus"
	PIDGen     = "pidgen"
)

func init() {
	bootpkg.MustRegister(boot.New(boot.P{
		Name:  AppContext,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			appCtx := contextapi.NewAppContext()
			return contextapi.WithAppContext(ctx, appCtx), nil
		},
	}))

	bootpkg.MustRegister(boot.New(boot.P{
		Name:  Logger,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			logger, err := zap.NewProduction()
			if err != nil {
				return ctx, err
			}
			return logapi.WithLogger(ctx, logger), nil
		},
	}))

	bootpkg.MustRegister(boot.New(boot.P{
		Name:  EventBus,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			bus := eventbus.NewBus()
			return event.WithBus(ctx, bus), nil
		},
	}))

	bootpkg.MustRegister(boot.New(boot.P{
		Name:  PIDGen,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			uniqGen := uniqid.NewGenerator()
			gen := uniqid.NewPIDGenerator(uniqGen)
			return pidgen.WithGenerator(ctx, gen), nil
		},
	}))
}
