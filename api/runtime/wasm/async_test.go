package wasm

import (
	"errors"
	"testing"

	appctx "github.com/wippyai/runtime/api/context"
)

// mockResourceCloser tracks Close() calls for testing.
type mockResourceCloser struct {
	closed bool
	err    error
}

func (m *mockResourceCloser) Close() error {
	m.closed = true
	return m.err
}

func TestAsyncFrame(t *testing.T) {
	t.Run("SetAsyncFrame and GetAsyncFrame", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		frame := &AsyncFrame{Scheduler: nil}
		if err := SetAsyncFrame(ctx, frame); err != nil {
			t.Fatalf("SetAsyncFrame error: %v", err)
		}

		got := GetAsyncFrame(ctx)
		if got != frame {
			t.Error("expected same frame back")
		}
	})

	t.Run("SetAsyncFrame without frame context returns error", func(t *testing.T) {
		ctx := appctx.NewRootContext()

		err := SetAsyncFrame(ctx, &AsyncFrame{})
		if err != appctx.ErrNoFrameContext {
			t.Errorf("expected ErrNoFrameContext, got %v", err)
		}
	})

	t.Run("GetAsyncFrame without frame context returns nil", func(t *testing.T) {
		ctx := appctx.NewRootContext()

		got := GetAsyncFrame(ctx)
		if got != nil {
			t.Error("expected nil frame")
		}
	})
}

func TestWithResources(t *testing.T) {
	t.Run("adds resources to existing frame", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)
		_ = SetAsyncFrame(ctx, &AsyncFrame{})

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		got := GetResources(ctx)
		if got != res {
			t.Error("expected same resources")
		}
	})

	t.Run("creates frame if needed", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		got := GetResources(ctx)
		if got != res {
			t.Error("expected resources even without prior frame")
		}
	})

	t.Run("creates frame context if needed", func(t *testing.T) {
		ctx := appctx.NewRootContext()

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		got := GetResources(ctx)
		if got != res {
			t.Error("expected resources with auto-created frame context")
		}
	})
}

func TestGetResources(t *testing.T) {
	t.Run("returns nil without frame", func(t *testing.T) {
		ctx := appctx.NewRootContext()

		got := GetResources(ctx)
		if got != nil {
			t.Error("expected nil resources")
		}
	})

	t.Run("returns nil without async frame", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		got := GetResources(ctx)
		if got != nil {
			t.Error("expected nil resources")
		}
	})
}

func TestCloseResources(t *testing.T) {
	t.Run("closes resources and clears", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		err := CloseResources(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !res.closed {
			t.Error("expected resources to be closed")
		}

		got := GetResources(ctx)
		if got != nil {
			t.Error("expected resources to be cleared after close")
		}
	})

	t.Run("returns error from Close", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		expectedErr := errors.New("close failed")
		res := &mockResourceCloser{err: expectedErr}
		ctx = WithResources(ctx, res)

		err := CloseResources(ctx)
		if err != expectedErr {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})

	t.Run("no-op without resources", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)
		_ = SetAsyncFrame(ctx, &AsyncFrame{})

		err := CloseResources(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("no-op without frame", func(t *testing.T) {
		ctx := appctx.NewRootContext()

		err := CloseResources(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestAsyncFrameResourceIntegration(t *testing.T) {
	t.Run("resources can be set alongside scheduler", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		// Scheduler can coexist with resources
		frame := GetAsyncFrame(ctx)
		if frame == nil {
			t.Fatal("expected frame")
		}
		if frame.Resources != res {
			t.Error("expected resources in frame")
		}
	})

	t.Run("double close is safe", func(t *testing.T) {
		ctx := appctx.NewRootContext()
		ctx, _ = appctx.OpenFrameContext(ctx)

		res := &mockResourceCloser{}
		ctx = WithResources(ctx, res)

		_ = CloseResources(ctx)
		err := CloseResources(ctx)
		if err != nil {
			t.Errorf("double close should not error: %v", err)
		}
	})
}
