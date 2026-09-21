// SPDX-License-Identifier: MPL-2.0

package system

import (
	"github.com/wippyai/runtime/api/boot"
	systemkv "github.com/wippyai/runtime/system/kv"
)

// watchLimits applies the same cluster.kv_watch policy to both replicated KV
// backends; each node-wide backend has an independent source-wide byte cap.
func watchLimits(cfg boot.Config) systemkv.WatchLimits {
	limits := systemkv.DefaultWatchLimits()
	if cfg == nil {
		return limits
	}
	limits.MaxSubscriptions = cfg.GetInt(ClusterKVWatchMaxSubscriptions, limits.MaxSubscriptions)
	limits.MaxEvents = cfg.GetInt(ClusterKVWatchMaxEvents, limits.MaxEvents)
	limits.MaxBytes = int64(cfg.GetInt(ClusterKVWatchMaxBytes, int(limits.MaxBytes)))
	return limits
}
