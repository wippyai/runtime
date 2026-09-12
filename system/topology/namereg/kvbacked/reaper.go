// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
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
	if pkg == nil {
		return fmt.Errorf("nil registry package")
	}
	leave, err := s.admitMutation(context.Background())
	if err != nil {
		return err
	}
	defer leave()
	return s.handleExitPackage(pkg)
}

// handleExitPackage owns an already-admitted package until cleanup finishes.
func (s *Service) exitPackageOwner(pkg *relay.Package) (pid.PID, bool) {
	var selected pid.PID
	found := false
	for _, msg := range pkg.Messages {
		if msg.Topic != topology.TopicEvents {
			continue
		}
		for _, p := range msg.Payloads {
			if owner, ok := registryExitOwner(p.Data()); ok {
				// A payload cannot grant authority over another PID. Native
				// topology preserves the target as Source after validating its
				// monitor reference; raw mesh delivery must also agree with the
				// connection-derived peer. Native providers have no wire ingress.
				if !owner.Equal(pkg.Source) || (pkg.ReceivedFrom != "" && owner.Node != pkg.ReceivedFrom) {
					s.logger.Debug("registry ignored exit with mismatched origin", zap.String("pid", owner.String()), zap.String("source", pkg.Source.String()), zap.String("peer", pkg.ReceivedFrom))
					continue
				}
				selected, found = owner, true
			}
		}
	}
	return selected, found
}

func (s *Service) handleExitPackage(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	selected, found := s.exitPackageOwner(pkg)
	if !found {
		return nil
	}
	// Origin validation means every admissible event in this package has the
	// same owner. Deduplicate it without retaining every payload or result.
	key := selected.String()
	if err := s.reapOwners(s.reconcileContext(), []pid.PID{selected})[key]; err != nil {
		s.logger.Warn("registry exit cleanup incomplete", zap.String("pid", key), zap.Error(err))
	} else {
		s.monitored.Delete(key)
	}
	return nil
}

// DropNode handles a departed node: it removes the node's owned names and drops
// it from any in-flight Strong reservation's required set so promotion can still
// complete on the survivors.
func (s *Service) DropNode(node pid.NodeID) {
	// Discovery loss cannot retire an enrolled incarnation or its claims.
	if s.strong != nil && s.strong.incarnation != "" {
		return
	}
	leave, err := s.admitMutation(context.Background())
	if err != nil {
		return
	}
	defer leave()
	if err := s.reapBindings(nodeIndexBase(node), true); err != nil {
		s.logger.Warn("registry node cleanup incomplete", zap.String("node", node), zap.Error(err))
	}
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
	if derr != nil || hdr.RequiredIncarnations != nil {
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

// Native MsgPack preserves PID extensions but decodes event structs as maps.
// Only a process exit is cleanup evidence; link loss and cancellation are not.
func registryExitOwner(data any) (pid.PID, bool) {
	switch ev := data.(type) {
	case *topology.ExitEvent:
		if ev != nil && ev.Kind == topology.Exit {
			return ev.From, true
		}
	case map[string]any:
		kind, ok := ev["kind"].(string)
		if !ok || kind != topology.Exit {
			break
		}
		owner, ok := ev["from"].(pid.PID)
		if ok {
			return owner, true
		}
	}
	return pid.PID{}, false
}
