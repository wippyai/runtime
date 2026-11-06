// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"testing"

	sec "github.com/wippyai/runtime/api/security"
)

func BenchmarkShapedPatternActions(b *testing.B) {
	for _, n := range []int{430, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			list, _ := shapedPolicies(b, n)
			s := freshShapedScope(list)
			actions := make([]string, 10000)
			for i := range actions {
				actions[i] = fmt.Sprintf("unnamed.%08d", i)
			}
			for _, kind := range []string{"check", "stream"} {
				b.Run(kind, func(b *testing.B) {
					// Warm every input, including inputs beyond a caller-key retention limit.
					for _, action := range actions {
						s.Evaluate(sec.Actor{}, action, "missing", nil)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						action := actions[len(actions)-1]
						if kind == "stream" {
							action = actions[i%len(actions)]
						}
						benchmarkResult = s.Evaluate(sec.Actor{}, action, "missing", nil)
					}
				})
			}
		})
	}
}

func TestPatternStreamRetainedMemory(t *testing.T) {
	for _, n := range []int{430, 1024} {
		list, _ := shapedPolicies(t, n)
		part := freshShapedScope(list).(*scope).segments[0]
		warm := part.plan("unnamed.warm")
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < 10000; i++ {
			part.evaluate(sec.Actor{}, fmt.Sprintf("unnamed.%08d", i), "missing", nil)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("policies=%d live heap delta after 10000 actions=%d bytes", n, int64(after.HeapAlloc)-int64(before.HeapAlloc))
		if warm != part.plan("unnamed.final") {
			t.Fatal("pattern plan changes with caller key")
		}
		runtime.KeepAlive(part)
	}
}
