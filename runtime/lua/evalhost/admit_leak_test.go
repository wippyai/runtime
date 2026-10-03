// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/registry"
)

// trackEntry counts the cached entries the collector reclaims.
func trackEntry(a *Admitter, program apihost.EvalProgram, collected *atomic.Int64) {
	entry := a.lookup(program.Key)
	runtime.SetFinalizer(entry, func(*admitEntry) { collected.Add(1) })
}

func awaitCollected(t *testing.T, collected *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for collected.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("collected %d programs, want %d", collected.Load(), want)
		}
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAdmitterCacheReleasesEvictedPrograms(t *testing.T) {
	const capacity = 8
	const total = 400
	a, _ := newTestAdmitter(t, WithProgramCacheSize(capacity))
	var collected atomic.Int64
	for i := 0; i < total; i++ {
		program, err := a.Compile(context.Background(), apihost.EvalCompileSpec{
			SourceCode: fmt.Sprintf(`return { main = function() return %d end }`, i),
		})
		require.NoError(t, err)
		trackEntry(a, program, &collected)
		require.LessOrEqual(t, len(a.entries), capacity)
		require.Equal(t, len(a.entries), a.lru.Len())
		require.LessOrEqual(t, len(a.sources), capacity)
	}
	awaitCollected(t, &collected, total-capacity)
}

func TestAdmitterCacheReleasesSupersededPrograms(t *testing.T) {
	a, _ := newTestAdmitter(t)
	generation := 0
	a.host.WithImportLoader(func(registry.ID) (string, error) {
		return fmt.Sprintf(`return { value = %d }`, generation), nil
	})
	policy := apihost.EvalPolicy{Imports: []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}}}

	var collected atomic.Int64
	const generations = 100
	for generation = 0; generation < generations; generation++ {
		program, err := a.Compile(context.Background(), apihost.EvalCompileSpec{SourceCode: admitSource, Policy: policy})
		require.NoError(t, err)
		trackEntry(a, program, &collected)
		require.Equal(t, 1, len(a.entries))
		require.Equal(t, 1, a.lru.Len())
		require.Equal(t, 1, len(a.sources))
	}
	awaitCollected(t, &collected, generations-1)
}

func TestAdmitterEvictReleasesProgram(t *testing.T) {
	a, _ := newTestAdmitter(t)
	var collected atomic.Int64
	for i := 0; i < 50; i++ {
		program, err := a.Compile(context.Background(), apihost.EvalCompileSpec{
			SourceCode: fmt.Sprintf(`return { main = function() return %d end }`, i),
		})
		require.NoError(t, err)
		trackEntry(a, program, &collected)
		ok, err := a.Evict(context.Background(), program)
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.Empty(t, a.entries)
	require.Zero(t, a.lru.Len())
	require.Empty(t, a.sources)
	awaitCollected(t, &collected, 50)
}
