// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func TestReleasePackageClearsMessageSlots(t *testing.T) {
	for _, size := range []int{1, 64, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			pkg := &Package{Messages: make([]*Message, size, size+1)}
			for i := range pkg.Messages {
				pkg.Messages[i] = &Message{Topic: "work", Payloads: payload.Payloads{payload.New(i)}}
			}
			backing := pkg.Messages
			capacity := cap(pkg.Messages)
			ReleasePackage(pkg)
			require.Empty(t, pkg.Messages)
			require.Equal(t, capacity, cap(pkg.Messages), "ordinary release should reuse the backing array")
			for i, msg := range backing {
				require.Nil(t, msg, "released slot %d must not retain a message that can be reused for another payload", i)
			}
		})
	}
}

func TestReleaseMessagePackageClearsTransferredSlice(t *testing.T) {
	messages := []*Message{{Topic: "first"}, nil, {Topic: "last"}}
	pkg := NewMessagePackage(pid.Zero(), pid.Zero(), messages...)
	require.Same(t, messages[0], pkg.Messages[0])
	require.Same(t, messages[2], pkg.Messages[2])
	ReleasePackage(pkg)
	for _, msg := range messages {
		require.Nil(t, msg, "the package owns the transferred slice as well as its messages")
	}
}

type poolRetentionLease struct{ releases atomic.Int32 }

func (l *poolRetentionLease) Release() { l.releases.Add(1) }

func TestReleasePackagePreservesLeaseHandoff(t *testing.T) {
	live, transferred := &poolRetentionLease{}, &poolRetentionLease{}
	first, second := &Message{}, &Message{}
	first.SetRetentionLease(live)
	second.SetRetentionLease(transferred)
	owned := second.TakeRetentionLease()
	pkg := &Package{Messages: []*Message{first, nil, second}}
	ReleasePackage(pkg)
	require.Equal(t, int32(1), live.releases.Load())
	require.Zero(t, transferred.releases.Load(), "package release must not release a transferred reservation")
	owned.Release()
	require.Equal(t, int32(1), transferred.releases.Load())
}

func TestReleasePackagePreservesCallerPayloads(t *testing.T) {
	values := payload.Payloads{payload.New("hello"), payload.New(42)}
	pkg := NewPackage(pid.Zero(), pid.Zero(), "work", values...)
	ReleasePackage(pkg)
	require.Equal(t, "hello", values[0].Data())
	require.Equal(t, 42, values[1].Data())
}

func BenchmarkPackageRoundTrip(b *testing.B) {
	values := payload.Payloads{payload.New("hello")}
	for _, size := range []int{1, 64, 1024} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			roundTrip := func() {
				pkg := AcquirePackage()
				for range size {
					msg := AcquireMessage()
					msg.Topic, msg.Payloads = "work", values
					pkg.Messages = append(pkg.Messages, msg)
				}
				ReleasePackage(pkg)
			}
			roundTrip()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				roundTrip()
			}
		})
	}
}
