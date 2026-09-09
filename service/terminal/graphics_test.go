// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func graphicFixture(t *testing.T) (*ttyapi.ImageStore, *ttyapi.Image) {
	t.Helper()
	var data bytes.Buffer
	rgba := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			rgba.Set(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 4), B: uint8(x * y), A: 255})
		}
	}
	require.NoError(t, png.Encode(&data, rgba))
	store := ttyapi.NewImageStore(ttyapi.DefaultImageBudget)
	img, err := store.ImportPNG(data.Bytes())
	require.NoError(t, err)
	return store, img
}
func TestGraphicsTransmitOnceMoveDeleteAndRestoreCursor(t *testing.T) {
	store, img := graphicFixture(t)
	defer img.Close()
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{Synchronized: true})
	s.probe = func() ttyapi.SurfaceCapabilities { return ttyapi.SurfaceCapabilities{Images: "kitty"} }
	frame := ttyapi.Frame{Rows: []string{strings.Repeat(" ", 40), strings.Repeat(" ", 40)}, Cursor: &ttyapi.Cursor{Column: 1, Row: 0}, Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "chart", Destination: ttyapi.CellRect{Cols: 4, Rows: 2}}}}}
	_, err := s.Present(frame)
	require.NoError(t, err)
	first := out.String()
	require.Contains(t, first, "a=t,f=100")
	require.Contains(t, first, "a=p,")
	require.True(t, strings.HasPrefix(first, "\x1b[?2026h"))
	require.True(t, strings.HasSuffix(first, "\x1b[1;2H\x1b[?25l\x1b[?2026l"))
	for _, part := range strings.Split(first, "\x1b_G")[1:] {
		end := strings.Index(part, "\x1b\\")
		if end < 0 {
			continue
		}
		payload := part[:end]
		if pos := strings.Index(payload, ";"); pos >= 0 {
			require.LessOrEqual(t, len(payload[pos+1:]), 4096)
		}
	}
	out.Reset()
	stats, err := s.Present(frame)
	require.NoError(t, err)
	require.Zero(t, stats.Bytes)
	require.Zero(t, out.Len())
	frame.Images[0].Placement.Destination.X = 5
	out.Reset()
	stats, err = s.Present(frame)
	require.NoError(t, err)
	require.Zero(t, stats.ChangedRows)
	require.NotContains(t, out.String(), "a=t,")
	require.Contains(t, out.String(), "a=p,")
	out.Reset()
	s.Invalidate()
	_, err = s.Present(frame)
	require.NoError(t, err)
	require.Contains(t, out.String(), "a=t,")
	out.Reset()
	_, err = s.Present(ttyapi.Frame{Rows: frame.Rows})
	require.NoError(t, err)
	require.Contains(t, out.String(), "a=d,d=I,")
	require.NoError(t, s.Close())
	require.NoError(t, img.Close())
	require.Zero(t, store.Used())
}
func TestGraphicsFallbackDoesNotEmitImageOrAltEscapes(t *testing.T) {
	_, img := graphicFixture(t)
	defer img.Close()
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{})
	frame := ttyapi.Frame{Rows: []string{strings.Repeat(" ", 40)}, Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "chart", Alt: "safe\x1b[2J\nlabel", Destination: ttyapi.CellRect{Cols: 30, Rows: 1}}}}}
	_, err := s.Present(frame)
	require.NoError(t, err)
	require.Contains(t, out.String(), "[image: safelabel]")
	require.NotContains(t, out.String(), "\x1b_G")
	require.NotContains(t, out.String(), "\x1b[2J")
	out.Reset()
	stats, err := s.Present(frame)
	require.NoError(t, err)
	require.Zero(t, stats.Bytes)
}
func TestGraphicsProbeUsesExistingReaderAndTimesOut(t *testing.T) {
	var out bytes.Buffer
	r := &InputReader{started: true, output: &out}
	require.Equal(t, "pending", r.ProbeGraphics().Images)
	require.Contains(t, out.String(), "a=q,")
	size := out.Len()
	require.Equal(t, "pending", r.ProbeGraphics().Images)
	require.Equal(t, size, out.Len())
	r.graphicsReply(graphicsProbeID, []byte("OK"))
	require.Equal(t, "kitty", r.ProbeGraphics().Images)
	r = &InputReader{started: true, output: &out, graphicsProbeAt: time.Now().Add(-time.Second)}
	require.Equal(t, "none", r.ProbeGraphics().Images)
	r.graphicsReply(graphicsProbeID, []byte("OK"))
	require.Equal(t, "none", r.ProbeGraphics().Images, "late reply cannot enable unsupported mode")
}

type partialGraphicsWriter struct {
	output bytes.Buffer
	fail   bool
}

func (w *partialGraphicsWriter) Write(p []byte) (int, error) {
	if w.fail {
		n := len(p) / 2
		_, _ = w.output.Write(p[:n])
		return n, nil
	}
	return w.output.Write(p)
}
func TestGraphicsPartialWriteInvalidatesUploadsAndCleansOnRetry(t *testing.T) {
	_, img := graphicFixture(t)
	defer img.Close()
	writer := &partialGraphicsWriter{fail: true}
	s := NewSurface(writer, ttyapi.SurfaceOptions{Synchronized: true})
	s.probe = func() ttyapi.SurfaceCapabilities { return ttyapi.SurfaceCapabilities{Images: "kitty"} }
	f := ttyapi.Frame{Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "p", Destination: ttyapi.CellRect{Cols: 2, Rows: 2}}}}}
	_, err := s.Present(f)
	require.Error(t, err)
	writer.fail = false
	writer.output.Reset()
	_, err = s.Present(f)
	require.NoError(t, err)
	wire := writer.output.String()
	require.Contains(t, wire, "a=d,d=I,")
	require.Contains(t, wire, "a=t,")
	require.Less(t, strings.Index(wire, "a=d,d=I,"), strings.Index(wire, "a=t,"))
	require.NoError(t, s.Close())
}

func TestGraphicsFallbackPaintsImageOnlyFramesWithinHostBounds(t *testing.T) {
	_, img := graphicFixture(t)
	defer img.Close()
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{})
	s.size = func() (int, int, error) { return 20, 10, nil }
	_, err := s.Present(ttyapi.Frame{Images: []ttyapi.PlacedImage{{Resource: img, Placement: ttyapi.Placement{ID: "p", Destination: ttyapi.CellRect{X: 1, Y: 2, Cols: 15, Rows: 2}}}}})
	require.NoError(t, err)
	require.Contains(t, out.String(), "[image]")
	require.NotContains(t, out.String(), "\x1b_G")
	require.Contains(t, out.String(), "\x1b[3;1H")
	require.NoError(t, s.Close())
	require.Nil(t, s.scratch)
}
