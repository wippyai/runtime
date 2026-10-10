// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	api "github.com/wippyai/runtime/api/supervisor"
)

func BenchmarkSupervisorLifecycleOwnership(b *testing.B) {
	cfg := api.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second}
	b.ReportAllocs()
	for b.Loop() {
		c := NewController(context.Background(), newFastService(), cfg, nil)
		if err := c.Start(); err != nil {
			b.Fatal(err)
		}
		if err := c.Stop(); err != nil {
			b.Fatal(err)
		}
		c.close()
	}
}

func BenchmarkSupervisorStateNotification(b *testing.B) {
	c := NewController(context.Background(), newFastService(), api.LifecycleConfig{},
		func(api.Status, any) {})
	defer c.close()
	b.ReportAllocs()
	for b.Loop() {
		c.updateState(api.StatusRunning, "healthy")
	}
}
