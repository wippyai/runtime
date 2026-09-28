// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func TestClusterKVWatchPolicy(t *testing.T) {
	defaults := systemkv.DefaultWatchLimits()
	if got := watchLimits(nil); got != defaults {
		t.Fatalf("missing config: got %+v, want %+v", got, defaults)
	}
	cfg := boot.NewConfig(boot.WithSection(ClusterName, map[string]any{
		ClusterKVWatchMaxSubscriptions: 17,
		ClusterKVWatchMaxEvents:        13,
		ClusterKVWatchMaxBytes:         12345,
	}))
	limits := watchLimits(cfg.Sub(ClusterName))
	if limits.MaxSubscriptions != 17 || limits.MaxEvents != 13 || limits.MaxBytes != 12345 {
		t.Fatalf("cluster watch config was ignored: %+v", limits)
	}
	fsm := systemkv.NewRaftFSM()
	if err := fsm.SetWatchLimits(limits); err != nil {
		t.Fatal(err)
	}
	if err := fsm.SetWatchLimits(systemkv.WatchLimits{}); err == nil {
		t.Fatal("zero resource limits accepted")
	}
}
