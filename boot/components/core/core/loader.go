package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/boot/loader"
	"github.com/ponyruntime/pony/boot/loader/interpolate"
)

func Loader() boot.Component {
	return boot.New(boot.P{
		Name:      LoaderName,
		Phase:     boot.Init,
		DependsOn: []string{LoggerName, TranscoderName},
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

			boot.WithLoader(ctx, ldr)

			return ctx, nil
		},
	})
}
