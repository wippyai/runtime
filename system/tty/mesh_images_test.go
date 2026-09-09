// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func largeFixtureImage(t testing.TB, s *Service) (*ttyapi.Image, []byte) {
	t.Helper()
	rgba := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	_, err := rand.New(rand.NewSource(17)).Read(rgba.Pix)
	require.NoError(t, err)
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, rgba))
	img, err := s.images.ImportPNG(b.Bytes())
	require.NoError(t, err)
	return img, b.Bytes()
}

type imageMeshFixture struct {
	box          *inbox
	a, b         *Service
	ta, tb       *testSurfaceTransport
	owner, agent context.Context
	view, remote ttyapi.Viewport
	output       ttyapi.Surface
	image        *ttyapi.Image
	ref          string
	data         []byte
	frame        ttyapi.Frame
}

func imageMesh(t testing.TB, ownerGraphics, consumerGraphics bool) *imageMeshFixture {
	t.Helper()
	a, b, ta, tb := meshFixture(t)
	ta.graphics.Store(ownerGraphics)
	tb.graphics.Store(consumerGraphics)
	owner, box := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	binding, err := a.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(owner)
	require.NoError(t, err)
	require.NoError(t, port.InputController().Start())
	output, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	img, data := largeFixtureImage(t, a)
	t.Cleanup(func() { _ = img.Close() })
	frame := ttyapi.Frame{Rows: []string{"ready"}, Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "chart", Destination: ttyapi.CellRect{Cols: 32, Rows: 8}}}}}
	_, err = output.Present(frame)
	require.NoError(t, err)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, pid.PID{Node: "b", Host: "workers", UniqID: "agent"}, ttyapi.MountRights{Observe: true, Input: true, Resize: true})
	require.NoError(t, err)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	return &imageMeshFixture{a: a, b: b, ta: ta, tb: tb, owner: owner, agent: agent, view: view, remote: remote, output: output, image: img, frame: frame, ref: ref, data: data, box: box}
}
func TestMeshImagesCaptureCacheAndRevocation(t *testing.T) {
	f := imageMesh(t, true, true)
	f.ta.duplicate.Store(true)
	f.tb.duplicate.Store(true)
	capture, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.NoError(t, err)
	snapshot := capture.Snapshot()
	require.Len(t, snapshot.Images, 1)
	img, err := capture.Image(snapshot.Images[0].Image.ID)
	require.NoError(t, err)
	var b bytes.Buffer
	_, err = img.WriteTo(&b)
	require.NoError(t, err)
	require.Equal(t, f.data, b.Bytes())
	chunks := f.tb.imageChunks.Load()
	require.Equal(t, uint64((len(f.data)+imageChunkBytes-1)/imageChunkBytes), chunks)
	f.frame.Images[0].Placement.Destination.X = 7
	_, err = f.output.Present(f.frame)
	require.NoError(t, err)
	moved, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.NoError(t, err)
	require.Equal(t, 7, moved.Snapshot().Images[0].Destination.X)
	require.Equal(t, chunks, f.tb.imageChunks.Load(), "movement must reuse the attachment cache")
	require.Zero(t, capture.Snapshot().Images[0].Destination.X, "capture is revision-bound")
	_, err = f.output.Present(ttyapi.Frame{Rows: []string{"deleted"}})
	require.NoError(t, err)
	require.NoError(t, f.image.Close())
	require.NoError(t, capture.Close())
	require.NoError(t, moved.Close())
	require.NoError(t, f.view.(ttyapi.MountableViewport).Revoke(f.owner, f.ref))
	require.Eventually(t, func() bool { f.b.mesh.mu.Lock(); defer f.b.mesh.mu.Unlock(); return f.b.mesh.views[f.ref] == nil }, time.Second, time.Millisecond)
	_, err = f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.ErrorIs(t, err, ttyapi.ErrMountExpired)
	// Revocation releases the cache, but cannot recall an already acquired image.
	require.Positive(t, f.b.images.Used())
	require.NoError(t, img.Close())
	require.Zero(t, f.b.images.Used())
	require.Zero(t, f.a.images.Used())
}
func TestMeshImagesMixedCapabilitiesStayTextOnly(t *testing.T) {
	for _, pair := range [][2]bool{{true, false}, {false, true}} {
		t.Run(string(rune('0'+btoi(pair[0]))), func(t *testing.T) {
			f := imageMesh(t, pair[0], pair[1])
			snapshot := f.remote.Snapshot()
			require.Empty(t, snapshot.Images)
			require.True(t, snapshot.ImagesOmitted)
			_, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
			require.ErrorIs(t, err, ttyapi.ErrGraphicsUnsupported)
			require.Zero(t, f.tb.imageChunks.Load())
			require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}))
		})
	}
}
func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
func TestMeshImagesInputProgressDuringStalledTransfer(t *testing.T) {
	f := imageMesh(t, true, true)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.ta.setHook(func(_ string, b []byte) error {
		var frame wireFrame
		h := codec.MsgpackHandle{}
		if codec.NewDecoderBytes(b, &h).Decode(&frame) == nil && len(frame.Data) > 0 {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	done := make(chan error, 1)
	go func() {
		capture, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
		if capture != nil {
			_ = capture.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no image chunk")
	}
	inputDone := make(chan error, 1)
	go func() { inputDone <- f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}) }()
	select {
	case err := <-inputDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		unblock.Do(func() { close(release) })
		t.Fatal("blob transfer blocked input")
	}
	unblock.Do(func() { close(release) })
	require.NoError(t, <-done)
}
func TestMeshImagesMemoryAdmissionAndCancellation(t *testing.T) {
	f := imageMesh(t, true, true)
	f.b.images = ttyapi.NewImageStore(1)
	_, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.ErrorIs(t, err, ttyapi.ErrImageBudget)
	require.Zero(t, f.tb.imageChunks.Load())
	require.Zero(t, f.b.images.Used())
	f.a.mu.Lock()
	record := f.a.mounts[f.ref]
	f.a.mu.Unlock()
	record.mu.Lock()
	require.Nil(t, record.capture)
	record.mu.Unlock()
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}))
	// Saturated image admission retries and cancels without advancing input Seq.
	f.b.images = ttyapi.NewImageStore(ttyapi.DefaultImageBudget)
	ctx, cancel := context.WithTimeout(f.agent, 20*time.Millisecond)
	defer cancel()
	f.tb.busy.Store(10000)
	_, err = f.remote.(ttyapi.CaptureViewport).Capture(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	f.tb.busy.Store(0)
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "y"}))
	require.Zero(t, f.b.images.Used())
}
func TestMeshImagesRejectForeignCallerAndMissingObservation(t *testing.T) {
	f := imageMesh(t, true, true)
	stranger, _ := meshContext(t, f.b, "b", "stranger")
	_, err := f.remote.(ttyapi.CaptureViewport).Capture(stranger)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	ref, err := f.view.(ttyapi.MountableViewport).Mount(f.owner, pid.PID{Node: "b", Host: "workers", UniqID: "agent"}, ttyapi.MountRights{Input: true})
	require.NoError(t, err)
	input, err := f.b.Attach(f.agent, ref)
	require.NoError(t, err)
	_, err = input.(ttyapi.CaptureViewport).Capture(f.agent)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	// Owner independently rejects a raw forged request even if client checks are bypassed.
	_, err = f.b.mesh.rpc(f.agent, "a", wireFrame{Op: opCapture, Ref: f.ref, Caller: pid.PID{Node: "b", Host: "workers", UniqID: "stranger"}}, f.b.mesh.done)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
}

func TestMeshImageCaptureLeaseReleasesDeletedResource(t *testing.T) {
	f := imageMesh(t, true, true)
	remote := f.remote.(*remoteViewport)
	reply, err := remote.imageCall(f.agent, wireFrame{Op: opCapture})
	require.NoError(t, err)
	_, err = f.output.Present(ttyapi.Frame{Rows: []string{"deleted"}})
	require.NoError(t, err)
	require.NoError(t, f.image.Close())
	require.Positive(t, f.a.images.Used())
	f.a.mu.Lock()
	record := f.a.mounts[f.ref]
	f.a.mu.Unlock()
	record.mu.Lock()
	record.captureTimer.Reset(time.Millisecond)
	record.mu.Unlock()
	require.Eventually(t, func() bool { return f.a.images.Used() == 0 }, time.Second, time.Millisecond)
	_, err = remote.imageCall(f.agent, wireFrame{Op: opImageChunk, CaptureID: reply.CaptureID, ImageID: reply.Snapshot.Images[0].Image.ID})
	require.ErrorIs(t, err, ttyapi.ErrImageClosed)
	// A delayed duplicate begin cannot resurrect an expired capture.
	f.a.mesh.handle("b", wireFrame{Op: opCapture, Ref: f.ref, Caller: remote.owner, ID: reply.CaptureID})
	require.Zero(t, f.a.images.Used())
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}))
}
func TestMeshImageCorruptionReleasesStagingWithoutClosingMount(t *testing.T) {
	f := imageMesh(t, true, true)
	var once sync.Once
	f.ta.setHook(func(_ string, b []byte) error {
		var frame wireFrame
		h := codec.MsgpackHandle{}
		h.WriteExt = true
		if codec.NewDecoderBytes(b, &h).Decode(&frame) == nil && len(frame.Data) > 0 {
			once.Do(func() {
				frame.Data[0] ^= 1
				var altered bytes.Buffer
				require.NoError(t, codec.NewEncoder(&altered, &h).Encode(frame))
				require.Equal(t, len(b), altered.Len())
				copy(b, altered.Bytes())
			})
		}
		return nil
	})
	_, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.ErrorIs(t, err, ttyapi.ErrImageInvalid)
	require.Zero(t, f.b.images.Used())
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}))
}
func TestMeshImageMetadataOverflowProjectsExplicitTextOnly(t *testing.T) {
	f := imageMesh(t, true, true)
	f.frame.Rows = []string{string(bytes.Repeat([]byte{'x'}, 450<<10))}
	f.frame.Images = nil
	for i := 0; i < ttyapi.MaxImagePlacements; i++ {
		f.frame.Images = append(f.frame.Images, ttyapi.PlacedImage{Resource: f.image, Placement: ttyapi.Placement{ID: fmt.Sprintf("placement-%03d", i), Alt: string(bytes.Repeat([]byte{'a'}, 256)), Destination: ttyapi.CellRect{Cols: 2, Rows: 2}}})
	}
	_, err := f.output.Present(f.frame)
	require.NoError(t, err)
	_, err = f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.ErrorIs(t, err, ttyapi.ErrImageBudget)
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}), "metadata overflow must not destroy the mount")
}

func TestMeshImageCancellationReleasesInFlightReservation(t *testing.T) {
	f := imageMesh(t, true, true)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	f.ta.setHook(func(_ string, b []byte) error {
		var frame wireFrame
		h := codec.MsgpackHandle{}
		if codec.NewDecoderBytes(b, &h).Decode(&frame) == nil && len(frame.Data) > 0 {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	})
	ctx, cancel := context.WithCancel(f.agent)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		capture, err := f.remote.(ttyapi.CaptureViewport).Capture(ctx)
		if capture != nil {
			_ = capture.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no image chunk")
	}
	require.Positive(t, f.b.images.Used(), "staging must be reserved before network data arrives")
	_, err := f.output.Present(ttyapi.Frame{Rows: []string{"deleted"}})
	require.NoError(t, err)
	require.NoError(t, f.image.Close())
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("capture ignored cancellation")
	}
	require.Zero(t, f.b.images.Used())
	require.Zero(t, f.a.images.Used())
	unblock.Do(func() { close(release) })
	require.NoError(t, f.remote.Send(ttyapi.Event{Type: "key", Key: "x"}))
}
