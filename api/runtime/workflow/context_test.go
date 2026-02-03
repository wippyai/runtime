package workflow

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestIsDeterministic_NoFrame(t *testing.T) {
	ctx := context.Background()
	if IsDeterministic(ctx) {
		t.Error("expected false when no frame context")
	}
}

func TestIsDeterministic_NotSet(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if IsDeterministic(ctx) {
		t.Error("expected false when deterministic not set")
	}
}

func TestSetDeterministic(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	if err := SetDeterministic(ctx); err != nil {
		t.Fatalf("SetDeterministic failed: %v", err)
	}

	if !IsDeterministic(ctx) {
		t.Error("expected true after SetDeterministic")
	}
}

func TestSetDeterministic_NoFrame(t *testing.T) {
	ctx := context.Background()
	if err := SetDeterministic(ctx); err == nil {
		t.Error("expected error when no frame context")
	}
}

func TestSetDeterministic_SealedFrame(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	fc.Seal()

	if err := SetDeterministic(ctx); err == nil {
		t.Error("expected error when frame is sealed")
	}
}
