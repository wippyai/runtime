// SPDX-License-Identifier: MPL-2.0

package host

import (
	"reflect"
	"sort"

	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/api/supervisor"
)

// updateConfig applies the supported live subset atomically with Host
// Start/Stop. The returned bool reports whether scheduler state changed.
func (h *Host) updateConfig(desired *hostapi.EntryConfig, affinityManaged bool) (bool, error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.shutdown.Load() {
		return false, ErrHostShuttingDown
	}
	unsupported := unsupportedHostUpdateFields(h.cfg, desired, affinityManaged)
	if len(unsupported) > 0 {
		return false, NewUnsupportedHostUpdateError(unsupported)
	}
	if h.cfg.HostConfig.Workers == desired.HostConfig.Workers {
		return false, nil
	}
	if err := h.scheduler.ResizeWorkers(desired.HostConfig.Workers); err != nil {
		return false, NewResizeWorkersError(err)
	}

	// Config snapshots are immutable after publication. A failed resize leaves
	// both the scheduler and this pointer at the previous effective config.
	h.cfg = desired
	return true, nil
}

func unsupportedHostUpdateFields(current, desired *hostapi.EntryConfig, affinityManaged bool) []string {
	fields := make([]string, 0, 4)
	if !reflect.DeepEqual(canonicalLifecycle(current.Lifecycle), canonicalLifecycle(desired.Lifecycle)) {
		fields = append(fields, "lifecycle")
	}
	if current.HostConfig.QueueSize != desired.HostConfig.QueueSize {
		fields = append(fields, "host.queue_size")
	}
	if current.HostConfig.LocalQueueSize != desired.HostConfig.LocalQueueSize {
		fields = append(fields, "host.local_queue_size")
	}
	if affinityManaged && current.HostConfig.Workers != desired.HostConfig.Workers {
		fields = append(fields, "host.workers")
	}
	return fields
}

func canonicalLifecycle(cfg supervisor.LifecycleConfig) supervisor.LifecycleConfig {
	cfg.InitDefaults()
	seen := make(map[string]struct{}, len(cfg.Requires)+len(cfg.DependsOn))
	required := make([]string, 0, len(cfg.Requires)+len(cfg.DependsOn))
	for _, dependency := range append(append([]string(nil), cfg.Requires...), cfg.DependsOn...) {
		if _, exists := seen[dependency]; exists {
			continue
		}
		seen[dependency] = struct{}{}
		required = append(required, dependency)
	}
	sort.Strings(required)
	cfg.Requires = required
	cfg.DependsOn = nil
	return cfg
}
