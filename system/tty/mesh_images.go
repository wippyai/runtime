// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"context"
	"errors"
	"io"
	"time"

	ttyapi "github.com/wippyai/runtime/api/tty"
)

const (
	imageChunkBytes        = 16 << 10
	captureTimeout         = 10 * time.Second
	opCapture        uint8 = opReply + 1
	opImageChunk     uint8 = opReply + 2
	opCaptureRelease uint8 = opReply + 3
)

func imageOperation(op uint8) bool { return op >= opCapture && op <= opCaptureRelease }
func (m *meshService) supportsGraphics(peer string) bool {
	c, ok := m.transport.(ttyapi.MeshGraphicsChecker)
	return ok && c.SupportsGraphics(peer)
}

// Image reads have no side effects on input sequencing. Capture IDs are request
// IDs; begin is monotonic/idempotent, chunk reads are idempotent by ID+offset,
// and release is idempotent. One capture is in flight per mounted observer.
func (m *meshService) handleImage(peer string, f wireFrame, record *mountRecord) {
	reply := wireFrame{Op: opReply, ID: f.ID, Ref: f.Ref, Graphics: true, CaptureID: f.CaptureID}
	record.mu.Lock()
	var err error
	select {
	case <-record.done:
		err = ttyapi.ErrMountExpired
	default:
	}
	if err == nil && (!record.attached || !record.graphics || !record.rights.Observe) {
		err = ttyapi.ErrPermissionDenied
	}
	if err == nil {
		switch f.Op {
		case opCapture:
			if f.ID < record.captureID || (f.ID == record.captureID && record.capture == nil) {
				err = ttyapi.ErrImageClosed
				break
			}
			if f.ID > record.captureID {
				record.closeCaptureLocked()
				// Reconstruct no identity from wire data: authorization above selects this view.
				ss := record.view.session
				ss.mu.RLock()
				record.capture, err = ttyapi.NewCapture(ttyapi.Snapshot{Rows: ss.rows, Cursor: ss.cursor, Revision: ss.revision, Width: ss.width, Height: ss.height}, ss.images)
				ss.mu.RUnlock()
				if err != nil {
					break
				}
				record.captureID = f.ID
				record.captureTimer = time.AfterFunc(captureTimeout, func() {
					record.mu.Lock()
					defer record.mu.Unlock()
					if record.captureID == f.ID {
						record.closeCaptureLocked()
					}
				})
			}
			reply.CaptureID = record.captureID
			reply.Snapshot = record.capture.Snapshot()
		case opImageChunk:
			if record.capture == nil || f.CaptureID != record.captureID {
				err = ttyapi.ErrImageClosed
				break
			}
			if len(f.ImageID) > 80 || f.Offset < 0 || f.Offset > ttyapi.MaxImageBytes || len(f.Data) > 0 {
				err = ttyapi.ErrImageInvalid
				break
			}
			var img *ttyapi.Image
			img, err = record.capture.Image(f.ImageID)
			if err != nil {
				break
			}
			info, _ := img.Info()
			if f.Offset >= info.Bytes {
				err = ttyapi.ErrImageInvalid
			} else {
				reply.Data = make([]byte, min(imageChunkBytes, info.Bytes-f.Offset))
				_, err = img.ReadAt(reply.Data, int64(f.Offset))
				if errors.Is(err, io.EOF) {
					err = nil
				}
				reply.ImageID = f.ImageID
				reply.Offset = f.Offset
			}
			_ = img.Close()
		case opCaptureRelease:
			if f.CaptureID == record.captureID {
				record.closeCaptureLocked()
			}
		default:
			err = ttyapi.ErrMeshProtocol
		}
	}
	reply.Error = wireError(err)
	if err == nil {
		record.timer.Reset(mountLease)
	}
	record.mu.Unlock()
	// Queue pressure on bulk traffic must not destroy the interactive mount.
	ctx, cancel := context.WithTimeout(context.Background(), meshTimeout)
	defer cancel()
	_ = m.sendImage(ctx, peer, reply)
}
func (m *mountRecord) closeCaptureLocked() {
	if m.captureTimer != nil {
		m.captureTimer.Stop()
		m.captureTimer = nil
	}
	if m.capture != nil {
		_ = m.capture.Close()
		m.capture = nil
	}
}
func (m *meshService) sendImage(ctx context.Context, peer string, f wireFrame) error {
	for {
		err := m.send(peer, f)
		if !errors.Is(err, ttyapi.ErrMeshBusy) {
			return err
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-m.done:
			timer.Stop()
			return ttyapi.ErrServiceUnavailable
		}
	}
}
func (v *remoteViewport) imageCall(ctx context.Context, f wireFrame) (wireFrame, error) {
	f.Ref = v.ref
	f.Caller = v.owner
	f.Graphics = true
	return v.mesh.rpc(ctx, v.peer, f, v.done)
}
func (v *remoteViewport) Capture(ctx context.Context) (*ttyapi.Capture, error) {
	if err := v.Check(ctx, ttyapi.RightObserve); err != nil {
		return nil, err
	}
	v.mu.Lock()
	supported := v.graphics
	v.mu.Unlock()
	if !supported {
		return nil, ttyapi.ErrGraphicsUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	select {
	case v.imageGate <- struct{}{}:
		defer func() { <-v.imageGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-v.done:
		return nil, ttyapi.ErrMountExpired
	}
	select {
	case v.mesh.imageTransfers <- struct{}{}:
		defer func() { <-v.mesh.imageTransfers }()
	default:
		return nil, ttyapi.ErrMeshBusy
	}
	reply, err := v.imageCall(ctx, wireFrame{Op: opCapture})
	if err != nil {
		return nil, err
	}
	captureID := reply.CaptureID
	// A release is bounded even if the caller cancelled. The owner timeout is the
	// final cleanup barrier when the peer/transport disappears before this request.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = v.imageCall(releaseCtx, wireFrame{Op: opCaptureRelease, CaptureID: captureID})
	}()
	snapshot := reply.Snapshot
	if snapshot.ImagesOmitted || len(snapshot.Images) > ttyapi.MaxImagePlacements || captureID == 0 {
		return nil, ttyapi.ErrImageBudget
	}
	acquired := make(map[string]*ttyapi.Image)
	defer func() {
		for _, img := range acquired {
			_ = img.Close()
		}
	}()
	placements := make([]ttyapi.PlacedImage, 0, len(snapshot.Images))
	for _, p := range snapshot.Images {
		info := p.Image
		if info.Format != "png" || info.Bytes < 1 || info.Bytes > ttyapi.MaxImageBytes || info.Width < 1 || info.Height < 1 || info.Width > ttyapi.MaxImagePixels/info.Height {
			return nil, ttyapi.ErrImageInvalid
		}
		img := acquired[info.ID]
		if img == nil {
			v.mu.Lock()
			if cached := v.imageCache[info.ID]; cached != nil {
				img, err = cached.Retain()
			}
			v.mu.Unlock()
			if err != nil {
				return nil, err
			}
			if img == nil {
				img, err = v.mesh.service.images.ImportPNGReader(info, &remoteImageReader{view: v, ctx: ctx, captureID: captureID, info: info})
				if err != nil {
					return nil, err
				}
			}
			acquired[info.ID] = img
			actual, _ := img.Info()
			if actual != info {
				return nil, ttyapi.ErrImageInvalid
			}
		}
		placements = append(placements, ttyapi.PlacedImage{Placement: p, Resource: img})
	}
	capture, err := ttyapi.NewCapture(snapshot, placements)
	if err != nil {
		return nil, err
	}
	// Refresh the bounded attachment cache only after the entire capture validates.
	v.mu.Lock()
	select {
	case <-v.done:
		v.mu.Unlock()
		_ = capture.Close()
		return nil, ttyapi.ErrMountExpired
	default:
	}
	for _, img := range v.imageCache {
		_ = img.Close()
	}
	v.imageCache = acquired
	acquired = nil
	v.mu.Unlock()
	return capture, nil
}

type remoteImageReader struct {
	ctx       context.Context
	view      *remoteViewport
	pending   []byte
	info      ttyapi.ImageInfo
	captureID uint64
	offset    int
}

func (r *remoteImageReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if r.offset >= r.info.Bytes {
			return 0, io.EOF
		}
		reply, err := r.view.imageCall(r.ctx, wireFrame{Op: opImageChunk, CaptureID: r.captureID, ImageID: r.info.ID, Offset: r.offset})
		if err != nil {
			return 0, err
		}
		count := min(imageChunkBytes, r.info.Bytes-r.offset)
		if reply.CaptureID != r.captureID || reply.ImageID != r.info.ID || reply.Offset != r.offset || len(reply.Data) != count {
			return 0, ttyapi.ErrImageInvalid
		}
		r.pending = reply.Data
		r.offset += count
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
