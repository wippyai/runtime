package otel

import (
	"context"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWithTracer_GetTracer(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx = WithTracer(ctx, tracer)

	retrieved := GetTracer(ctx)
	assert.NotNil(t, retrieved)
	assert.Equal(t, tracer, retrieved)
}

func TestGetTracer_NoAppContext(t *testing.T) {
	ctx := context.Background()
	retrieved := GetTracer(ctx)
	assert.Nil(t, retrieved)
}

func TestGetTracer_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	retrieved := GetTracer(ctx)
	assert.Nil(t, retrieved)
}

func TestWithTracer_SetOnce(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	tracer1 := noop.NewTracerProvider().Tracer("test1")
	tracer2 := noop.NewTracerProvider().Tracer("test2")

	ctx = WithTracer(ctx, tracer1)
	ctx = WithTracer(ctx, tracer2)

	retrieved := GetTracer(ctx)
	assert.Equal(t, tracer1, retrieved)
}

func TestSetSpan_GetSpan(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(ctx, "test-span")
	defer span.End()

	err := SetSpan(ctx, span)
	assert.NoError(t, err)

	retrieved, ok := GetSpan(ctx)
	assert.True(t, ok)
	assert.Equal(t, span, retrieved)
}

func TestGetSpan_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	retrieved, ok := GetSpan(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestGetSpan_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	retrieved, ok := GetSpan(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestSpanInheritance(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, parentFrame := ctxapi.OpenFrameContext(ctx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, parentSpan := tracer.Start(ctx, "parent-span")
	defer parentSpan.End()

	err := SetSpan(ctx, parentSpan)
	assert.NoError(t, err)

	parentFrame.Seal()

	childCtx, _ := ctxapi.OpenFrameContext(ctx)

	retrieved, ok := GetSpan(childCtx)
	assert.True(t, ok)
	assert.Equal(t, parentSpan, retrieved)
}
