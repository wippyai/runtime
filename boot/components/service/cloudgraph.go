// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/topology"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	bootservicetemporal "github.com/wippyai/runtime/boot/components/service/temporal"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/cloudgraph"
	"go.uber.org/zap"
)

// CloudGraph creates the boot component wiring the cloud-graph deployment
// control plane (docs/design/cloud-graph.md §4.3).
func CloudGraph() boot.Component {
	return boot.New(boot.P{
		Name: CloudGraphName,
		DependsOn: []boot.Name{
			core.RegistryName,
			system.FunctionsName,
			system.ResourcesName,
			system.TopologyName,
			system.ProcessManagerName,
			core.PIDGenName,
			bootservicetemporal.Name,
		},
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

			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailable
			}

			topo := topology.GetTopology(ctx)
			if topo == nil {
				return ctx, ErrTopologyNotAvailable
			}

			procs := process.GetManager(ctx)
			if procs == nil {
				return ctx, ErrProcessManagerNotAvailable
			}

			registrar := temporalapi.GetWorkerRegistrar(ctx)
			if registrar == nil {
				return ctx, ErrWorkerRegistrarNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			cgLogger := logger.Named("cloudgraph")

			patterns := []regapi.DependencyPattern{
				{Path: "data.worker", Description: "Reference to Temporal worker for the cloudgraph engine"},
				{Path: "data.client", Description: "Reference to Temporal client for the cloudgraph engine"},
				{Path: "data.database", Description: "Reference to the cloudgraph ledger database"},
				{Path: "data.host", Description: "Reference to the process host for provider actors"},
				{Path: "data.actor_process", Description: "Reference to a cloudgraph provider actor process"},
				{Path: "data.executor", Description: "Reference to the exec entry used by a cloudgraph provider"},
			}
			for _, pattern := range patterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					cgLogger.Warn("failed to register cloudgraph dependency pattern",
						zap.String("path", pattern.Path),
						zap.Error(err))
				}
			}

			manager := cloudgraph.NewManager(cgLogger, bus, dtt, pidGen, node, topo, procs, registrar)
			handlers.RegisterListener("cloudgraph.(engine|provider)", manager)

			cgLogger.Info("cloudgraph component loaded")
			return ctx, nil
		},
	})
}
