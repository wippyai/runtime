// Package app provides application initialization for wippy CLI commands.
package app

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/deps/client"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	"github.com/wippyai/runtime/cmd/internal/logger"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	json2 "github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/yaml"
	"go.uber.org/zap"
)

// registryClientKey is used as a context key for storing the registry client
type registryClientKey struct{}

// Context holds initialized application infrastructure
type Context struct {
	Ctx            context.Context
	Logger         *zap.Logger
	Transcoder     payload.Transcoder
	Loader         *loader.Loader
	RegistryClient *client.RegistryClient
}

// Init initializes the CLI application infrastructure
func Init(ctx context.Context, verbose, veryVerbose, console, silent bool, appStartTime time.Time) (*Context, error) {
	log, err := logger.CreateLogger(logger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silent,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return nil, NewCreateLoggerError(err)
	}

	// Create AppContext for storing global singletons
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

	// Initialize transcoder
	dtt := transcoder.GlobalTranscoder()
	json2.Register(dtt)
	yaml.Register(dtt)
	luapayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Initialize loader
	interpolator := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	ldr := loader.NewLoader(dtt, log.Named("loader"), interpolator)

	// Initialize registry client
	registryClient := client.NewRegistryClientFromConfig(nil)

	// Store in context for entries.go compatibility
	ctx = WithRegistryClient(ctx, registryClient)
	ctx = boot.WithLoader(ctx, ldr)

	return &Context{
		Ctx:            ctx,
		Logger:         log,
		Transcoder:     dtt,
		Loader:         ldr,
		RegistryClient: registryClient,
	}, nil
}

// WithRegistryClient stores the registry client in the context
func WithRegistryClient(ctx context.Context, client *client.RegistryClient) context.Context {
	return context.WithValue(ctx, registryClientKey{}, client)
}

// GetRegistryClient retrieves the registry client from the context
func GetRegistryClient(ctx context.Context) *client.RegistryClient {
	if v := ctx.Value(registryClientKey{}); v != nil {
		return v.(*client.RegistryClient)
	}
	return nil
}
