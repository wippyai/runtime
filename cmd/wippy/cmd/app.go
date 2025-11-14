package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"

	identityv1connect "github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	modulev1connect "github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/cli"
	"github.com/wippyai/runtime/boot/deps/client"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	transcoder "github.com/wippyai/runtime/system/payload"
	json2 "github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/lua"
	"github.com/wippyai/runtime/system/payload/yaml"
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
