package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	logapi "github.com/wippyai/runtime/api/logs"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	bootsystem "github.com/wippyai/runtime/boot/components/system/system"
	"go.uber.org/zap"
)

type debugInterceptor struct {
	logger *zap.Logger
}

func (d *debugInterceptor) Handle(ctx context.Context, task runtimeapi.Task, next func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error)) (*runtimeapi.Result, error) {
	// Get options from task
	if task.Options == nil {
		return next(ctx, task)
	}

	opts, ok := task.Options.(runtimeapi.Bag)
	if !ok || !opts.GetBool("debug", false) {
		return next(ctx, task)
	}

	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return next(ctx, task)
	}

	taskID, _ := fc.Get(runtimeapi.FrameIDKey)
	pid, _ := fc.Get(runtimeapi.FramePIDKey)

	d.logger.Info("debug: task execution",
		zap.Any("task_id", taskID),
		zap.Any("pid", pid))

	return next(ctx, task)
}

func InterceptorDebug() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorDebugName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootsystem.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			registry := apiinterceptor.GetRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			debug := &debugInterceptor{
				logger: logger.Named("interceptor.debug"),
			}

			if err := registry.Register("debug", debug, 10); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
