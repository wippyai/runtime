// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}
func TestImageOwnershipAndBudget(t *testing.T) {
	data := testPNG(t)
	charge := int64(len(data) + 32)
	store := NewImageStore(charge)
	original, err := store.ImportPNG(data)
	require.NoError(t, err)
	same, err := store.ImportPNG(data)
	require.NoError(t, err)
	require.Equal(t, charge, store.Used())
	data[0] = 0 // Store owns its bytes.
	ref, err := original.Retain()
	require.NoError(t, err)
	require.NoError(t, original.Close())
	require.NoError(t, original.Close())
	_, err = original.Info()
	require.ErrorIs(t, err, ErrImageClosed)
	var b bytes.Buffer
	_, err = ref.WriteTo(&b)
	require.NoError(t, err)
	require.Equal(t, byte(137), b.Bytes()[0])
	require.NoError(t, ref.Close())
	require.Equal(t, charge, store.Used())
	require.NoError(t, same.Close())
	require.Zero(t, store.Used())
	_, err = NewImageStore(charge - 1).ImportPNG(b.Bytes())
	require.ErrorIs(t, err, ErrImageBudget)
}
func TestImageRejectsCorruptionAndCapturePinsResources(t *testing.T) {
	data := testPNG(t)
	store := NewImageStore(DefaultImageBudget)
	_, err := store.ImportPNG(data[:len(data)-8])
	require.ErrorIs(t, err, ErrImageInvalid)
	require.Zero(t, store.Used())
	img, err := store.ImportPNG(data)
	require.NoError(t, err)
	info, _ := img.Info()
	inputs := []PlacedImage{{Placement: Placement{ID: "chart", Destination: CellRect{Cols: 2, Rows: 2}}, Resource: img}}
	capture, err := NewCapture(Snapshot{Rows: []string{"old"}, Revision: 7}, inputs)
	require.NoError(t, err)
	require.NoError(t, img.Close())
	snapshot := capture.Snapshot()
	snapshot.Rows[0] = "tampered"
	snapshot.Images[0].ID = "tampered"
	require.Equal(t, "old", capture.Snapshot().Rows[0])
	require.Equal(t, "chart", capture.Snapshot().Images[0].ID)
	_, err = capture.Image("foreign")
	require.ErrorIs(t, err, ErrPermissionDenied)
	pinned, err := capture.Image(info.ID)
	require.NoError(t, err)
	require.NoError(t, capture.Close())
	require.NoError(t, capture.Close())
	got := make([]byte, info.Bytes)
	_, err = pinned.ReadAt(got, 0)
	require.NoError(t, err)
	require.Equal(t, data, got)
	_, err = pinned.ReadAt(got, int64(info.Bytes))
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, pinned.Close())
	require.Zero(t, store.Used())
}
func TestPlacementsValidateAtomically(t *testing.T) {
	store := NewImageStore(DefaultImageBudget)
	img, err := store.ImportPNG(testPNG(t))
	require.NoError(t, err)
	good := PlacedImage{Placement: Placement{ID: "p", Destination: CellRect{Cols: 1, Rows: 1}}, Resource: img}
	_, err = RetainPlacements([]PlacedImage{good, good})
	require.ErrorIs(t, err, ErrImageInvalid)
	bad := good
	bad.Placement.ID = "other"
	bad.Placement.Source = PixelRect{X: 1, Width: 2, Height: 1}
	_, err = RetainPlacements([]PlacedImage{good, bad})
	require.ErrorIs(t, err, ErrImageInvalid)
	require.NoError(t, img.Close())
	require.Zero(t, store.Used())
}
func TestImageConcurrentRetainClose(t *testing.T) {
	store := NewImageStore(DefaultImageBudget)
	img, err := store.ImportPNG(testPNG(t))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				ref, err := img.Retain()
				if err == nil {
					buf := make([]byte, 8)
					_, _ = ref.ReadAt(buf, 0)
					_ = ref.Close()
				}
			}
		})
	}
	_ = img.Close()
	wg.Wait()
	require.Zero(t, store.Used())
}

type blockedImageReader struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockedImageReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.reader.Read(p)
}
func TestReaderAdmissionPrecedesAllocationAndCloseCancelsImport(t *testing.T) {
	data := testPNG(t)
	info, err := inspectPNG(data)
	require.NoError(t, err)
	store := NewImageStore(imageCharge(info))
	reader := &blockedImageReader{reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		img, err := store.ImportPNGReader(info, reader)
		if img != nil {
			_ = img.Close()
		}
		done <- err
	}()
	<-reader.entered
	require.Equal(t, imageCharge(info), store.Used())
	_, err = store.ImportPNG(data)
	require.ErrorIs(t, err, ErrImageBudget)
	require.NoError(t, store.Close())
	close(reader.release)
	require.ErrorIs(t, <-done, ErrImageClosed)
	require.Zero(t, store.Used())
}
func TestReaderIdentityIsNotAuthorityAndFailureReleasesBudget(t *testing.T) {
	data := testPNG(t)
	store := NewImageStore(DefaultImageBudget)
	img, err := store.ImportPNG(data)
	require.NoError(t, err)
	info, _ := img.Info()
	before := store.Used()
	corrupt := bytes.Clone(data)
	corrupt[0] ^= 1
	_, err = store.ImportPNGReader(info, bytes.NewReader(corrupt))
	require.ErrorIs(t, err, ErrImageInvalid)
	require.Equal(t, before, store.Used())
	_, err = store.ImportPNGReader(info, bytes.NewReader(data[:5]))
	require.Error(t, err)
	require.Equal(t, before, store.Used())
	require.NoError(t, img.Close())
	require.Zero(t, store.Used())
}
