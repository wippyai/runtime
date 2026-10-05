// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func BenchmarkReleasePIDWithoutNames(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s := NewService(Config{LocalNodeID: "local"})
			b.Cleanup(func() { _ = s.Stop() })
			for i := range count {
				p := pid.PID{Node: "local", Host: "worker", UniqID: fmt.Sprint(i)}
				_, err := s.Register(fmt.Sprintf("service-%d", i), p)
				require.NoError(b, err)
			}
			p := (&pid.PID{Node: "local", Host: "chat", UniqID: "no-name"}).Precomputed()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.ReleasePID(p)
			}
		})
	}
}

func TestReleasePIDHandlesCachedAndUncachedIdentities(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	a := pid.PID{Node: "local", Host: "workers", UniqID: "owner"}
	_, err := s.Register("one", a.Precomputed())
	require.NoError(t, err)
	_, err = s.Register("two", a)
	require.NoError(t, err)
	s.ReleasePID(a)
	_, found := s.state.Lookup("one")
	require.False(t, found)
	_, found = s.state.Lookup("two")
	require.False(t, found)
	require.Empty(t, s.ownedByPID, "empty owner buckets must not accumulate")
}

func TestReleasePIDLeavesAReplacementOwnerAlone(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	a := pid.PID{Node: "local", Host: "workers", UniqID: "a"}
	b := pid.PID{Node: "local", Host: "workers", UniqID: "b"}
	_, err := s.Register("name", a)
	require.NoError(t, err)
	require.True(t, s.Unregister("name"))
	_, err = s.Register("name", b)
	require.NoError(t, err)
	s.ReleasePID(a)
	got, found := s.state.Lookup("name")
	require.True(t, found)
	require.True(t, got.Equal(b))
	s.ReleasePID(b)
	_, found = s.state.Lookup("name")
	require.False(t, found)
	require.Empty(t, s.ownedByPID)
}
