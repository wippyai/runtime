// SPDX-License-Identifier: MPL-2.0

package host

import (
	"sort"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

// ListHosts returns summary stats for all running hosts.
func (m *Manager) ListHosts() []process.HostStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]process.HostStats, 0, len(m.hosts))
	for id, h := range m.hosts {
		if h.scheduler == nil {
			continue
		}
		stats := h.scheduler.Stats()
		result = append(result, process.HostStats{
			ID:         id,
			Workers:    stats["workers"],
			Processes:  stats["processes"],
			Executed:   stats["executed"],
			Stolen:     stats["stolen"],
			QueueDepth: stats["global_queue"],
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID.String() < result[j].ID.String()
	})

	return result
}

// HostProcesses returns process info for a specific host.
// Enables stats collection on the host's scheduler for future snapshots.
func (m *Manager) HostProcesses(hostID registry.ID) []process.Stats {
	m.mu.RLock()
	h, ok := m.hosts[hostID]
	m.mu.RUnlock()

	if !ok || h.scheduler == nil {
		return nil
	}

	h.scheduler.EnableStats()

	procs := h.scheduler.ListProcesses()
	result := make([]process.Stats, len(procs))
	for i, p := range procs {
		result[i] = process.Stats{
			PID:       p.PID,
			Parent:    p.Parent,
			Host:      hostID,
			Source:    p.Source,
			State:     p.State,
			Steps:     p.Steps,
			StartedAt: p.StartedAt,
			Stats:     p.Stats,
			ActorID:   p.ActorID,
		}
	}

	return result
}

// NotifyOutdated fans the affected set out to every host scheduler so running
// instances whose source is in the set receive an OUTDATED event. Engine-
// agnostic: the scheduler sends a generic package and each engine decides how to
// react.
func (m *Manager) NotifyOutdated(affected map[registry.ID]bool) {
	if len(affected) == 0 {
		return
	}
	m.mu.RLock()
	schedulers := make([]*actor.Scheduler, 0, len(m.hosts))
	for _, h := range m.hosts {
		if h.scheduler != nil {
			schedulers = append(schedulers, h.scheduler)
		}
	}
	m.mu.RUnlock()

	for _, s := range schedulers {
		s.SendOutdated(affected)
	}
}

var (
	_ process.Inspector        = (*Manager)(nil)
	_ process.OutdatedNotifier = (*Manager)(nil)
)
