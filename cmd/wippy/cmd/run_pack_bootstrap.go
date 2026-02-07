package cmd

import (
	"context"

	logapi "github.com/wippyai/runtime/api/logs"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	bootpkg "github.com/wippyai/runtime/boot"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"go.uber.org/zap"
)

// bootstrapPackRuntime initializes the runtime context used by pack execution paths.
// It keeps boot sequencing identical for both single-pack and multi-pack code paths.
func bootstrapPackRuntime(baseLogger *zap.Logger) (context.Context, *bootpkg.Loader, *zap.Logger, *embedpkg.Registry, error) {
	cfg, err := loadBootConfig()
	if err != nil {
		baseLogger.Error("failed to load config", zap.Error(err))
		return nil, nil, nil, nil, err
	}

	if cfg == nil {
		cfg = createDefaultConfig()
	}

	ctx, err := bootpkg.NewBootstrapContext(baseLogger, cfg)
	if err != nil {
		baseLogger.Error("failed to initialize bootstrap context", zap.Error(err))
		return nil, nil, nil, nil, NewInitializeBootstrapContextError(err)
	}

	logger := logapi.GetLogger(ctx).Named("run-pack")
	logger.Info("infrastructure initialized")

	embedReg := embedpkg.NewRegistry()
	ctx = embedapi.WithRegistry(ctx, embedReg)

	components := StandardComponents()
	ctx, extensionComponents, err := loadExtensionComponents(ctx, logger, components)
	if err != nil {
		logger.Error("failed to load extensions", zap.Error(err))
		embedReg.Close()
		return nil, nil, nil, nil, err
	}

	components = append(components, extensionComponents...)
	logger.Info("registered components", zap.Int("count", len(components)))

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		embedReg.Close()
		return nil, nil, nil, nil, NewCreateLoaderError(err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		embedReg.Close()
		return nil, nil, nil, nil, NewLoadComponentsError(err)
	}

	logger.Info("components loaded successfully")
	return ctx, loader, logger, embedReg, nil
}
