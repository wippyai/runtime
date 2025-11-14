package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/boot/cli"
	"github.com/ponyruntime/pony/boot/loader"
	"github.com/ponyruntime/pony/boot/loader/interpolate"
	"github.com/ponyruntime/pony/deps/client"
	transcoder "github.com/ponyruntime/pony/system/payload"
	json2 "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	identityv1connect "github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	modulev1connect "github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.uber.org/zap"
)

// AppContext holds initialized application infrastructure
type AppContext struct {
	Ctx            context.Context
	Logger         *zap.Logger
	Transcoder     payload.Transcoder
	Loader         *loader.Loader
	RegistryClient *client.RegistryClient
}

// InitApp initializes the CLI application infrastructure
func InitApp(ctx context.Context) (*AppContext, error) {
	logger, err := CreateLogger()
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	// Create AppContext for storing global singletons
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

	// Initialize transcoder
	dtt := transcoder.GlobalTranscoder()
	json2.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Initialize loader
	interpolator := interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	ldr := loader.NewLoader(dtt, logger.Named("loader"), interpolator)

	// Initialize registry client
	baseURL := os.Getenv("WIPPY_MODULES_URL")
	if baseURL == "" {
		baseURL = cli.DefaultRegistryURL
	}

	httpClient := &http.Client{}
	registryClient := client.NewRegistryClient(
		identityv1connect.NewOrganizationServiceClient(httpClient, baseURL),
		modulev1connect.NewModuleServiceClient(httpClient, baseURL),
		modulev1connect.NewLabelServiceClient(httpClient, baseURL),
		modulev1connect.NewDownloadServiceClient(httpClient, baseURL),
	)

	// Store in context for entries.go compatibility
	ctx = cli.WithRegistryClient(ctx, registryClient)
	ctx = cli.WithLoader(ctx, ldr)

	return &AppContext{
		Ctx:            ctx,
		Logger:         logger,
		Transcoder:     dtt,
		Loader:         ldr,
		RegistryClient: registryClient,
	}, nil
}
