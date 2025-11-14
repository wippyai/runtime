package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/service/template"
	"go.uber.org/zap"
)

func Template() boot.Component {
	return boot.New(boot.P{
		Name:      TemplateName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, fmt.Errorf("transcoder not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Register template dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.set",
				Description: "Reference to a template set",
			}); err != nil {
				logger.Warn("failed to register template dependency pattern", zap.Error(err))
			}

			manager := template.NewManager(
				bus,
				dtt,
				logger.Named("tmpl"),
			)

			handlers.RegisterListener("template.(jet|set)", manager)
			return ctx, nil
		},
	})
}
