// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/service/supervisor"
)

// ProcessSupervisor creates a boot component that listens for process.service
// registry entries and registers them with the supervisor system.
func ProcessSupervisor() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessSupervisorName,
		DependsOn: []boot.Name{core.SupervisorName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailable
			}

			pidGen := process.GetPIDGenerator(ctx)
			if pidGen == nil {
				return ctx, ErrPIDGeneratorNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			manager := supervisor.NewManager(
				bus,
				dtt,
				pidGen,
				logger.Named("process.service"),
			)

			handlers.RegisterListener(supervisorapi.ProcessService, manager)

			logger.Named("supervisor").Info("manager registered")
			return ctx, nil
		},
	})
}
