// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"github.com/wippyai/runtime/api/topology"
)

// Inbound relay dispatch and cluster membership event handling. Send routes
// each relay topic to its handler; the handlers apply remote discover/sync/
// join/leave/exit packages and node up/down events to local state.

// Send implements relay.Receiver for the pg host.
// Incoming relay packages are dispatched to protocol handlers.
func (s *Service) Send(pkg *relay.Package) error {
	if pkg == nil || len(pkg.Messages) == 0 {
		return nil
	}

	// Any inbound PG protocol package counts as activity for the
	// liveness signal — receiving join/leave/sync from a peer is
	// progress, even if no local broadcast was emitted.
	s.activity.Touch()

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case pgapi.TopicDiscover:
			s.handleDiscoverPackage(msg)
		case pgapi.TopicSync:
			s.handleSyncPackage(msg)
		case pgapi.TopicJoin:
			s.handleJoinPackage(msg)
		case pgapi.TopicLeave:
			s.handleLeavePackage(msg)
		case topology.TopicEvents:
			s.handleExitPackage(msg)
		}
	}

	relay.ReleasePackage(pkg)
	return nil
}

// handleDiscoverPackage processes an incoming discover message.
func (s *Service) handleDiscoverPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	// pg_queue_dropped_total{reason="full"} is recorded inside submit()
	// when the queue rejects the operation; no per-message log needed.
	s.submit(func() {
		s.handleDiscover(fromNodeID)
		s.publishDirty()
	})
}

// handleSyncPackage processes an incoming sync message.
func (s *Service) handleSyncPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}
	rawGroups, _ := data["groups"].(map[string]any)

	groups := make(map[string][]pid.PID, len(rawGroups))
	for group, raw := range rawGroups {
		pidStrs, ok := raw.([]any)
		if !ok {
			continue
		}
		pids := make([]pid.PID, 0, len(pidStrs))
		for _, ps := range pidStrs {
			if s, ok := ps.(string); ok {
				if p, err := pid.ParsePID(s); err == nil {
					pids = append(pids, p)
				}
			}
		}
		if len(pids) > 0 {
			groups[group] = pids
		}
	}

	s.submit(func() {
		s.handleSync(fromNodeID, groups)
		s.publishDirty()
	})
}

// decodeGroupPidsMap parses a `map[string][]string` payload field (the
// receiver-side decoded shape after relay serialization) into the
// internal map[string][]pid.PID form, skipping unparseable entries.
func decodeGroupPidsMap(raw any) map[string][]pid.PID {
	rawMap, ok := raw.(map[string]any)
	if !ok || len(rawMap) == 0 {
		return nil
	}
	result := make(map[string][]pid.PID, len(rawMap))
	for g, rawPids := range rawMap {
		pids := decodePidList(rawPids)
		if len(pids) > 0 {
			result[g] = pids
		}
	}
	return result
}

// decodePidList parses a `[]any` of pid strings into []pid.PID.
func decodePidList(raw any) []pid.PID {
	rawSlice, ok := raw.([]any)
	if !ok || len(rawSlice) == 0 {
		return nil
	}
	pids := make([]pid.PID, 0, len(rawSlice))
	for _, ps := range rawSlice {
		s, ok := ps.(string)
		if !ok {
			continue
		}
		if p, err := pid.ParsePID(s); err == nil {
			pids = append(pids, p)
		}
	}
	return pids
}

// handleJoinPackage processes an incoming join message. The payload carries
// a `joins` map of {group -> pid strings}; one packet may cover multiple
// groups (batched broadcast).
func (s *Service) handleJoinPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	joins := decodeGroupPidsMap(data["joins"])
	if len(joins) == 0 {
		// Fallback to the pre-batch single-group format
		// ({"group": "...", "pids": [...]}) so older senders still work
		// through a rolling upgrade.
		if group, _ := data["group"].(string); group != "" {
			if pids := decodePidList(data["pids"]); len(pids) > 0 {
				joins = map[string][]pid.PID{group: pids}
			}
		}
	}
	if len(joins) == 0 {
		return
	}

	s.submit(func() {
		for group, pids := range joins {
			s.handleRemoteJoin(fromNodeID, group, pids)
		}
		s.publishDirty()
	})
}

// handleLeavePackage processes an incoming leave message. The payload
// carries a `leaves` map of {group -> pid strings}; PIDs repeated in a
// group's value list cause the matching number of multi-join slots to be
// removed.
func (s *Service) handleLeavePackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	leaves := decodeGroupPidsMap(data["leaves"])
	if len(leaves) == 0 {
		// Fallback to the pre-batch flat ({pids, groups}) shape where the
		// receiver must remove each pid from each group once. Translate
		// it into the batched map by repeating each pid per group entry.
		pids := decodePidList(data["pids"])
		rawGroups, _ := data["groups"].([]any)
		if len(pids) > 0 && len(rawGroups) > 0 {
			leaves = make(map[string][]pid.PID, len(rawGroups))
			for _, raw := range rawGroups {
				g, ok := raw.(string)
				if !ok || g == "" {
					continue
				}
				leaves[g] = append(leaves[g], pids...)
			}
		}
	}
	if len(leaves) == 0 {
		return
	}

	s.submit(func() {
		for group, pids := range leaves {
			s.handleRemoteLeave(fromNodeID, pids, []string{group})
		}
		s.publishDirty()
	})
}

// handleExitPackage processes an incoming process exit event.
func (s *Service) handleExitPackage(msg *relay.Message) {
	for _, p := range msg.Payloads {
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			exitedPID := exitEvent.From
			s.submit(func() {
				s.handleProcessExit(exitedPID)
				s.publishDirty()
			})
		}
	}
}

// handleNodeJoinedEvent is called by the event bus subscriber when a node joins.
func (s *Service) handleNodeJoinedEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.sendDiscover(nodeID)
	})
}

// handleNodeLeftEvent is called by the event bus subscriber when a node leaves.
func (s *Service) handleNodeLeftEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.handleNodeLeft(nodeID)
		s.publishDirty()
	})
}
