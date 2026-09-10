// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func fixtureImage(t testing.TB, s *Service) *ttyapi.Image {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 8, 8))))
	img, err := s.ImageStore().ImportPNG(data.Bytes())
	require.NoError(t, err)
	return img
}
func TestImagePublicationCaptureAndCleanup(t *testing.T) {
	s := NewService()
	defer s.Close()
	ctx, frame, _ := processContext(t, s)
	defer frame.Close()
	v, err := s.Create(ctx, 20, 10)
	require.NoError(t, err)
	binding, err := s.Binding(v.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	output, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	img := fixtureImage(t, s)
	item := ttyapi.PlacedImage{Resource: img, Placement: ttyapi.Placement{ID: "chart", Destination: ttyapi.CellRect{Cols: 4, Rows: 2}}}
	f := ttyapi.Frame{Rows: []string{"text"}, Images: []ttyapi.PlacedImage{item}}
	_, err = output.Present(f)
	require.NoError(t, err)
	before := v.Snapshot()
	require.Len(t, before.Images, 1)
	before.Images[0].ID = "tampered"
	require.Equal(t, "chart", v.Snapshot().Images[0].ID, "snapshot metadata must not alias broker state")
	stats, err := output.Present(f)
	require.NoError(t, err)
	require.Zero(t, stats.ChangedRows)
	require.Equal(t, before.Revision, v.Snapshot().Revision)
	f.Images[0].Placement.Destination.X = 3
	stats, err = output.Present(f)
	require.NoError(t, err)
	require.Zero(t, stats.ChangedRows)
	require.Greater(t, v.Snapshot().Revision, before.Revision)
	require.Zero(t, before.Images[0].Destination.X, "old metadata remains immutable")
	capture, err := v.(ttyapi.CaptureViewport).Capture(ctx)
	require.NoError(t, err)
	f.Images[0].Placement.Source = ttyapi.PixelRect{Width: 999, Height: 1}
	_, err = output.Present(f)
	require.ErrorIs(t, err, ttyapi.ErrImageInvalid)
	require.Equal(t, capture.Snapshot().Revision, v.Snapshot().Revision, "failed present is atomic")
	require.NoError(t, img.Close())
	_, err = output.Present(ttyapi.Frame{Rows: []string{"text"}})
	require.NoError(t, err)
	require.Empty(t, v.Snapshot().Images)
	require.Positive(t, s.ImageStore().Used(), "capture pins deleted image")
	require.NoError(t, capture.Close())
	require.Zero(t, s.ImageStore().Used())
	require.NoError(t, port.Close())
	require.NoError(t, v.Close())
}
func TestImageCaptureRequiresOwnerAndObservation(t *testing.T) {
	s := NewService()
	defer s.Close()
	ctx, f, _ := processContextFor(t, s, "owner")
	defer f.Close()
	stranger, sf, _ := processContextFor(t, s, "other")
	defer sf.Close()
	v, err := s.Create(ctx, 20, 10)
	require.NoError(t, err)
	_, err = v.(ttyapi.CaptureViewport).Capture(stranger)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	require.NoError(t, v.Close())
	_, err = v.(ttyapi.CaptureViewport).Capture(ctx)
	require.ErrorIs(t, err, ttyapi.ErrViewportClosed)
}
func TestLastOwnerReleasesPublishedImages(t *testing.T) {
	s := NewService()
	defer s.Close()
	ctx, f, _ := processContext(t, s)
	defer f.Close()
	v, err := s.Create(ctx, 20, 10)
	require.NoError(t, err)
	binding, err := s.Binding(v.Grant())
	require.NoError(t, err)
	p, err := binding.Resolve(ctx)
	require.NoError(t, err)
	output, err := p.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	img := fixtureImage(t, s)
	_, err = output.Present(ttyapi.Frame{Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "p", Destination: ttyapi.CellRect{Cols: 1, Rows: 1}}}}})
	require.NoError(t, err)
	require.NoError(t, img.Close())
	require.NoError(t, p.Close())
	require.Positive(t, s.ImageStore().Used())
	require.NoError(t, v.Close())
	require.Zero(t, s.ImageStore().Used())
}
