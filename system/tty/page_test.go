// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"image/color"
	"strconv"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

const (
	pageForeground = "#e0def4"
	pageBackground = "#191724"
)

func pageColors(t *testing.T) (color.Color, color.Color) {
	t.Helper()
	foreground, background := (ttyapi.Page{Foreground: pageForeground, Background: pageBackground}).Colors()
	return foreground, background
}

// rowCells decodes a rendered row back into styled cells without reusing the
// resolver's own composition helpers.
func rowCells(t *testing.T, row string, width int) uv.Line {
	t.Helper()
	screen := uv.NewScreenBuffer(width, 1)
	screen.Method = ansi.GraphemeWidth
	uv.NewStyledString(row).Draw(screen, screen.Bounds())
	return screen.Line(0)
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

func requirePageRow(t *testing.T, row string, width int, foreground, background color.Color) {
	t.Helper()
	require.Equal(t, width, ansi.StringWidth(row), "row %q must cover the viewport width", row)
	for index, cell := range rowCells(t, row, width) {
		// Zero-width cells are wide-glyph placeholders and carry no style.
		if cell.Width < 1 {
			continue
		}
		require.Truef(t, sameColor(cell.Style.Fg, foreground),
			"cell %d of %q must carry the page foreground", index, row)
		require.Truef(t, sameColor(cell.Style.Bg, background),
			"cell %d of %q must carry the page background", index, row)
	}
}

func producerSurface(t *testing.T, service *Service, view ttyapi.Viewport) ttyapi.Surface {
	t.Helper()
	ctx, frame, _ := processContextFor(t, service, "producer")
	t.Cleanup(func() { _ = frame.Close() })
	binding, err := service.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(ctx)
	require.NoError(t, err)
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	return surface
}

func TestViewportPageResolvesProducerDefaultCells(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	page := &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}
	view, err := createPageView(ctx, service, pageTestOptions{Width: 6, Height: 3, Page: page})
	require.NoError(t, err)
	surface := producerSurface(t, service, view)

	// An unstyled row, an explicit SGR reset, and a row the producer left short
	// of the viewport width all describe terminal-default cells.
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"ab", "\x1b[0m  ", ""}})
	require.NoError(t, err)

	foreground, background := pageColors(t)
	rows := view.Snapshot().Rows
	require.Len(t, rows, 3)
	for _, row := range rows {
		requirePageRow(t, row, 6, foreground, background)
	}
	require.Equal(t, "ab    ", ansi.Strip(rows[0]))
	require.Equal(t, page, view.(*viewport).session.page)
}

func TestViewportPagePreservesProducerColors(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 1,
		Page: &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}})
	require.NoError(t, err)
	surface := producerSurface(t, service, view)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"\x1b[38;2;255;0;0;48;2;0;0;255mR\x1b[0mx"}})
	require.NoError(t, err)

	foreground, background := pageColors(t)
	line := rowCells(t, view.Snapshot().Rows[0], 4)
	require.Len(t, line, 4)
	require.True(t, sameColor(line[0].Style.Fg, color.RGBA{R: 255, A: 255}),
		"an explicit producer foreground must survive composition")
	require.True(t, sameColor(line[0].Style.Bg, color.RGBA{B: 255, A: 255}),
		"an explicit producer background must survive composition")
	for index := 1; index < len(line); index++ {
		require.True(t, sameColor(line[index].Style.Fg, foreground))
		require.True(t, sameColor(line[index].Style.Bg, background))
	}
}

func TestViewportPageResolvesWideCellsAndClippedSequences(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := createPageView(ctx, service, pageTestOptions{Width: 6, Height: 2,
		Page: &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}})
	require.NoError(t, err)
	surface := producerSurface(t, service, view)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"世界", "ok\x1b[31"}})
	require.NoError(t, err)

	foreground, background := pageColors(t)
	rows := view.Snapshot().Rows
	require.Equal(t, 6, ansi.StringWidth(rows[0]))
	require.Equal(t, "世界  ", ansi.Strip(rows[0]))
	requirePageRow(t, rows[0], 6, foreground, background)
	require.NotContains(t, rows[1], "\x1b[31", "a clipped sequence must not reach a composited row")
	requirePageRow(t, rows[1], 6, foreground, background)
	require.Equal(t, "ok    ", ansi.Strip(rows[1]))
}

func TestViewportWithoutPagePublishesProducerRows(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := createPageView(ctx, service, pageTestOptions{Width: 6, Height: 3})
	require.NoError(t, err)
	require.Nil(t, view.(*viewport).session.page)
	surface := producerSurface(t, service, view)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"ab", "\x1b[0m  "}})
	require.NoError(t, err)
	require.Equal(t, []string{"ab", "\x1b[0m  "}, view.Snapshot().Rows)
}

func TestViewportSetPageBumpsRevisionAndWakesViewers(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 1})
	require.NoError(t, err)
	viewer, err := service.Attach(ctx, view.Handle())
	require.NoError(t, err)
	surface := producerSurface(t, service, view)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"ab"}})
	require.NoError(t, err)
	drain(view.Updates())
	drain(viewer.Updates())
	revision := view.Snapshot().Revision

	page := &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}
	require.NoError(t, view.(ttyapi.PageViewport).SetPage(ctx, page))
	require.Equal(t, revision+1, view.Snapshot().Revision,
		"a page change must invalidate the snapshot even when content is unchanged")
	require.Equal(t, ttyapi.Update{Revision: revision + 1}, <-view.Updates())
	require.Equal(t, ttyapi.Update{Revision: revision + 1}, <-viewer.Updates())

	foreground, background := pageColors(t)
	rows := viewer.Snapshot().Rows
	require.Equal(t, view.Snapshot().Rows, rows, "every viewer reads the same resolved rows")
	requirePageRow(t, rows[0], 4, foreground, background)

	require.NoError(t, view.(ttyapi.PageViewport).SetPage(ctx, nil))
	require.Nil(t, view.(*viewport).session.page)
	require.Equal(t, revision+2, view.Snapshot().Revision)
	require.Equal(t, []string{"ab"}, view.Snapshot().Rows)
}

func TestViewportRejectsIncompletePage(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	_, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 1,
		Page: &ttyapi.Page{Foreground: pageForeground}})
	require.Error(t, err)

	view, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 1})
	require.NoError(t, err)
	require.Error(t, view.(ttyapi.PageViewport).SetPage(ctx, &ttyapi.Page{Foreground: "#zzzzzz", Background: pageBackground}))
	require.Error(t, view.(ttyapi.PageViewport).SetPage(ctx, &ttyapi.Page{Background: pageBackground}))
	require.Nil(t, view.(*viewport).session.page)
}

func TestProducerSurfaceReportsLivePageColors(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()

	view, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 1})
	require.NoError(t, err)
	surface := producerSurface(t, service, view)
	paged, ok := surface.(ttyapi.PageProvider)
	require.True(t, ok, "a virtual surface must expose its viewport page to the producer")
	_, hasPage := paged.Page()
	require.False(t, hasPage)

	require.NoError(t, view.(ttyapi.PageViewport).SetPage(ctx, &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}))
	foreground, background := pageColors(t)
	page, hasPage := paged.Page()
	resolvedForeground, resolvedBackground := page.Colors()
	require.True(t, hasPage)
	require.True(t, sameColor(resolvedForeground, foreground))
	require.True(t, sameColor(resolvedBackground, background))
}

func drain(updates <-chan ttyapi.Update) {
	for {
		select {
		case <-updates:
		default:
			return
		}
	}
}

// Keep the existing Service.Create API compatible for embedders.
type pageTestOptions struct {
	Page          *ttyapi.Page
	Width, Height int
}

func createPageView(ctx context.Context, service *Service, options pageTestOptions) (ttyapi.Viewport, error) {
	view, err := service.Create(ctx, options.Width, options.Height)
	if err != nil {
		return nil, err
	}
	if err := view.(ttyapi.PageViewport).SetPage(ctx, options.Page); err != nil {
		_ = view.Close()
		return nil, err
	}
	return view, nil
}

func TestPageOwnerAuthorityResizeAndCachedSnapshots(t *testing.T) {
	service := NewService()
	defer service.Close()
	ctx, frame, _ := processContext(t, service)
	defer frame.Close()
	view, err := createPageView(ctx, service, pageTestOptions{Width: 4, Height: 2, Page: &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}})
	require.NoError(t, err)
	require.Len(t, view.Snapshot().Rows, 2, "page exists before first producer frame")
	surface := producerSurface(t, service, view)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"a", "b"}})
	require.NoError(t, err)
	before := view.Snapshot()
	require.NoError(t, view.(ttyapi.PageViewport).SetPage(ctx, &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}))
	require.Equal(t, before.Revision, view.Snapshot().Revision, "same page is a no-op")
	other, otherFrame, _ := processContextFor(t, service, "other")
	defer otherFrame.Close()
	require.ErrorIs(t, view.(ttyapi.PageViewport).SetPage(other, nil), ttyapi.ErrPermissionDenied)
	attached, err := service.Attach(ctx, view.Handle())
	require.NoError(t, err)
	require.NoError(t, attached.(ttyapi.PageViewport).SetPage(ctx, &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}), "the creator can recover page control after reattaching")
	outsider, err := service.Attach(other, view.Handle())
	require.NoError(t, err)
	require.ErrorIs(t, outsider.(ttyapi.PageViewport).SetPage(other, nil), ttyapi.ErrPermissionDenied)
	require.NoError(t, view.Resize(2, 3))
	foreground, background := pageColors(t)
	for _, row := range view.Snapshot().Rows {
		requirePageRow(t, row, 2, foreground, background)
	}
	require.Equal(t, "a   ", ansi.Strip(before.Rows[0]), "retained snapshots stay immutable")
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"a"}})
	require.NoError(t, err)
	require.Equal(t, "  ", ansi.Strip(view.Snapshot().Rows[1]), "omitted row clears stale content")
	require.Zero(t, testing.AllocsPerRun(100, func() { _ = view.Snapshot() }))
	require.NoError(t, view.Close())
	require.ErrorIs(t, view.(ttyapi.PageViewport).SetPage(ctx, nil), ttyapi.ErrViewportClosed)
}

func TestMeshPageChangePublishesResolvedRows(t *testing.T) {
	a, b, _, _ := meshFixture(t)
	owner, _ := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 4, 2)
	require.NoError(t, err)
	require.NoError(t, view.(ttyapi.PageViewport).SetPage(owner, &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}))
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
	require.NoError(t, err)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	require.Equal(t, view.Snapshot().Rows, remote.Snapshot().Rows)
	_, canSet := remote.(ttyapi.PageViewport)
	require.False(t, canSet, "mesh mounts cannot mutate owner page policy")
	revision := remote.Snapshot().Revision
	require.NoError(t, view.(ttyapi.PageViewport).SetPage(owner, &ttyapi.Page{Foreground: "#102030", Background: "#f0e0d0"}))
	require.Eventually(t, func() bool { return remote.Snapshot().Revision > revision }, time.Second, time.Millisecond)
	require.Equal(t, view.Snapshot().Rows, remote.Snapshot().Rows)
}

func BenchmarkPagePresent(b *testing.B) {
	for _, mode := range []string{"unchanged", "one-row", "all-rows"} {
		b.Run(mode, func(b *testing.B) {
			service := NewService()
			defer service.Close()
			ctx, frame, _ := processContext(b, service)
			defer frame.Close()
			view, err := createPageView(ctx, service, pageTestOptions{Width: 120, Height: 40, Page: &ttyapi.Page{Foreground: pageForeground, Background: pageBackground}})
			require.NoError(b, err)
			binding, err := service.Binding(view.Grant())
			require.NoError(b, err)
			port, err := binding.Resolve(ctx)
			require.NoError(b, err)
			surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
			require.NoError(b, err)
			rows := make([]string, 40)
			for y := range rows {
				rows[y] = strings.Repeat("content ", 15)
			}
			_, err = surface.Present(ttyapi.Frame{Rows: rows})
			require.NoError(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if mode != "unchanged" {
					limit := 1
					if mode == "all-rows" {
						limit = len(rows)
					}
					for y := 0; y < limit; y++ {
						rows[y] = strings.Repeat("content ", 14) + strconv.Itoa(n%2)
					}
				}
				_, _ = surface.Present(ttyapi.Frame{Rows: rows})
			}
		})
	}
}
