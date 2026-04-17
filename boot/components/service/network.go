// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
	envapi "github.com/wippyai/runtime/api/env"
	logapi "github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	netservice "github.com/wippyai/runtime/service/net"
	"github.com/wippyai/runtime/service/net/i2p"
	"github.com/wippyai/runtime/service/net/socks5"
	"github.com/wippyai/runtime/service/net/tailscale"
	netsystem "github.com/wippyai/runtime/system/net"
)

// Network creates the network overlay service boot component.
// It creates the driver manager that handles SOCKS5, I2P, and Tailscale
// overlay entries from the registry, delegating storage to the
// system-level network Registry.
func Network() boot.Component {
	return boot.New(boot.P{
		Name:      NetworkServiceName,
		DependsOn: []boot.Name{bootsystem.NetworkName, bootsystem.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			envRegistry := envapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			netReg := netapi.GetNetworkRegistry(ctx)
			if netReg == nil {
				return ctx, fmt.Errorf("network service: system registry not found")
			}
			reg, ok := netReg.(*netsystem.Registry)
			if !ok {
				return ctx, fmt.Errorf("network service: unexpected registry type %T", netReg)
			}

			stateDir := DefaultNetworkStateDir
			defaultNetwork := ""
			if cfg := boot.GetConfig(ctx); cfg != nil {
				if netCfg := cfg.Sub(NetworkServiceName); netCfg != nil {
					stateDir = netCfg.GetString(NetworkStateDir, stateDir)
					defaultNetwork = netCfg.GetString(NetworkDefault, "")
				}
				if stateDir != "" && !filepath.IsAbs(stateDir) {
					if baseDir := cfg.GetString("boot.config_dir", ""); baseDir != "" {
						stateDir = filepath.Join(baseDir, stateDir)
					}
				}
			}

			if defaultNetwork != "" {
				ctx = netapi.WithAppDefaultNetwork(ctx, defaultNetwork)
			}

			manager, err := netservice.NewManager(reg, dtt, envRegistry, logger.Named("network"),
				netservice.WithStateDir(stateDir),
				netservice.WithDriver(
					socks5.NewDriver(),
					i2p.NewDriver(),
					tailscale.NewDriver(),
				),
			)
			if err != nil {
				return ctx, err
			}

			handlers.RegisterListener("network.*", manager)
			return ctx, nil
		},
	})
}
