// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// BenchmarkBootTransition1000 runs the actual transaction runner with 1000
// creates and no component listeners; history IO and Lua compilation are excluded.
func BenchmarkBootTransition1000(b *testing.B) {
	bus := eventbus.NewBus()
	defer bus.Stop()
	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	runner := NewBusRunner(bus, zap.NewNop(), builder, WithDispatchPolicy(internalDispatchPolicy()))
	changes := make(registry.ChangeSet, 1000)
	for i := range changes {
		changes[i] = registry.Operation{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("boot", fmt.Sprint(i)), Kind: registry.EntryKind}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state, err := runner.Transition(b.Context(), nil, changes, nil)
		if err != nil || len(state) != len(changes) {
			b.Fatalf("transition: %d entries, %v", len(state), err)
		}
	}
}
