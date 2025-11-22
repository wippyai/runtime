package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"go.temporal.io/sdk/converter"
)

func DataConverter() boot.Component {
	return boot.New(boot.P{
		Name:      DataConverterName,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			transcoder := payload.GetTranscoder(ctx)
			if transcoder == nil {
				return ctx, fmt.Errorf("transcoder not available in context")
			}

			customDC := dataconverter.NewDataConverter(
				transcoder,
				converter.GetDefaultDataConverter(),
			)

			registry := dataconverter.NewRegistry(customDC)
			ctx = temporalapi.WithDataConverterRegistry(ctx, registry)

			logger.Info("temporal data converter registry initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal data converter registry started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal data converter registry stopped")
			return nil
		},
	})
}
