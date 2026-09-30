// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
	"go.uber.org/zap"
)

// standaloneRegistry composes the existing name registry over serialized local
// KV writes. There are no network peers: Strong's observer cohort is just self.
// It is never used as a fallback for an unavailable enabled cluster.
type standaloneRegistry struct {
	engine   *systemkv.Service
	registry *kvbacked.Service
	cancel   context.CancelFunc
	release  context.CancelFunc
}

func loadStandaloneRegistry(ctx context.Context, logger *zap.Logger) (context.Context, *standaloneRegistry, error) {
	node := relay.GetNode(ctx)
	if node == nil {
		return ctx, nil, ErrRelayNotAvailable
	}
	topo := topology.GetTopology(ctx)
	if topo == nil {
		return ctx, nil, fmt.Errorf("standalone registry: topology not available")
	}
	engine := systemkv.NewService("names", logger)
	if _, err := engine.Start(ctx); err != nil {
		return ctx, nil, fmt.Errorf("standalone registry: start KV: %w", err)
	}
	reg := kvbacked.NewService(engine, node.ID(), nil, logger)
	reg.SetTopology(topo)
	reg.ConfigureStrong(kvbacked.StrongDeps{
		Members: func() ([]pid.NodeID, error) { return []pid.NodeID{node.ID()}, nil },
	})
	s := &standaloneRegistry{engine: engine, registry: reg}
	if owned, ok := node.(relay.OwnedHostRegistrar); ok {
		release, err := owned.RegisterOwnedHost(kvbacked.RegistryHostID, reg)
		if err != nil {
			_ = engine.Stop(ctx)
			return ctx, nil, fmt.Errorf("standalone registry: register relay host: %w", err)
		}
		s.release = release
	} else {
		if err := node.RegisterHost(kvbacked.RegistryHostID, reg); err != nil {
			_ = engine.Stop(ctx)
			return ctx, nil, fmt.Errorf("standalone registry: register relay host: %w", err)
		}
		s.release = func() { node.UnregisterHost(kvbacked.RegistryHostID) }
	}
	if pidReg := topology.GetRegistry(ctx); pidReg != nil {
		if setter, ok := pidReg.(interface{ SetGlobalRegistry(topology.GlobalRegistry) }); ok {
			setter.SetGlobalRegistry(reg)
		}
	}
	ctx = topology.WithGlobalRegistry(ctx, reg)
	ctx = globalapi.WithRegistry(ctx, reg)
	return ctx, s, nil
}

func (s *standaloneRegistry) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	if err := s.registry.StartReconciler(ctx); err != nil {
		cancel()
		return fmt.Errorf("standalone registry: start observer: %w", err)
	}
	s.cancel = cancel
	return nil
}

func (s *standaloneRegistry) Stop(ctx context.Context) error {
	s.release()
	if s.cancel != nil {
		s.cancel()
	}
	return s.engine.Stop(ctx)
}
