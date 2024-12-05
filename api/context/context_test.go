package context

import (
	"context"
	"testing"
)

func BenchmarkContext(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, LoggerKey, "logger")
		ctx = context.WithValue(ctx, CfgFilenameKey, "cfgfilename")

		val1 := ctx.Value(LoggerKey)
		val2 := ctx.Value(CfgFilenameKey)

		if val1 != "logger" || val2 != "cfgfilename" {
			b.Fail()
		}
	}
}

func BenchmarkRegularKey(b *testing.B) {
	ctx := context.Background()

	type key int
	const uniq1 key = 1
	const uniq2 key = 2

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, uniq1, "logger")
		ctx = context.WithValue(ctx, uniq2, "cfgfilename")

		val1 := ctx.Value(uniq1)
		val2 := ctx.Value(uniq2)

		if val1 != "logger" || val2 != "cfgfilename" {
			b.Fail()
		}
	}
}
