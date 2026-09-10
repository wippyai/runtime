// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/input"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	tty "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

func TestNewEventInputReader_ConstructorAndDefaults(t *testing.T) {
	// Defaults when nil values passed
	reader := NewEventInputReader(nil, nil, nil, nil)
	require.NotNil(t, reader)
	require.NotNil(t, reader.output)
	require.NotNil(t, reader.sink)
	require.NotNil(t, reader.Done())
	require.NoError(t, reader.Err())

	// Non-nil stdin should initialize default RawManager if raw was nil
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	readerWithStdin := NewEventInputReader(r, nil, nil, nil)
	require.NotNil(t, readerWithStdin.raw)
}

func TestNewEventInputReader_DirectSinkDelivery(t *testing.T) {
	var delivered []tty.Event
	var mu sync.Mutex

	sink := func(ev tty.Event) {
		mu.Lock()
		delivered = append(delivered, ev)
		mu.Unlock()
	}

	reader := NewEventInputReader(nil, io.Discard, nil, sink)
	reader.emitter = newInputEmitter(reader.deliverToSink)

	reader.sendEvent(&tty.Event{
		Type:   "key",
		Key:    "a",
		Action: "press",
	})
	reader.sendEvent(&tty.Event{
		Type:   "mouse",
		Action: "press",
		X:      10,
		Y:      20,
	})

	mu.Lock()
	require.Len(t, delivered, 2)
	assert.Equal(t, "key", delivered[0].Type)
	assert.Equal(t, "a", delivered[0].Key)
	assert.Equal(t, "mouse", delivered[1].Type)
	assert.Equal(t, 10, delivered[1].X)
	assert.Equal(t, 20, delivered[1].Y)
	mu.Unlock()
}

func TestInputReaderFramesFragmentedWheelBurstBeforeFollowingKey(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	require.NoError(t, pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}))

	events := make(chan tty.Event, 64)
	reader := NewEventInputReader(slave, io.Discard, NewRawManager(slave), func(event tty.Event) {
		events <- event
	})
	require.NoError(t, reader.Start())
	t.Cleanup(func() { require.NoError(t, reader.Stop()) })

	// The burst is deliberately larger than x/input's 256-byte read buffer,
	// leaving one SGR sequence split across reads. Trackpads commonly produce
	// this shape; a wheel produces the identical SGR records individually.
	var burst strings.Builder
	for range 12 {
		burst.WriteString("\x1b[<64;5;5M")
	}
	for range 40 {
		burst.WriteString("\x1b[<65;5;5M")
	}
	_, err = io.WriteString(master, burst.String()+"z")
	require.NoError(t, err)

	var received []tty.Event
	require.Eventually(t, func() bool {
		for {
			select {
			case event := <-events:
				if event.Type != "start" {
					received = append(received, event)
				}
			default:
				return len(received) >= 53
			}
		}
	}, time.Second, time.Millisecond)

	var wheelUp, wheelDown int
	var keys []string
	for _, event := range received {
		switch event.Type {
		case "mouse":
			require.Equal(t, "wheel", event.Action)
			switch event.Button {
			case "wheel_up":
				wheelUp++
			case "wheel_down":
				wheelDown++
			default:
				t.Fatalf("unexpected mouse button %q", event.Button)
			}
		case "key":
			keys = append(keys, event.Key)
		default:
			t.Fatalf("unexpected event %#v", event)
		}
	}
	require.Equal(t, 12, wheelUp)
	require.Equal(t, 40, wheelDown)
	require.Equal(t, []string{"z"}, keys)
}

func TestInputReaderPreservesEscapeTimeoutAndFragmentedNavigation(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	events := make(chan tty.Event, 4)
	reader := NewEventInputReader(slave, io.Discard, NewRawManager(slave), func(event tty.Event) {
		if event.Type == "key" {
			events <- event
		}
	})
	require.NoError(t, reader.Start())
	t.Cleanup(func() { require.NoError(t, reader.Stop()) })

	_, err = master.Write([]byte("\x1b"))
	require.NoError(t, err)
	select {
	case event := <-events:
		require.Equal(t, "esc", event.KeyType)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("standalone escape did not honor the decoder timeout")
	}

	_, err = master.Write([]byte("\x1b["))
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = master.Write([]byte("A"))
	require.NoError(t, err)
	select {
	case event := <-events:
		require.Equal(t, "up", event.KeyType)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fragmented navigation sequence was not decoded")
	}
}

func TestInputReaderStopDrainsPartialEscape(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	reader := NewEventInputReader(slave, io.Discard, NewRawManager(slave), func(tty.Event) {})
	require.NoError(t, reader.Start())
	_, err = master.Write([]byte("\x1b["))
	require.NoError(t, err)

	stopped := make(chan error, 1)
	go func() { stopped <- reader.Stop() }()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop blocked while the decoder held a partial escape")
	}
}

func TestInputReaderDeliversDataReturnedWithEOF(t *testing.T) {
	input := &eofWithDataReader{data: []byte("z")}
	events := make(chan tty.Event, 1)
	reader := NewEventInputReader(nil, io.Discard, nil, func(event tty.Event) { events <- event })
	reader.started = true
	reader.reader = input
	reader.emitter = newInputEmitter(reader.deliverToSink)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reader.wg.Add(1)
	go reader.readLoop(ctx, input, reader.Done())

	select {
	case event := <-events:
		require.Equal(t, "key", event.Type)
		require.Equal(t, "z", event.Key)
	case <-time.After(time.Second):
		t.Fatal("data returned with EOF was not delivered")
	}
	select {
	case <-reader.Done():
	case <-time.After(time.Second):
		t.Fatal("reader did not complete after EOF")
	}
	require.ErrorIs(t, reader.Err(), io.EOF)
}

func TestInputReaderBoundsUnterminatedBracketedPaste(t *testing.T) {
	// Thousands of already-decoded keys make the event acknowledgements drain
	// the preceding read. The unterminated paste that follows must still reach
	// the framing limit instead of growing indefinitely.
	input := &unterminatedPasteReader{prefix: []byte(strings.Repeat("z", terminalInputReadSize-6) + "\x1b[200~")}
	reader := NewEventInputReader(nil, io.Discard, nil, func(tty.Event) {})
	reader.started = true
	reader.reader = input
	reader.emitter = newInputEmitter(reader.deliverToSink)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reader.wg.Add(1)
	go reader.readLoop(ctx, input, reader.Done())

	select {
	case <-reader.Done():
	case <-time.After(time.Second):
		t.Fatal("unterminated paste did not reach the frame limit")
	}
	require.ErrorIs(t, reader.Err(), ErrInputFrameTooLarge)
}

type eofWithDataReader struct {
	data   []byte
	offset int
}

func (r *eofWithDataReader) Read(p []byte) (int, error) {
	if r.offset == len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	if r.offset == len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

func (r *eofWithDataReader) Cancel() bool { return true }

func (r *eofWithDataReader) Close() error { return nil }

type unterminatedPasteReader struct {
	prefix []byte
	offset int
}

func (r *unterminatedPasteReader) Read(p []byte) (int, error) {
	if r.offset < len(r.prefix) {
		n := copy(p, r.prefix[r.offset:])
		r.offset += n
		return n, nil
	}
	for index := range p {
		p[index] = 'x'
	}
	return len(p), nil
}

func (r *unterminatedPasteReader) Cancel() bool { return true }

func (r *unterminatedPasteReader) Close() error { return nil }

func TestNewInputReader_SchedulerAdapterCompatibility(t *testing.T) {
	targetPID := pid.PID{Host: "node1", UniqID: "proc123"}
	registry := &mockCommandRegistry{}
	sched := actor.NewScheduler(registry, actor.WithWorkers(1))
	sched.Start()
	defer sched.Stop(context.Background())

	adapterReader := NewInputReader(nil, io.Discard, nil, sched, targetPID)
	require.NotNil(t, adapterReader.sink)

	testEvent := tty.Event{Type: "key", Key: "enter"}
	// Calling sink sends via sched.Send(pkg); does not panic even if proc not found
	require.NotPanics(t, func() {
		adapterReader.sink(testEvent)
	})

	// Test nil scheduler does not panic
	nilSchedReader := NewInputReader(nil, io.Discard, nil, nil, targetPID)
	require.NotPanics(t, func() {
		nilSchedReader.sink(testEvent)
	})
}

func TestInputReader_PTY_StartStopLifecycle_DoneAndErr(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()

	var events []tty.Event
	var mu sync.Mutex
	sink := func(ev tty.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	var output bytes.Buffer
	raw := NewRawManager(slave)
	reader := NewEventInputReader(slave, &output, raw, sink)

	doneCh := reader.Done()
	require.NotNil(t, doneCh)

	select {
	case <-doneCh:
		t.Fatal("Done() must not be closed before Start()")
	default:
	}

	require.NoError(t, reader.Start())
	require.True(t, raw.Enabled())

	// Initial start event should be emitted
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) >= 1 && events[0].Type == "start"
	}, time.Second, 10*time.Millisecond)

	// Clean stop
	require.NoError(t, reader.Stop())
	require.False(t, raw.Enabled())

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("Done() did not close after Stop()")
	}

	require.NoError(t, reader.Err())

	// Stop is idempotent
	require.NoError(t, reader.Stop())
	require.NoError(t, reader.Err())
}

func TestInputReader_PTY_EOF_TeardownAndNotification(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer slave.Close()

	var events []tty.Event
	var mu sync.Mutex
	sink := func(ev tty.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	var output bytes.Buffer
	raw := NewRawManager(slave)
	reader := NewEventInputReader(slave, &output, raw, sink)

	require.NoError(t, reader.Start())
	require.True(t, raw.Enabled())

	// Write input to master and close it to trigger EOF on slave
	_, err = master.Write([]byte("x"))
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	// Close master to cause EOF on slave
	require.NoError(t, master.Close())

	// Reader should detect EOF, clean up terminal, and close Done() without hanging
	select {
	case <-reader.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Done() on EOF")
	}

	// Preserve the actual platform read error (EOF or a PTY I/O error).
	require.Error(t, reader.Err())

	// Raw mode should be disabled automatically
	require.False(t, raw.Enabled())

	// A closed PTY may also fail terminal restoration. Preserve that failure
	// through Err rather than requiring successful I/O on the closed peer.
	if stopErr := reader.Stop(); stopErr != nil {
		require.ErrorIs(t, reader.Err(), stopErr)
	}
}

func TestInputReader_PTY_RestartLifecycle(t *testing.T) {
	master1, slave1, err := pty.Open()
	require.NoError(t, err)
	defer master1.Close()
	defer slave1.Close()

	var events []tty.Event
	var mu sync.Mutex
	sink := func(ev tty.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	raw1 := NewRawManager(slave1)
	reader := NewEventInputReader(slave1, io.Discard, raw1, sink)

	// Session 1
	require.NoError(t, reader.Start())
	done1 := reader.Done()
	require.NoError(t, reader.Stop())

	select {
	case <-done1:
	case <-time.After(time.Second):
		t.Fatal("done1 did not close")
	}
	require.NoError(t, reader.Err())

	// Session 2 (restart on new pty)
	master2, slave2, err := pty.Open()
	require.NoError(t, err)
	defer master2.Close()
	defer slave2.Close()

	reader.stdin = slave2
	reader.raw = NewRawManager(slave2)

	require.NoError(t, reader.Start())
	done2 := reader.Done()
	require.NotEqual(t, done1, done2, "Done() must return a fresh channel on restart")

	select {
	case <-done2:
		t.Fatal("done2 must be open while active")
	default:
	}
	require.NoError(t, reader.Err())

	require.NoError(t, reader.Stop())

	select {
	case <-done2:
	case <-time.After(time.Second):
		t.Fatal("done2 did not close after Stop()")
	}
	require.NoError(t, reader.Err())
}

func TestInputReader_ConcurrentStopCallers_ActiveStart(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()

	raw := NewRawManager(slave)
	reader := NewEventInputReader(slave, io.Discard, raw, func(tty.Event) {})

	require.NoError(t, reader.Start())
	doneCh := reader.Done()

	const concurrency = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = reader.Stop()
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "Stop caller %d failed", i)
	}

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("Done() did not close")
	}
	require.False(t, raw.Enabled())
}

func TestInputReader_StartFailureCleanup(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()

	failure := errors.New("write failure")
	raw := NewRawManager(slave)
	reader := NewEventInputReader(slave, &failingWriter{err: failure}, raw, nil)
	defer reader.Stop()

	// Fail after raw mode and the cancelable input reader have been acquired.
	require.ErrorIs(t, reader.Start(), failure)
	require.False(t, raw.Enabled())
	require.False(t, reader.started)
	require.False(t, reader.stopping)
	require.Nil(t, reader.cancel)
	require.Nil(t, reader.reader)
	require.Nil(t, reader.emitter)

	// The failed start must not poison a later attempt on the same terminal.
	reader.output = io.Discard
	require.NoError(t, reader.Start())
	require.NoError(t, reader.Stop())
	require.False(t, raw.Enabled())
}

type mockErrorReader struct {
	err error
}

func (m *mockErrorReader) Read(p []byte) (int, error) {
	return 0, m.err
}

func TestInputReader_PermanentReadError_TerminatesWithoutSpinning(t *testing.T) {
	expectedErr := errors.New("permanent I/O device error")
	errReader := &mockErrorReader{err: expectedErr}

	inputRd, err := input.NewReader(errReader, "xterm", 0)
	require.NoError(t, err)

	reader := NewEventInputReader(nil, io.Discard, nil, func(tty.Event) {})
	reader.started = true
	reader.done = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	reader.cancel = cancel
	reader.reader = inputRd
	reader.emitter = newInputEmitter(reader.deliverToSink)

	reader.wg.Add(1)
	go reader.readLoop(ctx, inputRd, reader.done)

	// Must terminate promptly and close Done without spinning
	select {
	case <-reader.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("readLoop spun on permanent read error rather than terminating promptly")
	}

	require.ErrorIs(t, reader.Err(), expectedErr)
}
