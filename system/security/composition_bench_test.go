// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
)

// stackGrowth holds fresh goroutines after the call so their grown stacks remain
// measurable, without including goroutine creation in the stack delta.
func stackGrowth(n int, call func()) uint64 {
	start, release := make(chan struct{}), make(chan struct{})
	var ready, done, exited sync.WaitGroup
	ready.Add(n)
	done.Add(n)
	exited.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer exited.Done()
			ready.Done()
			<-start
			call()
			done.Done()
			<-release
		}()
	}
	ready.Wait()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	close(start)
	done.Wait()
	runtime.ReadMemStats(&after)
	close(release)
	exited.Wait()
	if after.StackInuse <= before.StackInuse {
		return 0
	}
	return (after.StackInuse - before.StackInuse) / uint64(n)
}

func BenchmarkDeclarationGoroutine(b *testing.B) {
	for _, n := range []int{2, 3, 8, 32} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			base := NewScope(benchmarkPolicies(b, 400, "static"))
			list := make([]sec.Policy, n)
			for i := range list {
				list[i] = benchmarkDeclaration(b, fmt.Sprint(i))
			}
			ctx, config := boundaryFixture(b, base, list)
			_, err := ResolveConfigPairs(ctx, config)
			require.NoError(b, err)
			call := func() {
				if _, err := ResolveConfigPairs(ctx, config); err != nil {
					b.Error(err)
				}
			}
			growth := stackGrowth(64, call)
			done := make(chan struct{})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				go func() { call(); done <- struct{}{} }()
				<-done
			}
			b.ReportMetric(float64(growth), "stack-growth-B/goroutine")
		})
	}
}

func nestedDeclarations(b *testing.B, chains, depth int) [][]sec.Policy {
	result := make([][]sec.Policy, chains)
	for chain := range result {
		result[chain] = make([]sec.Policy, depth)
		for i := range result[chain] {
			result[chain][i] = benchmarkDeclaration(b, fmt.Sprintf("nested-%d-%d", chain, i))
		}
	}
	return result
}

func BenchmarkNestedBoundary(b *testing.B) {
	for _, n := range []int{400, 600} {
		for _, depth := range []int{4, 8, 12, 16} {
			b.Run(fmt.Sprintf("%d/%d", n, depth), func(b *testing.B) {
				base := NewScope(benchmarkPolicies(b, n, "static"))
				declarations := nestedDeclarations(b, 1, depth)[0]
				ctx, config := boundaryFixture(b, base, declarations)
				ids := config.Policies
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					s := base
					for _, id := range ids {
						require.NoError(b, sec.SetScope(ctx, s))
						pairs, err := ResolveConfigPairs(ctx, &sec.Config{Policies: []registry.ID{id}})
						require.NoError(b, err)
						s = pairs[len(pairs)-1].Value.(sec.Scope)
						s.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
					}
					benchmarkScope = s
				}
			})
		}
	}
}

func BenchmarkNestedRetained(b *testing.B) {
	for _, n := range []int{400, 600} {
		for _, depth := range []int{4, 8, 12, 16} {
			b.Run(fmt.Sprintf("%d/%d", n, depth), func(b *testing.B) {
				isolateBenchmarkInterner(b)
				declarations := nestedDeclarations(b, 100, depth)
				policies := make([][]sec.Policy, 10)
				for i := range policies {
					policies[i] = benchmarkPolicies(b, n, "static")
				}
				var all []sec.Policy
				for _, list := range declarations {
					all = append(all, list...)
				}
				ctx, config := boundaryFixture(b, nil, all)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				bases := make([]sec.Scope, len(policies))
				for i := range bases {
					bases[i] = NewScope(policies[i])
				}
				live := make([]sec.Scope, 0, 1000)
				b.ResetTimer()
				for chain := range declarations {
					for _, base := range bases {
						s := base
						for _, id := range config.Policies[chain*depth : (chain+1)*depth] {
							require.NoError(b, sec.SetScope(ctx, s))
							pairs, err := ResolveConfigPairs(ctx, &sec.Config{Policies: []registry.ID{id}})
							require.NoError(b, err)
							s = pairs[len(pairs)-1].Value.(sec.Scope)
							s.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
						}
						live = append(live, s)
					}
				}
				b.StopTimer()
				require.NoError(b, sec.SetScope(ctx, nil))
				runtime.GC()
				runtime.ReadMemStats(&after)
				b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "retained-B/1000-scopes")
				runtime.KeepAlive(live)
				runtime.KeepAlive(bases)
				runtime.KeepAlive(policies)
			})
		}
	}
}

func BenchmarkResidentListGoroutine(b *testing.B) {
	for _, n := range []int{400, 600} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			list := benchmarkPolicies(b, n, "static")
			NewScope(list)
			call := func() { NewScope(list) }
			growth := stackGrowth(64, call)
			done := make(chan struct{})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				go func() { call(); done <- struct{}{} }()
				<-done
			}
			b.ReportMetric(float64(growth), "stack-growth-B/goroutine")
		})
	}
}
