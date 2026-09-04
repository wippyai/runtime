// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	relaysys "github.com/wippyai/runtime/system/relay"
)

type inbox struct{ packages chan *relay.Package }

func (i *inbox) Send(pkg *relay.Package) error { i.packages <- pkg; return nil }

func processContext(t testing.TB, service *Service) (context.Context, ctxapi.FrameContext, *inbox) {
	return processContextFor(t, service, "paint")
}

func processContextFor(t testing.TB, service *Service, id string) (context.Context, ctxapi.FrameContext, *inbox) {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	ctx = ttyapi.WithService(ctx, service)
	node := relaysys.NewNode("node")
	box := &inbox{packages: make(chan *relay.Package, 1)}
	require.NoError(t, node.RegisterHost("workers", box))
	ctx = relay.WithNode(ctx, node)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.Set(runtime.FramePIDKey, pid.PID{Node: "node", Host: "workers", UniqID: id}))
	return ctx, frame, box
}

func TestViewportBindingLifecycle(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, box := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	_, err = service.Binding(view.Grant())
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)

	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	require.NoError(t, port.InputController().Start())
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	stats, err := surface.Present(ttyapi.Frame{Rows: []string{"paint", "ready"}})
	require.NoError(t, err)
	require.Equal(t, 2, stats.ChangedRows)
	require.Equal(t, []string{"paint", "ready"}, view.Snapshot().Rows)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"paint", "ready"},
		Cursor: &ttyapi.Cursor{Column: 3, Row: 1, Visible: true}})
	require.NoError(t, err)
	require.Equal(t, &ttyapi.Cursor{Column: 3, Row: 1, Visible: true}, view.Snapshot().Cursor)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"row-only update"}})
	require.NoError(t, err)
	require.Equal(t, &ttyapi.Cursor{Column: 3, Row: 1, Visible: true}, view.Snapshot().Cursor,
		"row-only presentation must preserve cursor state")
	revision := view.Snapshot().Revision
	surface.Invalidate()
	require.Equal(t, []string{"row-only update"}, view.Snapshot().Rows,
		"invalidation must not erase the published snapshot")
	stats, err = surface.Present(ttyapi.Frame{Rows: []string{"row-only update"}})
	require.NoError(t, err)
	require.Equal(t, 1, stats.ChangedRows)
	require.Greater(t, view.Snapshot().Revision, revision)

	require.NoError(t, view.Send(ttyapi.Event{Type: "key", Key: "x"}))
	pkg := <-box.packages
	require.Equal(t, ttyapi.TopicEvents, pkg.Messages[0].Topic)

	require.NoError(t, binding.Close())
	require.ErrorIs(t, view.Send(ttyapi.Event{Type: "key"}), ttyapi.ErrViewportClosed)
	require.NoError(t, view.Close())
}

func TestViewportInputFollowsProducerInputLifecycle(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, box := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	input := port.InputController()

	require.ErrorIs(t, view.Send(ttyapi.Event{Type: "key", Key: "x"}), ttyapi.ErrInputInactive)
	require.NoError(t, input.Start())
	require.NoError(t, view.Send(ttyapi.Event{Type: "key", Key: "x"}))
	require.Equal(t, ttyapi.TopicEvents, (<-box.packages).Messages[0].Topic)
	require.NoError(t, input.Stop())
	require.ErrorIs(t, view.Send(ttyapi.Event{Type: "key", Key: "x"}), ttyapi.ErrInputInactive)
}

func TestVirtualPortOwnsOnePresentationLease(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()
	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	first, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	_, err = port.OpenSurface(ttyapi.SurfaceOptions{})
	require.ErrorIs(t, err, ttyapi.ErrSurfaceOpen)
	require.NoError(t, first.Close())
	second, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	require.NoError(t, second.Close())
	require.NoError(t, port.Close())
	_, err = port.OpenSurface(ttyapi.SurfaceOptions{})
	require.ErrorIs(t, err, ttyapi.ErrInvalidPort)
}

func TestClosedVirtualSurfaceCannotInvalidateItsSuccessor(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()
	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)

	first, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	_, err = first.Present(ttyapi.Frame{Rows: []string{"stable"}})
	require.NoError(t, err)
	require.NoError(t, first.Close())
	revision := view.Snapshot().Revision
	first.Invalidate()

	second, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	stats, err := second.Present(ttyapi.Frame{Rows: []string{"stable"}})
	require.NoError(t, err)
	require.Zero(t, stats.ChangedRows)
	require.Equal(t, revision, view.Snapshot().Revision)
}

func TestViewportResizeBeforeBinding(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, box := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	require.NoError(t, view.Resize(90, 30))
	require.Equal(t, 90, view.Snapshot().Width)
	require.Equal(t, 30, view.Snapshot().Height)

	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	_, err = binding.Resolve(ctx)
	require.NoError(t, err)
	require.NoError(t, view.Resize(100, 35))
	pkg := <-box.packages
	event, ok := pkg.Messages[0].Payloads[0].Data().(*ttyapi.Event)
	require.True(t, ok)
	require.Equal(t, ttyapi.Event{Type: "resize", Width: 100, Height: 35}, *event)
}

func TestViewportResizeAdvancesRevisionAndNotifiesEveryViewer(t *testing.T) {
	service := NewService()
	defer service.Close()
	creatorCtx, creatorFrame, _ := processContextFor(t, service, "creator")
	defer creatorFrame.Close()
	viewerCtx, viewerFrame, _ := processContextFor(t, service, "viewer")
	defer viewerFrame.Close()

	creator, err := service.Create(creatorCtx, 40, 12)
	require.NoError(t, err)
	viewer, err := service.Attach(viewerCtx, creator.Handle())
	require.NoError(t, err)
	before := creator.Snapshot().Revision

	require.NoError(t, creator.Resize(90, 30))
	require.Greater(t, creator.Snapshot().Revision, before)
	require.Equal(t, creator.Snapshot().Revision, (<-creator.Updates()).Revision)
	require.Equal(t, viewer.Snapshot().Revision, (<-viewer.Updates()).Revision)
}

func TestClosingUnresolvedBindingRestoresOneShotGrant(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	grant := view.Grant()
	binding, err := service.Binding(grant)
	require.NoError(t, err)
	require.NoError(t, binding.Close())

	retry, err := service.Binding(grant)
	require.NoError(t, err, "an attachment rejected before frame ownership must be retryable")
	require.NoError(t, retry.Close())
}

func TestClosingResolvedBindingDoesNotRestoreProducerGrant(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	grant := view.Grant()
	binding, err := service.Binding(grant)
	require.NoError(t, err)
	_, err = binding.Resolve(ctx)
	require.NoError(t, err)
	require.NoError(t, binding.Close())

	_, err = service.Binding(grant)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
}

func TestSnapshotRowsRemainImmutable(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)

	_, err = surface.Present(ttyapi.Frame{Rows: []string{"first"}})
	require.NoError(t, err)
	first := view.Snapshot()
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"second"}})
	require.NoError(t, err)
	require.Equal(t, []string{"first"}, first.Rows)
	require.Equal(t, []string{"second"}, view.Snapshot().Rows)
}

func TestViewportUpdatesAreCoalescedAndCloseWithView(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)

	_, err = surface.Present(ttyapi.Frame{Rows: []string{"one"}})
	require.NoError(t, err)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"two"}})
	require.NoError(t, err)
	update := <-view.Updates()
	require.Equal(t, uint64(2), update.Revision)
	require.Equal(t, uint64(2), view.Snapshot().Revision)
	select {
	case <-view.Updates():
		t.Fatal("a slow viewer should receive one coalesced notification")
	default:
	}

	require.NoError(t, view.Close())
	_, open := <-view.Updates()
	require.False(t, open)
}

func TestViewportRejectsOversizedGeometry(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, ttyapi.MaxViewportCells, 2)
	require.Nil(t, view)
	require.ErrorIs(t, err, ttyapi.ErrInvalidViewportSize)

	view, err = service.Create(ctx, 80, 24)
	require.NoError(t, err)
	require.ErrorIs(t, view.Resize(ttyapi.MaxViewportCells, 2), ttyapi.ErrInvalidViewportSize)
}

func TestViewportCanReattachWithoutStoppingProducer(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	handle := view.Handle()
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	require.NoError(t, view.Close())
	require.ErrorIs(t, view.Send(ttyapi.Event{Type: "key"}), ttyapi.ErrViewportClosed)

	_, err = surface.Present(ttyapi.Frame{Rows: []string{"still running"}})
	require.NoError(t, err)
	attached, err := service.Attach(ctx, handle)
	require.NoError(t, err)
	require.Empty(t, attached.Grant(), "reattachment handles must not mint producer authority")
	require.Equal(t, []string{"still running"}, attached.Snapshot().Rows)
	require.NoError(t, attached.Close())
	require.NoError(t, port.Close())
	_, err = service.Attach(ctx, handle)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
}

func TestRedeemedBindingKeepsSessionAttachableUntilResolution(t *testing.T) {
	service := NewService()
	defer service.Close()
	producerCtx, producerFrame, _ := processContextFor(t, service, "producer")
	defer producerFrame.Close()
	viewerCtx, viewerFrame, _ := processContextFor(t, service, "viewer")
	defer viewerFrame.Close()

	creator, err := service.Create(producerCtx, 40, 12)
	require.NoError(t, err)
	handle := creator.Handle()
	binding, err := service.Binding(creator.Grant())
	require.NoError(t, err)
	require.NoError(t, creator.Close())

	viewer, err := service.Attach(viewerCtx, handle)
	require.NoError(t, err, "a pending binding owns the session")
	port, err := binding.Resolve(producerCtx)
	require.NoError(t, err)
	require.NoError(t, viewer.Close())
	require.NoError(t, port.Close())
	_, err = service.Attach(viewerCtx, handle)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
}

func TestConcurrentAttachPresentAndClose(t *testing.T) {
	service := NewService()
	defer service.Close()
	producerCtx, producerFrame, _ := processContextFor(t, service, "producer")
	defer producerFrame.Close()
	viewerCtx, viewerFrame, _ := processContextFor(t, service, "viewer")
	defer viewerFrame.Close()

	creator, err := service.Create(producerCtx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(creator.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(producerCtx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_, presentErr := surface.Present(ttyapi.Frame{Rows: []string{string(rune('a' + i%26))}})
			if presentErr != nil {
				errs <- presentErr
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			view, attachErr := service.Attach(viewerCtx, creator.Handle())
			if attachErr != nil {
				errs <- attachErr
				return
			}
			if closeErr := view.Close(); closeErr != nil {
				errs <- closeErr
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestCreateAndAttachRequireAProcessOwner(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), service)
	_, err := service.Create(ctx, 40, 12)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
}

func TestDetachingOneViewerDoesNotInvalidateAnother(t *testing.T) {
	service := NewService()
	defer service.Close()
	creatorCtx, creatorFrame, _ := processContextFor(t, service, "shell-a")
	defer creatorFrame.Close()
	viewerCtx, viewerFrame, _ := processContextFor(t, service, "shell-b")
	defer viewerFrame.Close()

	creator, err := service.Create(creatorCtx, 40, 12)
	require.NoError(t, err)
	require.NotEmpty(t, creator.Grant())
	viewer, err := service.Attach(viewerCtx, creator.Handle())
	require.NoError(t, err)
	require.Empty(t, viewer.Grant())

	require.NoError(t, creator.Close())
	require.Empty(t, creator.Snapshot().Rows)
	require.ErrorIs(t, creator.Resize(80, 24), ttyapi.ErrViewportClosed)
	require.ErrorIs(t, creator.Send(ttyapi.Event{Type: "key"}), ttyapi.ErrViewportClosed)
	require.Equal(t, 40, viewer.Snapshot().Width)
	require.NoError(t, viewer.Resize(80, 24))
	require.Equal(t, 80, viewer.Snapshot().Width)
}

func TestLifecycleDetachesOnlyCompletedProcessViews(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctxA, frameA, _ := processContextFor(t, service, "shell-a")
	defer frameA.Close()
	ctxB, frameB, _ := processContextFor(t, service, "shell-b")
	defer frameB.Close()

	viewA, err := service.Create(ctxA, 40, 12)
	require.NoError(t, err)
	viewB, err := service.Attach(ctxB, viewA.Handle())
	require.NoError(t, err)

	service.OnComplete(context.Background(), pid.PID{Node: "node", Host: "workers", UniqID: "shell-a"}, nil)
	// Lifecycle cleanup detaches the process-owned reference in the broker. The
	// local object may still be called until its frame closer closes it, while
	// the other process remains independently attached.
	require.Equal(t, 40, viewB.Snapshot().Width)
	require.NoError(t, viewB.Resize(90, 30))
	require.Equal(t, 90, viewA.Snapshot().Width)
	require.NoError(t, viewA.Close())
	require.NoError(t, viewB.Close())
}

func TestLifecycleReleasesUnresolvedBinding(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContextFor(t, service, "producer")
	viewerCtx, viewerFrame, _ := processContextFor(t, service, "viewer")
	defer viewerFrame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	handle := view.Handle()
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	require.NoError(t, frame.Set(ttyapi.PortKey(), binding))
	require.NoError(t, view.Close())

	service.OnComplete(ctx, pid.PID{Node: "node", Host: "workers", UniqID: "producer"}, nil)
	_, err = service.Attach(viewerCtx, handle)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
	require.NoError(t, frame.Close())
}

func TestLifecycleClosesResolvedProducerPort(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContextFor(t, service, "producer")
	defer frame.Close()

	view, err := service.Create(ctx, 40, 12)
	require.NoError(t, err)
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	require.NoError(t, frame.Set(ttyapi.PortKey(), binding))
	port, err := ttyapi.GetPort(ctx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)

	service.OnComplete(ctx, pid.PID{Node: "node", Host: "workers", UniqID: "producer"}, nil)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"after exit"}})
	require.ErrorIs(t, err, ttyapi.ErrViewportClosed)
}

func BenchmarkVirtualSurfacePresentUnchanged(b *testing.B) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(b, service)
	defer frame.Close()
	view, _ := service.Create(ctx, 120, 40)
	binding, _ := service.Binding(view.Grant())
	port, _ := binding.Resolve(ctx)
	surface, _ := port.OpenSurface(ttyapi.SurfaceOptions{})
	rows := make([]string, 40)
	_, _ = surface.Present(ttyapi.Frame{Rows: rows})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = surface.Present(ttyapi.Frame{Rows: rows})
	}
}
