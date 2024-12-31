package context

import (
	"context"
	"testing"
)

func BenchmarkContext(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, LoggerCtx, "logger")

		val1 := ctx.Value(LoggerCtx)

		if val1 != "logger" {
			b.Fail()
		}
	}
}

func BenchmarkRegularKey(b *testing.B) {
	ctx := context.Background()

	type key int
	const uniq1 key = 1

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, uniq1, "logger")

		val1 := ctx.Value(uniq1)

		if val1 != "logger" {
			b.Fail()
		}
	}
}
