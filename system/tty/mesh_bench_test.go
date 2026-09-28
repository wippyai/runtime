// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func BenchmarkMeshSnapshotFanout(b *testing.B) {
	for _, count := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("observers=%d", count), func(b *testing.B) {
			a, peer, ta, tb := meshFixture(b)
			owner, _ := meshContext(b, a, "a", "owner")
			view, err := a.Create(owner, 120, 40)
			require.NoError(b, err)
			binding, err := a.Binding(view.Grant())
			require.NoError(b, err)
			port, err := binding.Resolve(owner)
			require.NoError(b, err)
			surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
			require.NoError(b, err)
			views := make([]ttyapi.Viewport, 0, count)
			for i := range count {
				agent, _ := meshContext(b, peer, "b", strconv.Itoa(i))
				target, _ := runtime.GetFramePID(agent)
				ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
				require.NoError(b, err)
				remote, err := peer.Attach(agent, ref)
				require.NoError(b, err)
				views = append(views, remote)
			}
			before := ta.sent.Load() + tb.sent.Load()
			rows := make([]string, 40)
			fill := strings.Repeat("x", 120)
			for i := range rows {
				rows[i] = fill
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				rows[0] = strconv.Itoa(i) + fill[20:]
				if _, err := surface.Present(ttyapi.Frame{Rows: rows}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			revision := view.Snapshot().Revision
			require.Eventually(b, func() bool {
				for _, v := range views {
					if v.Snapshot().Revision != revision {
						return false
					}
				}
				return true
			}, time.Second, time.Millisecond)
			b.ReportMetric(float64(ta.sent.Load()+tb.sent.Load()-before)/float64(b.N), "wire_frames/present")
		})
	}
}

func BenchmarkRemoteCachedSnapshot(b *testing.B) {
	a, peer, _, _ := meshFixture(b)
	owner, _ := meshContext(b, a, "a", "owner")
	agent, _ := meshContext(b, peer, "b", "agent")
	view, err := a.Create(owner, 120, 40)
	require.NoError(b, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
	require.NoError(b, err)
	remote, err := peer.Attach(agent, ref)
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = remote.Snapshot()
	}
}
