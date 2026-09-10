// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestClipboardIsExplicitAndNotReplayedByPresent(t *testing.T) {
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{Synchronized: true})
	_, err := s.Present(ttyapi.Frame{Rows: []string{"frame"}})
	require.NoError(t, err)
	out.Reset()
	copy, ok := any(s).(interface{ Clipboard(string) error })
	require.True(t, ok, "physical surface must expose explicit clipboard output")
	text := "hello\n界\x1b]52;c;untrusted\a"
	require.NoError(t, copy.Clipboard(text))
	require.Equal(t, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte(text))+"\x07", out.String())
	out.Reset()
	s.Invalidate()
	_, err = s.Present(ttyapi.Frame{Rows: []string{"frame"}})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "]52;")
	require.NoError(t, s.Close())
	out.Reset()
	require.Error(t, copy.Clipboard("stale"))
	require.Empty(t, out.String())
}

func TestClipboardRejectsBoundsAndInvalidUTF8BeforeWriting(t *testing.T) {
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{})
	copy, ok := any(s).(interface{ Clipboard(string) error })
	require.True(t, ok)
	require.Error(t, copy.Clipboard(strings.Repeat("x", 65537)))
	require.Error(t, copy.Clipboard(string([]byte{0xff})))
	require.Empty(t, out.String())
	require.NoError(t, copy.Clipboard(strings.Repeat("x", 65536)))
}

type clipboardShortWriter struct{}

func (clipboardShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestClipboardShortWriteIsNotSuccess(t *testing.T) {
	s := NewSurface(clipboardShortWriter{}, ttyapi.SurfaceOptions{})
	require.ErrorIs(t, s.Clipboard("text"), io.ErrShortWrite)
}

func TestClipboardAndFramesAreSerialized(t *testing.T) {
	var out bytes.Buffer
	s := NewSurface(&out, ttyapi.SurfaceOptions{Synchronized: true})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); assert.NoError(t, s.Clipboard("text")) }()
		go func() {
			defer wg.Done()
			_, err := s.Present(ttyapi.Frame{Rows: []string{"frame"}})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	request := "\x1b]52;c;dGV4dA==\x07"
	require.Equal(t, 40, strings.Count(out.String(), request))
	stripped := strings.ReplaceAll(out.String(), request, "")
	require.NotContains(t, stripped, "]52;")
	require.Equal(t, strings.Count(stripped, "\x1b[?2026h"), strings.Count(stripped, "\x1b[?2026l"))
}
