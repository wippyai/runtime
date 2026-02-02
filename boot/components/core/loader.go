package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
)

func Loader() boot.Component {
	return boot.New(boot.P{
		Name:      LoaderName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)

			if dtt == nil {
				logger.Fatal("transcoder not found in context")
			}

			interpolator := interpolate.NewEntryInterpolator(dtt,
				interpolate.WithInterpolator(interpolate.LoadFile),
			)

			ldr := loader.NewLoader(dtt, logger.Named("loader"), interpolator)

			return boot.WithLoader(ctx, ldr), nil
		},
	})
}
