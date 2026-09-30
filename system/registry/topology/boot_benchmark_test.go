// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

type bootHistory struct {
	*MockHistory
	changes registry.ChangeSet
}

func (h *bootHistory) ReplayChanges(ctx context.Context, _ registry.Version, apply func(registry.ChangeSet) error) error {
	for start := 0; start < len(h.changes); start += 20 {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+20, len(h.changes))
		if err := apply(h.changes[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// BenchmarkBootReplay700 models Bee's registry distributed over 35 changesets.
func BenchmarkBootReplay700(b *testing.B) {
	changes := make(registry.ChangeSet, 700)
	for i := range changes {
		changes[i] = registry.Operation{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("bee", fmt.Sprintf("entry%04d", i)), Kind: "lua.library"}}
	}
	h := &bootHistory{MockHistory: NewMockHistory(), changes: changes}
	builder := NewStateBuilder(zap.NewNop(), nil)
	target := version.New(registry.RootVersion)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state, err := builder.BuildState(h, target)
		if err != nil || len(state) != 700 {
			b.Fatalf("state: %d, %v", len(state), err)
		}
	}
}
