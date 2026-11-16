// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"testing"

	sec "github.com/wippyai/runtime/api/security"
)

func BenchmarkPatternPlanStorage(b *testing.B) {
	for _, n := range []int{430, 1024} {
		for _, named := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/named=%t", n, named), func(b *testing.B) {
				list, _ := shapedPolicies(b, n)
				scopes := make([]sec.Scope, b.N)
				actions := make([]string, 10000)
				for i := range actions {
					actions[i] = fmt.Sprintf("unnamed.%08d", i)
				}
				runtime.GC()
				var before, warm, streamed runtime.MemStats
				runtime.ReadMemStats(&before)
				b.ReportAllocs()
				b.ResetTimer()
				for i := range scopes {
					scopes[i] = freshShapedScope(list)
					scopes[i].Evaluate(sec.Actor{}, "unnamed.warm", "missing", nil)
					if named {
						for _, action := range append(shapeOtherActions(), shapedHotActions...) {
							scopes[i].Evaluate(sec.Actor{}, action, "missing", nil)
						}
					}
				}
				b.StopTimer()
				runtime.GC()
				runtime.ReadMemStats(&warm)
				for _, s := range scopes {
					for _, action := range actions {
						s.Evaluate(sec.Actor{}, action, "missing", nil)
					}
				}
				runtime.GC()
				runtime.ReadMemStats(&streamed)
				b.ReportMetric(float64(int64(warm.HeapAlloc)-int64(before.HeapAlloc))/float64(b.N), "retained-B/segment")
				b.ReportMetric(float64(int64(streamed.HeapAlloc)-int64(warm.HeapAlloc))/float64(b.N), "stream-delta-B/segment")
				runtime.KeepAlive(scopes)
				runtime.KeepAlive(list)
				runtime.KeepAlive(actions)
			})
		}
	}
}
