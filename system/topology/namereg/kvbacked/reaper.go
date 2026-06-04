// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"strings"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// RegistryHostID is the relay host the kv-backed registry registers to receive
// process-exit events for auto-removal of a dead process's names.
const RegistryHostID pid.HostID = "sysreg"

// SetTopology wires the topology used to monitor registered PIDs for exit.
func (s *Service) SetTopology(topo topology.Topology) { s.topo = topo }

// monitor asks topology to deliver an exit event for p so its names are reaped
// when the process dies. Deduped; nil topology disables it (node-leave reaping
// still works via RemoveNode).
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
				s.monitored.Delete(ev.From.String())
				_ = s.Remove(context.Background(), ev.From)
			}
		}
	}
	return nil
}

// DropNode handles a departed node: it removes the node's owned names and drops
// it from any in-flight Strong reservation's required set so promotion can still
// complete on the survivors.
func (s *Service) DropNode(node pid.NodeID) {
	_ = s.RemoveNode(context.Background(), node)
	if s.strong != nil {
		s.strong.dropNode(node)
	}
}

// dropNode removes a departed node from every in-flight pending reservation's
// RequiredNodes (leader only), then reconciles so a now-complete set promotes.
func (st *strongState) dropNode(node pid.NodeID) {
	if !st.isLeader() {
		return
	}
	var names []string
	_ = st.svc.engine.Scan(pendingPrefix, func(e kvapi.Entry) bool {
		names = append(names, strings.TrimPrefix(e.Key, pendingPrefix))
		return true
	})
	for _, name := range names {
		st.dropNodeFromPending(name, node)
	}
}

func (st *strongState) dropNodeFromPending(name string, node pid.NodeID) {
	pe, err := st.svc.get(pendingKey(name))
	if err != nil {
		return
	}
	hdr, derr := decodePending(pe.Value)
	if derr != nil {
		return
	}
	idx := -1
	for i, n := range hdr.RequiredNodes {
		if n == node {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	hdr.RequiredNodes = append(hdr.RequiredNodes[:idx:idx], hdr.RequiredNodes[idx+1:]...)
	val, eerr := encode(hdr)
	if eerr != nil {
		return
	}
	// Atomically drop the node from RequiredNodes and delete its now-orphaned
	// ack/reject keys (they would otherwise never be cleaned, since promote/expire
	// only sweep keys for the remaining RequiredNodes).
	committed, terr := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: pe.Version},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: pendingKey(name), Value: val},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ackKey(name, pe.Epoch, node)},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: rejectKey(name, pe.Epoch, node)},
	})
	if terr != nil || !committed {
		return
	}
	st.reconcile(name)
}

var _ relay.Receiver = (*Service)(nil)
