// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestInputReaderDoesNotEnableMouseWhileStopped(t *testing.T) {
	var output bytes.Buffer
	reader := NewInputReader(os.Stdin, &output, NewRawManager(os.Stdin), nil, pid.PID{})
	reader.EnableMouse()
	require.Empty(t, output.String())
	require.False(t, reader.mouseEnabled)
}

func TestInputReaderStopSerializesTerminalModeCleanup(t *testing.T) {
	var output lockedBuffer
	reader := NewInputReader(os.Stdin, &output, NewRawManager(os.Stdin), nil, pid.PID{})
	reader.started = true
	reader.mouseEnabled = true
	reader.pasteEnabled = true

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1000 {
			reader.EnableMouse()
			reader.DisableMouse()
		}
	}()
	go func() {
		defer wg.Done()
		require.NoError(t, reader.Stop())
	}()
	wg.Wait()
	require.False(t, reader.started)
	require.False(t, reader.mouseEnabled)
	require.False(t, reader.pasteEnabled)
}

func TestConcurrentInputStopsJoinTheSameCleanup(t *testing.T) {
	reader := NewInputReader(os.Stdin, io.Discard, NewRawManager(os.Stdin), nil, pid.PID{})
	reader.started = true
	reader.wg.Add(1)
	firstDone := make(chan error, 1)
	go func() { firstDone <- reader.Stop() }()
	require.Eventually(t, func() bool {
		reader.mu.Lock()
		defer reader.mu.Unlock()
		return reader.stopping
	}, time.Second, time.Millisecond)

	secondDone := make(chan error, 1)
	go func() { secondDone <- reader.Stop() }()
	select {
	case <-secondDone:
		t.Fatal("concurrent Stop returned before terminal cleanup completed")
	case <-time.After(20 * time.Millisecond):
	}

	reader.wg.Done()
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
}

type lockedBuffer struct {
	bytes.Buffer
	mu sync.Mutex
}

func (b *lockedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(payload)
}
