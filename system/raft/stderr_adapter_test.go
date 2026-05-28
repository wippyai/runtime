// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// observedAdapter wires an in-memory zapcore.Observer into the adapter so
// tests can count emitted lines without touching real stderr.
func observedAdapter() (*raftStderrAdapter, *observer.ObservedLogs) {
	core, logs := observer.New(zap.WarnLevel)
	return newRaftStderrAdapter(zap.New(core)), logs
}

func TestRaftStderrAdapter_RateLimitsRepeats(t *testing.T) {
	// Regression test for the chaos-time spam from hashicorp/raft's
	// internal logger — the adapter must collapse identical lines (after
	// trimming the IP:port suffix) to a small bounded number.
	a, logs := observedAdapter()

	for i := 0; i < 1000; i++ {
		// Vary only the trailing IP:port — same first 64 bytes, so the
		// limiter key collapses them all into one bucket.
		line := fmt.Sprintf("raft-net: failed to flush response: error=\"write tcp 10.42.0.38:7960->10.42.2.29:%d: write: broken pipe\"\n", 50000+i)
		_, err := a.Write([]byte(line))
		require.NoError(t, err)
	}

	// Burst is 5 + at most 1 extra from the limiter's tick. We assert
	// "much smaller than the 1000 inputs" rather than an exact count to
	// avoid flakiness from token-bucket scheduling.
	emitted := logs.Len()
	require.Less(t, emitted, 20, "expected aggressive rate limiting; got %d emissions for 1000 inputs", emitted)
}

func TestRaftStderrAdapter_DistinctMessagesEachLogged(t *testing.T) {
	a, logs := observedAdapter()
	for i := 0; i < 5; i++ {
		line := fmt.Sprintf("distinct-message-%d this should not collapse with the others\n", i)
		_, err := a.Write([]byte(line))
		require.NoError(t, err)
	}
	require.Equal(t, 5, logs.Len(), "5 distinct first-64-byte prefixes should each emit once")
}

func TestRaftStderrAdapter_LRUCapsKeyTable(t *testing.T) {
	// Regression test: under chaos generating many distinct first-64-byte
	// prefixes, the adapter must NOT grow its limiter table without bound.
	core, _ := observer.New(zap.WarnLevel)
	a := newRaftStderrAdapterWithCap(zap.New(core), 64)

	// Each line has a unique 64-byte prefix; cap is 64, so the table
	// should plateau at 64 with N-64 evictions.
	const n = 1000
	for i := 0; i < n; i++ {
		line := fmt.Sprintf("prefix-key-%07d-padding-pad-pad-pad-pad-pad-pad-pad-pad-pad-pad-pad-padXYZ\n", i)
		_, err := a.Write([]byte(line))
		require.NoError(t, err)
	}

	a.mu.Lock()
	keys := len(a.limiters)
	lruLen := a.lru.Len()
	evicts := a.evictCount
	a.mu.Unlock()

	require.Equal(t, 64, keys, "limiter map must be capped at maxKeys")
	require.Equal(t, 64, lruLen, "lru list must match map size")
	require.Equal(t, uint64(n-64), evicts, "every key past the cap must trigger one eviction")
}

func TestRaftStderrAdapter_LRUEvictsOldestNotRandom(t *testing.T) {
	// Specifically guards the regression we replaced: the old
	// "for k := range m { delete }" eviction was non-deterministic.
	// Now LRU must evict the least-recently-used key.
	core, _ := observer.New(zap.WarnLevel)
	a := newRaftStderrAdapterWithCap(zap.New(core), 3)

	// Insert A, B, C — table now full.
	_, _ = a.Write([]byte("AAAAAA-this-is-prefix-A-padding-padding-padding-padding-padding-padding\n"))
	_, _ = a.Write([]byte("BBBBBB-this-is-prefix-B-padding-padding-padding-padding-padding-padding\n"))
	_, _ = a.Write([]byte("CCCCCC-this-is-prefix-C-padding-padding-padding-padding-padding-padding\n"))

	// Touch A so it becomes most recent.
	_, _ = a.Write([]byte("AAAAAA-this-is-prefix-A-padding-padding-padding-padding-padding-padding\n"))

	// Insert D — must evict B (LRU at this point), not A or C.
	_, _ = a.Write([]byte("DDDDDD-this-is-prefix-D-padding-padding-padding-padding-padding-padding\n"))

	a.mu.Lock()
	defer a.mu.Unlock()
	require.Contains(t, a.limiters, "AAAAAA-this-is-prefix-A-padding-padding-padding-padding-padding-")
	require.Contains(t, a.limiters, "CCCCCC-this-is-prefix-C-padding-padding-padding-padding-padding-")
	require.Contains(t, a.limiters, "DDDDDD-this-is-prefix-D-padding-padding-padding-padding-padding-")
	require.NotContains(t, a.limiters, "BBBBBB-this-is-prefix-B-padding-padding-padding-padding-padding-")
}

func TestRaftStderrAdapter_PartialLineBuffering(t *testing.T) {
	// hashicorp/raft uses the standard library's log package, which writes
	// header + message + newline as separate Writer calls. The adapter must
	// not treat each fragment as its own line.
	a, logs := observedAdapter()
	chunks := []string{"raft-net: ", "this is one ", "logical line\n"}
	for _, c := range chunks {
		_, err := a.Write([]byte(c))
		require.NoError(t, err)
	}
	require.Equal(t, 1, logs.Len(), "fragments before \\n should buffer into one emission")
	require.Equal(t, "raft-net: this is one logical line", logs.All()[0].Message)
}
