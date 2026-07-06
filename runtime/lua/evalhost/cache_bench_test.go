// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
)

// makeEvalSources returns n distinct eval programs so each has its own compile
// key.
func makeEvalSources(n int) []RunCmd {
	cmds := make([]RunCmd, n)
	for i := range n {
		cmds[i] = RunCmd{
			Source:  fmt.Sprintf(`return { run = function() return %d * 2 + 1 end }`, i),
			Method:  "run",
			Modules: []string{"json"},
		}
	}
	return cmds
}

// benchEvalRun runs full eval (compile + process + execute) cycling through 60
// distinct programs. cacheSize 0 disables the compile cache.
func benchEvalRun(b *testing.B, cacheSize int) {
	var opts []HostOption
	if cacheSize > 0 {
		opts = append(opts, WithProgramCache(HostConfig{CacheSize: cacheSize}))
	}
	host := NewHost(zap.NewNop(), safeModulesProvider(), opts...)
	cmds := makeEvalSources(60)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := host.Run(ctx, cmds[i%len(cmds)]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalRun_60Codes_Cached(b *testing.B)   { benchEvalRun(b, 64) }
func BenchmarkEvalRun_60Codes_Uncached(b *testing.B) { benchEvalRun(b, 0) }

// benchEvalCompile isolates the compile path (no process/execute) to show the
// raw compile-cache effect.
func benchEvalCompile(b *testing.B, cacheSize int) {
	var opts []HostOption
	if cacheSize > 0 {
		opts = append(opts, WithProgramCache(HostConfig{CacheSize: cacheSize}))
	}
	host := NewHost(zap.NewNop(), safeModulesProvider(), opts...)
	runCmds := makeEvalSources(60)
	cmds := make([]CompileCmd, len(runCmds))
	for i, rc := range runCmds {
		cmds[i] = CompileCmd{Source: rc.Source, Method: rc.Method, Modules: rc.Modules}
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := host.Compile(ctx, cmds[i%len(cmds)]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalCompile_60Codes_Cached(b *testing.B)   { benchEvalCompile(b, 64) }
func BenchmarkEvalCompile_60Codes_Uncached(b *testing.B) { benchEvalCompile(b, 0) }
