// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// pagedTestSurface reports the page at the moment the producer's reply is
// composed, the way the viewport broker does.
type pagedTestSurface struct {
	colors func() (color.Color, color.Color, bool)
	testSurface
}

func (s *pagedTestSurface) Page() (ttyapi.Page, bool) {
	fg, bg, ok := s.colors()
	if !ok {
		return ttyapi.Page{}, false
	}
	hex := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return ttyapi.Page{Foreground: hex(fg), Background: hex(bg)}, true
}

func runQueryProxy(t *testing.T, surface ttyapi.Surface, queries string, replies int) []string {
	t.Helper()
	process := &testProcess{
		stdout: io.NopCloser(strings.NewReader(queries)),
		input:  make(chan []byte, replies),
	}
	proxy, err := New(process, surface, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()
	answers := make([]string, 0, replies)
	for len(answers) < replies {
		select {
		case reply := <-process.input:
			answers = append(answers, string(reply))
		case <-time.After(time.Second):
			t.Fatal("terminal color query was never answered")
		}
	}
	require.NoError(t, <-done)
	return answers
}

func TestProxyAnswersColorQueriesWithViewportPage(t *testing.T) {
	surface := &pagedTestSurface{colors: func() (color.Color, color.Color, bool) {
		return color.RGBA{R: 0xe0, G: 0xde, B: 0xf4, A: 0xff},
			color.RGBA{R: 0x19, G: 0x17, B: 0x24, A: 0xff}, true
	}}
	replies := runQueryProxy(t, surface, "\x1b]11;?\x07\x1b]10;?\x07", 2)
	require.Equal(t, "\x1b]11;rgb:1919/1717/2424\x07", replies[0])
	require.Equal(t, "\x1b]10;rgb:e0e0/dede/f4f4\x07", replies[1])
}

func TestProxyWithoutPageKeepsEmulatorDefaultColors(t *testing.T) {
	replies := runQueryProxy(t, &testSurface{}, "\x1b]11;?\x07", 1)
	require.Equal(t, "\x1b]11;rgb:0000/0000/0000\x07", replies[0])

	unset := &pagedTestSurface{colors: func() (color.Color, color.Color, bool) { return nil, nil, false }}
	replies = runQueryProxy(t, unset, "\x1b]11;?\x07", 1)
	require.Equal(t, "\x1b]11;rgb:0000/0000/0000\x07", replies[0],
		"a viewport without a page keeps the emulator's own default colors")
}

func TestProxyColorQueryNeverAnswersWithSupersededPage(t *testing.T) {
	var mu sync.Mutex
	background := color.RGBA{R: 0x19, G: 0x17, B: 0x24, A: 0xff}
	surface := &pagedTestSurface{}
	surface.colors = func() (color.Color, color.Color, bool) {
		mu.Lock()
		defer mu.Unlock()
		current := background
		// The desktop theme changes while the query is in flight.
		background = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		return color.RGBA{A: 0xff}, current, true
	}
	replies := runQueryProxy(t, surface, "\x1b]11;?\x07\x1b]11;?\x07", 2)
	require.Equal(t, "\x1b]11;rgb:1919/1717/2424\x07", replies[0])
	require.Equal(t, "\x1b]11;rgb:ffff/ffff/ffff\x07", replies[1],
		"a reply must carry the page in effect when it is written")
}
