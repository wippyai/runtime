// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

const EvalHostName boot.Name = "runtime.lua.eval"

// Eval creates the eval host boot component.
func Eval() boot.Component {
	return boot.New(boot.P{
		Name:      EvalHostName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherRegistrarNotFound
			}

			// Get code manager for dynamic module lookup
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, ErrCodeManagerNotFound
			}

			// Create eval host with dynamic module provider
			evalLogger := logger.Named("eval")
			host := evalhost.NewHost(
				evalLogger,
				cm.GetModuleDefs,
			)

			// Set up import loader to load library sources from code manager
			host.WithImportLoader(func(id registry.ID) (string, error) {
				node, err := cm.GetNode(id)
				if err != nil {
					return "", err
				}
				if node.Source == "" {
					return "", fmt.Errorf("import %s has no source (bytecode libraries not supported in eval)", id)
				}
				return node.Source, nil
			})

			// Register dispatcher handlers
			d := evalhost.NewDispatcher(host)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
