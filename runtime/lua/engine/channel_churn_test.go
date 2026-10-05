// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestChannelConstructionOwnsEmptyQueues(t *testing.T) {
	for _, capacity := range []int{0, 1, 64} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			var ch *Channel
			allocs := testing.AllocsPerRun(100, func() { ch = NewChannel(capacity) })
			require.LessOrEqual(t, allocs, float64(2), "empty queue headers should share one allocation")
			require.Zero(t, ch.Size())
			require.Equal(t, capacity, ch.Slots())
			require.False(t, ch.CanReceive())
			require.Equal(t, capacity > 0, ch.CanSend())
			require.Zero(t, ch.Drain())
			require.Nil(t, ch.Close(nil))
			require.True(t, ch.CanReceive())
		})
	}
}

func TestChannelQueuesRemainIndependent(t *testing.T) {
	a, b := NewChannel(1), NewChannel(1)
	_, sent := a.TrySend(lua.LString("a"))
	require.True(t, sent)
	require.Equal(t, 1, a.Size())
	require.Zero(t, b.Size())
	_, sent = b.TrySend(lua.LString("b"))
	require.True(t, sent)
	require.Equal(t, 1, a.Drain())
	require.Zero(t, a.Size())
	require.Equal(t, 1, b.Size())
	r := b.Receive(nil, nil)
	require.Equal(t, []lua.LValue{lua.LString("b"), lua.LTrue}, r.Updates[0].GetResult())
	ReleaseResult(r)
	require.Nil(t, a.Close(nil))
	require.False(t, b.IsClosed())
}

func TestChannelValueCopyPreservesQueueAlias(t *testing.T) {
	original := NewChannel(1)
	_, sent := original.TrySend(lua.LString("before copy"))
	require.True(t, sent)
	copied := *original
	r := copied.Receive(nil, nil)
	require.Equal(t, []lua.LValue{lua.LString("before copy"), lua.LTrue}, r.Updates[0].GetResult())
	ReleaseResult(r)
	require.Zero(t, original.Size(), "native value copies must still share the original queue")
	_, sent = copied.TrySend(lua.LString("after copy"))
	require.True(t, sent)
	require.Equal(t, 1, original.Drain())
	require.Zero(t, copied.Size())
}

var channelChurnSink *Channel

func BenchmarkChannelConstruction(b *testing.B) {
	for _, capacity := range []int{0, 1, 64} {
		b.Run(fmt.Sprint(capacity), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				channelChurnSink = NewChannel(capacity)
			}
		})
	}
}
