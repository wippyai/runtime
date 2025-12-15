package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/temporal/activity"
	"github.com/wippyai/runtime/service/temporal/client"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/worker"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

const (
	ComponentName boot.Name = "temporal"
)

func Component() boot.Component {
	return boot.New(boot.P{
		Name:      ComponentName,
		DependsOn: []boot.Name{bootcore.RegistryName, bootsystem.FunctionsName, bootsystem.ResourcesName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("temporal")
			if logger == nil {
				return ctx, fmt.Errorf("logger not available")
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, fmt.Errorf("transcoder not available")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available")
			}

			envRegistry := env.GetRegistry(ctx)
			if envRegistry == nil {
				return ctx, fmt.Errorf("env registry not available")
			}

			resourceReg := resource.GetRegistry(ctx)
			if resourceReg == nil {
				return ctx, fmt.Errorf("resource registry not available")
			}

			funcRegistry := funcapi.GetRegistry(ctx)
			if funcRegistry == nil {
				return ctx, fmt.Errorf("function registry not available")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available")
			}

			// Register Temporal dependency patterns
			temporalPatterns := []regapi.DependencyPattern{
				{Path: "data.client", Description: "Reference to Temporal client"},
				{Path: "meta.temporal.activity.worker", Description: "Reference to Temporal worker for activity"},
				{Path: "meta.temporal.workflow.worker", Description: "Reference to Temporal worker for workflow"},
			}
			for _, pattern := range temporalPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register temporal dependency pattern",
						zap.String("path", pattern.Path),
						zap.Error(err))
				}
			}

			// Create data converter with transcoder
			dc := dataconverter.NewDataConverter(dtt, converter.GetDefaultDataConverter())

			// Create client manager
			clientManager, err := client.NewManager(
				logger.Named("client"),
				dtt,
				bus,
				envRegistry,
				dc,
				nil, // client interceptors
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create client manager: %w", err)
			}

			// Create worker manager
			workerManager, err := worker.NewManager(
				logger.Named("worker"),
				dtt,
				bus,
				resourceReg,
				nil, // worker interceptors
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create worker manager: %w", err)
			}

			// Create activity listener to auto-register functions as activities
			activityListener := activity.NewListener(
				logger.Named("activity"),
				workerManager,
			)

			// Register handlers for temporal entry kinds
			handlers.RegisterListener(temporalapi.Client, clientManager)
			handlers.RegisterListener(temporalapi.Worker, workerManager)

			// Register activity observer for function entries with temporal.activity metadata
			// Use RegisterObserver because this is a secondary observer that shouldn't ack events
			handlers.RegisterObserver("function.*", activityListener)
			handlers.RegisterObserver("process.*", activityListener)

			logger.Info("temporal component loaded")

			return ctx, nil
		},
	})
}

func All() []boot.Component {
	return []boot.Component{
		Component(),
	}
}
