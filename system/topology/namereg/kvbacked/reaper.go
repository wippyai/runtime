// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// RegistryHostID is the relay host the kv-backed registry registers to receive
// process-exit events for auto-removal of a dead process's names.
const RegistryHostID pid.HostID = "sysreg"

// SetTopology wires the topology used to monitor registered PIDs for exit.
func (s *Service) SetTopology(topo topology.Topology) { s.topo = topo }

// monitor asks topology to deliver an exit event for p so its names are reaped
// when the process dies. Deduped; nil topology disables process-exit reaping.
func (s *Service) monitor(p pid.PID) {
	if s.topo == nil {
		return
	}
	key := p.String()
	if _, loaded := s.monitored.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	if err := s.topo.Monitor(s.self, p); err != nil {
		s.monitored.Delete(key)
		s.logger.Debug("registry monitor failed", zap.String("pid", key), zap.Error(err))
	}
}

// Send implements relay.Receiver: a registered process's exit removes its names.
func (s *Service) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		if msg.Topic != topology.TopicEvents {
			continue
		}
		for _, p := range msg.Payloads {
			if ev, ok := p.Data().(*topology.ExitEvent); ok {
				switch ev.Kind {
				case topology.Exit:
					s.monitored.Delete(ev.From.String())
					_ = s.Remove(context.Background(), ev.From)
				case topology.LinkDown:
					s.monitored.Delete(ev.From.String())
				}
			}
		}
	}
	return nil
}

var _ relay.Receiver = (*Service)(nil)
