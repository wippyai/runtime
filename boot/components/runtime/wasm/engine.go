// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/internal/cachedir"
	"github.com/wippyai/runtime/internal/toolchain"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmfunc "github.com/wippyai/runtime/runtime/wasm/component/function"
	wasmproc "github.com/wippyai/runtime/runtime/wasm/component/process"
	"github.com/wippyai/runtime/system/scheduler/affinity"
	"github.com/wippyai/wasm-runtime/asyncify"
	"go.uber.org/zap"
)

const wasmRuntimeModulePath = "github.com/wippyai/wasm-runtime"

type identityResolver func(string) (string, error)

// Engine wires WASM function registry handling and runtime lifecycle.
func Engine() boot.Component {
	return EngineWithHostProfiles()
}

// EngineWithHostProfiles wires WASM function runtime using provided host profiles.
// This is the extension point for boot-time host plugins.
func EngineWithHostProfiles(hostProfiles ...wasmcomponent.HostProfile) boot.Component {
	return newEngine(toolchain.ModuleIdentity, hostProfiles...)
}

func newEngine(resolveIdentity identityResolver, hostProfiles ...wasmcomponent.HostProfile) boot.Component {
	profiles := append([]wasmcomponent.HostProfile(nil), hostProfiles...)
	var funcs *wasmfunc.Manager
	var procs *wasmproc.Manager

	return boot.New(boot.P{
		Name:      EngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, dispatchers.SocketDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			transportRegistry, err := newTransportRegistry()
			if err != nil {
				return ctx, err
			}
			ctx = wasmapi.SetTransportRegistry(ctx, transportRegistry)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, dispatchers.ErrDispatcherNotFound
			}

			cfg := boot.GetConfig(ctx)
			caches := resolveCaches(cfg, logger.Named("wasm"), resolveIdentity)

			fsReg := fsapi.GetRegistry(ctx)
			funcs = wasmfunc.NewManager(
				logger.Named("wasm.func"),
				bus,
				disp,
				fsReg,
				caches,
			)
			if part, ok := affinity.PartitionFromContext(ctx); ok && part.Enabled {
				funcs.SetWASMAffinity(part.WASMCPUs)
			}
			procs = wasmproc.NewManager(
				logger.Named("wasm.process"),
				bus,
				fsReg,
				caches,
			)
			effectiveProfiles := profiles
			if len(effectiveProfiles) == 0 {
				effectiveProfiles = DefaultHostProfiles(logger.Named("wasm.host"), disp)
			}
			if err := funcs.RegisterHostProfiles(effectiveProfiles...); err != nil {
				return ctx, err
			}
			if err := procs.RegisterHostProfiles(effectiveProfiles...); err != nil {
				return ctx, err
			}

			handlers.Register(wasmcomponent.NewHandler("function.(wasm|wat)", funcs))
			handlers.Register(wasmcomponent.NewHandler("process.wasm", procs))

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if funcs != nil {
				if err := funcs.Start(ctx); err != nil {
					return err
				}
			}
			if procs != nil {
				if err := procs.Start(ctx); err != nil {
					return err
				}
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs != nil {
				funcs.Stop()
			}
			if procs != nil {
				procs.Stop()
			}
			return nil
		},
	})
}

func resolveCaches(cfg boot.Config, logger *zap.Logger, resolveIdentity identityResolver) wasmcomponent.Caches {
	enabled := true
	dir := filepath.Join(cachedir.Dir(), "wasm")
	if cfg != nil {
		wasmCfg := cfg.Sub("wasm")
		if _, ok := wasmCfg.Get("cache.enabled"); ok {
			enabled = wasmCfg.GetBool("cache.enabled", enabled)
		}
		dir = wasmCfg.GetString("cache.dir", dir)
		if dir != "" && !filepath.IsAbs(dir) {
			if baseDir := cfg.GetString("boot.config_dir", ""); baseDir != "" {
				dir = filepath.Join(baseDir, dir)
			}
		}
	}
	if !enabled {
		return wasmcomponent.InMemoryCaches()
	}
	if err := cachedir.Probe(dir); err != nil {
		logger.Warn("wasm compilation cache directory unusable; falling back to in-memory cache",
			zap.String("dir", dir),
			zap.Error(err),
		)
		return wasmcomponent.InMemoryCaches()
	}

	compilationCache := wasmcomponent.DirCompilationCache(dir)

	identity, err := resolveIdentity(wasmRuntimeModulePath)
	if err != nil {
		logger.Warn("wasm transform cache module identity unresolvable; falling back to in-memory cache",
			zap.Error(err),
		)
		return wasmcomponent.Caches{
			Compilation: compilationCache,
			Transform:   asyncify.NewMemoryTransformCache(),
		}
	}

	transformDir := filepath.Join(dir, "asyncify", identity)
	transformCache, err := asyncify.NewDirTransformCache(transformDir)
	if err != nil {
		logger.Warn("wasm transform cache directory unusable; falling back to in-memory cache",
			zap.String("dir", transformDir),
			zap.Error(err),
		)
		return wasmcomponent.Caches{
			Compilation: compilationCache,
			Transform:   asyncify.NewMemoryTransformCache(),
		}
	}

	return wasmcomponent.Caches{
		Compilation: compilationCache,
		Transform:   transformCache,
	}
}
