package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	contextapi "github.com/ponyruntime/pony/api/context"
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

			ac := contextapi.AppFromContext(ctx)
			if ac != nil {
				ac.With(loaderKey{}, ldr)
			}

			return ctx, nil
		},
	})
}

// loaderKey is the context key for the loader component.
type loaderKey struct{}

// GetLoader retrieves the loader from context.
func GetLoader(ctx context.Context) *loader.Loader {
	ac := contextapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if ldr, ok := ac.Get(loaderKey{}).(*loader.Loader); ok {
		return ldr
	}
	return nil
}
