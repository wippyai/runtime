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
	temporalinterceptor "github.com/wippyai/runtime/service/temporal/interceptor"
	"github.com/wippyai/runtime/service/temporal/worker"
	temporalworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	"go.temporal.io/sdk/converter"
	sdkinterceptor "go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// InterceptorComponent creates the interceptor registries that other components can use to register interceptors
func InterceptorComponent() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			clientReg := temporalinterceptor.NewClientRegistry()
			workerReg := temporalinterceptor.NewWorkerRegistry()

			ctx = temporalapi.WithClientInterceptorRegistry(ctx, clientReg)
			ctx = temporalapi.WithWorkerInterceptorRegistry(ctx, workerReg)

			return ctx, nil
		},
	})
}

func Component() boot.Component {
	return boot.New(boot.P{
		Name:      Name,
		DependsOn: []boot.Name{bootcore.RegistryName, bootsystem.FunctionsName, bootsystem.ResourcesName, InterceptorName},
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

			// Get interceptor registries
			clientInterceptorReg := temporalapi.GetClientInterceptorRegistry(ctx)
			workerInterceptorReg := temporalapi.GetWorkerInterceptorRegistry(ctx)

			// Register Temporal dependency patterns
			temporalPatterns := []regapi.DependencyPattern{
				{Path: "client", Description: "Reference to Temporal client"},
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

			// Collect interceptors from registries
			var clientInterceptors []sdkinterceptor.ClientInterceptor
			var workerInterceptors []sdkinterceptor.WorkerInterceptor
			if clientInterceptorReg != nil {
				clientInterceptors = clientInterceptorReg.GetAll()
			}
			if workerInterceptorReg != nil {
				workerInterceptors = workerInterceptorReg.GetAll()
			}

			// Create client manager
			clientManager, err := client.NewManager(
				client.WithLogger(logger.Named("client")),
				client.WithTranscoder(dtt),
				client.WithEventBus(bus),
				client.WithEnvRegistry(envRegistry),
				client.WithDataConverter(dc),
				client.WithInterceptors(clientInterceptors),
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create client manager: %w", err)
			}

			// Create worker manager
			workerManager, err := worker.NewManager(
				worker.WithLogger(logger.Named("worker")),
				worker.WithTranscoder(dtt),
				worker.WithEventBus(bus),
				worker.WithResourceRegistry(resourceReg),
				worker.WithEnvRegistry(envRegistry),
				worker.WithInterceptors(workerInterceptors),
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create worker manager: %w", err)
			}

			// Create activity listener to auto-register functions as activities
			activityListener := activity.NewListener(
				logger.Named("activity"),
				workerManager,
			)

			// Create workflow listener to auto-register workflows
			workflowListener := temporalworkflow.NewListener(
				logger.Named("workflow"),
				workerManager,
			)

			// Register handlers for temporal entry kinds
			handlers.RegisterListener(temporalapi.Client, clientManager)
			handlers.RegisterListener(temporalapi.Worker, workerManager)

			// Register activity observer for function entries with temporal.activity metadata
			// Use RegisterObserver because this is a secondary observer that shouldn't ack events
			handlers.RegisterObserver("function.*", activityListener)
			handlers.RegisterObserver("process.*", activityListener)

			// Register workflow observer for workflow entries with temporal.workflow metadata
			handlers.RegisterObserver("workflow.*", workflowListener)

			logger.Info("temporal component loaded")

			return ctx, nil
		},
	})
}

func All() []boot.Component {
	return []boot.Component{
		InterceptorComponent(),
		Component(),
	}
}
