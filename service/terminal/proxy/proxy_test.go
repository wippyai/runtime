// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/service/exec/native"
	"go.uber.org/zap"
)

type testProcess struct {
	stdout        io.ReadCloser
	input         chan []byte
	width, height int
	mu            sync.Mutex
}

func (p *testProcess) Start() error          { return nil }
func (p *testProcess) State() string         { return "running" }
func (p *testProcess) Signal(int) error      { return nil }
func (p *testProcess) Stderr() io.ReadCloser { return nil }
func (p *testProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *testProcess) Wait() error           { return nil }
func (p *testProcess) WriteStdin(data []byte) error {
	p.input <- append([]byte(nil), data...)
	return nil
}
func (p *testProcess) Resize(width, height int) error {
	p.mu.Lock()
	p.width, p.height = width, height
	p.mu.Unlock()
	return nil
}

type testSurface struct {
	cursor    *ttyapi.Cursor
	presented chan struct{}
	rows      []string
	cursors   []ttyapi.Cursor
	mu        sync.Mutex
}

type failingSurface struct{ err error }

func (s *failingSurface) Present(ttyapi.Frame) (ttyapi.PresentStats, error) {
	return ttyapi.PresentStats{}, s.err
}
func (*failingSurface) Invalidate()  {}
func (*failingSurface) Close() error { return nil }

func (s *testSurface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	s.mu.Lock()
	s.rows = append([]string(nil), frame.Rows...)
	if frame.Cursor != nil {
		copy := *frame.Cursor
		s.cursor = &copy
		s.cursors = append(s.cursors, copy)
	}
	if s.presented != nil {
		select {
		case s.presented <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	return ttyapi.PresentStats{Rows: len(frame.Rows), ChangedRows: len(frame.Rows)}, nil
}

func TestProxyBoundsRetainedScrollbackByViewportWidth(t *testing.T) {
	proxy, err := New(
		&testProcess{input: make(chan []byte, 1)},
		&testSurface{},
		80,
		24,
	)
	require.NoError(t, err)
	require.Equal(t, 256, proxy.screen.Scrollback().MaxLines())

	wide, err := New(&testProcess{input: make(chan []byte, 1)}, &testSurface{}, execapi.MaxPTYDimension, 4)
	require.NoError(t, err)
	require.Equal(t, 1, wide.screen.Scrollback().MaxLines())
}

func TestProxyPrimaryScreenWheelPresentsBoundedHistory(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	surface := &testSurface{}
	proxy, err := New(process, surface, 12, 2)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, proxy.screen.ScrollbackLen(), 3)

	require.NoError(t, proxy.handle(wheel("wheel_up")))
	surface.mu.Lock()
	rows, cursor := append([]string(nil), surface.rows...), *surface.cursor
	surface.mu.Unlock()
	require.Equal(t, []string{"one", "two"}, rows)
	require.False(t, cursor.Visible)
	select {
	case unexpected := <-process.input:
		t.Fatalf("primary-screen wheel reached child: %q", unexpected)
	default:
	}

	// Growing history keeps the reader on the same lines.
	_, err = proxy.writeOutput([]byte("\r\nsix"))
	require.NoError(t, err)
	require.NoError(t, proxy.present())
	surface.mu.Lock()
	rows = append([]string(nil), surface.rows...)
	surface.mu.Unlock()
	require.Equal(t, []string{"one", "two"}, rows)

	// Actual input returns the viewport to the live screen before forwarding.
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", Key: "x", Action: "press"}))
	require.Equal(t, "x", string(<-process.input))
	surface.mu.Lock()
	cursor = *surface.cursor
	surface.mu.Unlock()
	require.True(t, cursor.Visible)
}

func TestProxyPrimaryTrackpadTicksScrollHistory(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 12, 2)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\nseven\r\neight\r\nnine\r\nten\r\neleven\r\ntwelve"))
	require.NoError(t, err)

	// Physical ports normalize wheel and trackpad movement into the same
	// discrete wheel event; a trackpad gesture therefore delivers many ticks.
	for range 3 {
		require.NoError(t, proxy.handle(wheel("wheel_up")))
	}
	require.Equal(t, 9, proxy.viewOffset)
	select {
	case unexpected := <-process.input:
		t.Fatalf("primary-screen trackpad tick reached child: %q", unexpected)
	default:
	}
}

func TestProxyIgnoresNonWheelMouseActionsOnPrimaryScreen(t *testing.T) {
	proxy, err := New(&testProcess{input: make(chan []byte, 1)}, &testSurface{}, 12, 2)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive"))
	require.NoError(t, err)

	for _, action := range []string{"press", "release"} {
		require.NoError(t, proxy.handle(ttyapi.Event{Type: "mouse", Action: action, Button: "wheel_up"}))
	}
	require.Zero(t, proxy.viewOffset)
}

func TestProxyResizeClampsHistoryAndReboundsItsCellBudget(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 80, 2)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("one\r\ntwo\r\nthree\r\nfour"))
	require.NoError(t, err)
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Positive(t, proxy.screen.ScrollbackLen())

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "resize", Width: execapi.MaxPTYDimension, Height: 4}))
	require.Equal(t, 1, proxy.screen.Scrollback().MaxLines())
	require.LessOrEqual(t, proxy.screen.ScrollbackLen(), proxy.screen.Scrollback().MaxLines())

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "resize", Width: 80, Height: 4}))
	require.Equal(t, 256, proxy.screen.Scrollback().MaxLines())
}

func TestProxyNarrowResizeDropsOverwideHistory(t *testing.T) {
	proxy, err := New(&testProcess{input: make(chan []byte, 1)}, &testSurface{}, execapi.MaxPTYDimension, 1)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("\x1b[1;65535HX\r\n"))
	require.NoError(t, err)
	require.Equal(t, 1, proxy.screen.ScrollbackLen())
	require.Greater(t, len(proxy.screen.Scrollback().Line(0)), scrollbackCellBudget)

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "resize", Width: 80, Height: 1}))
	require.Zero(t, proxy.screen.ScrollbackLen())
	require.Equal(t, maxScrollbackLines, proxy.screen.Scrollback().MaxLines())
}

func TestProxyNarrowResizeCapsLaterOutputAroundRetainedWideRows(t *testing.T) {
	proxy, err := New(&testProcess{input: make(chan []byte, 1)}, &testSurface{}, 5_000, 1)
	require.NoError(t, err)
	for i := range 4 {
		_, err = proxy.writeOutput([]byte(fmt.Sprintf("\x1b[1;4000H%d\r\n", i)))
		require.NoError(t, err)
	}
	require.Equal(t, 4, proxy.screen.ScrollbackLen())

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "resize", Width: 80, Height: 1}))
	// Four 4,000-cell rows leave space for only 56 current-width rows.
	require.Equal(t, 60, proxy.screen.Scrollback().MaxLines())
	narrowRow := []byte(strings.Repeat("n", 80) + "\r\n")
	for range maxScrollbackLines {
		_, err = proxy.writeOutput(narrowRow)
		require.NoError(t, err)
	}
	lines := proxy.screen.Scrollback().Lines()
	used := 0
	for _, line := range lines {
		used += len(line)
	}
	require.LessOrEqual(t, used, scrollbackCellBudget)
}

func TestProxyForwardsPrimaryWheelWhenChildTracksMouse(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 12, 2)
	require.NoError(t, err)
	_, err = proxy.writeOutput([]byte("one\r\ntwo\r\nthree\r\nfour"))
	require.NoError(t, err)
	_, err = proxy.screen.Write([]byte(ansi.SetModeMouseNormal))
	require.NoError(t, err)

	require.NoError(t, proxy.handle(wheel("wheel_up")))
	require.Equal(t, ansi.MouseX10(ansi.EncodeMouseButton(ansi.MouseWheelUp, false, false, false, false), 2, 3), string(<-process.input))
}

func TestProxyScrolledViewportMovesAfterHistoryEviction(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	surface := &testSurface{}
	proxy, err := New(process, surface, 8, 1)
	require.NoError(t, err)
	for i := range maxScrollbackLines + 1 {
		_, err = proxy.writeOutput([]byte(fmt.Sprintf("%03d\r\n", i)))
		require.NoError(t, err)
	}
	require.Equal(t, maxScrollbackLines, proxy.screen.ScrollbackLen())
	require.NoError(t, proxy.handle(wheel("wheel_up")))
	surface.mu.Lock()
	before := surface.rows[0]
	surface.mu.Unlock()

	_, err = proxy.writeOutput([]byte("999\r\n"))
	require.NoError(t, err)
	require.NoError(t, proxy.present())
	surface.mu.Lock()
	after := surface.rows[0]
	surface.mu.Unlock()
	require.NotEqual(t, before, after, "x/vt does not expose an eviction generation to anchor a full history viewport")
}

func BenchmarkProxyOutputScrollback(b *testing.B) {
	for _, limit := range []struct {
		name   string
		lines  int
		legacy bool
	}{
		{name: "previous_retain_1", lines: 1, legacy: true},
		{name: "bounded_256", lines: maxScrollbackLines},
	} {
		b.Run(limit.name, func(b *testing.B) {
			proxy, err := New(&testProcess{input: make(chan []byte, 1)}, &testSurface{}, 80, 24)
			require.NoError(b, err)
			proxy.screen.SetScrollbackSize(limit.lines)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if limit.legacy {
					proxy.screenMu.Lock()
					_, err = proxy.screen.Write([]byte("benchmark output\r\n"))
					proxy.screenMu.Unlock()
				} else {
					_, err = proxy.writeOutput([]byte("benchmark output\r\n"))
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestProxyCoalescesTransportChunksIntoOneCursorFrame(t *testing.T) {
	reader, writer := io.Pipe()
	process := &shutdownProcess{
		testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
		wait:        make(chan error, 1),
		signals:     make(chan int, 1),
	}
	surface := &testSurface{presented: make(chan struct{}, 1)}
	proxy, err := New(process, surface, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()

	// A terminal commonly fills a row before moving its cursor. The PTY may
	// split those writes, but the outer compositor must not publish the
	// temporary right-edge cursor as a standalone frame.
	require.NoError(t, writeAll(writer, "abcdefghij"))
	require.NoError(t, writeAll(writer, "\x1b[1;2H"))
	select {
	case <-surface.presented:
	case <-time.After(time.Second):
		t.Fatal("coalesced terminal frame was not presented")
	}
	surface.mu.Lock()
	require.Equal(t, []ttyapi.Cursor{{Column: 1, Row: 0, Visible: true}}, surface.cursors)
	surface.mu.Unlock()

	require.NoError(t, writer.Close())
	process.wait <- nil
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("terminal proxy did not finish")
	}
}

func writeAll(writer io.Writer, value string) error {
	_, err := io.WriteString(writer, value)
	return err
}
func (*testSurface) Invalidate()  {}
func (*testSurface) Close() error { return nil }

type shutdownProcess struct {
	*testProcess
	wait    chan error
	signals chan int
}

func (p *shutdownProcess) Wait() error          { return <-p.wait }
func (p *shutdownProcess) Signal(sig int) error { p.signals <- sig; return nil }

type cancelableWaitProcess struct {
	*shutdownProcess
	canceled chan struct{}
	once     sync.Once
}

func (p *cancelableWaitProcess) Wait() error {
	select {
	case err := <-p.wait:
		return err
	case <-p.canceled:
		return context.Canceled
	}
}

func (p *cancelableWaitProcess) CancelWait() { p.once.Do(func() { close(p.canceled) }) }

type gatedProcess struct {
	*shutdownProcess
	startEntered chan struct{}
	startRelease chan struct{}
}

func (p *gatedProcess) Start() error {
	close(p.startEntered)
	<-p.startRelease
	return nil
}

type blockingInputProcess struct {
	*shutdownProcess
	writeEntered chan struct{}
	writeRelease chan struct{}
}

func (p *blockingInputProcess) WriteStdin([]byte) error {
	close(p.writeEntered)
	<-p.writeRelease
	return io.ErrClosedPipe
}

func (p *blockingInputProcess) Signal(sig int) error {
	p.signals <- sig
	close(p.writeRelease)
	return nil
}

func TestProxyRendersAndResizes(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	surface := &testSurface{}
	proxy, err := New(process, surface, 10, 2)
	require.NoError(t, err)
	_, err = proxy.screen.Write([]byte("\x1b[31mhello\x1b[0m"))
	require.NoError(t, err)
	require.NoError(t, proxy.present())
	require.Len(t, surface.rows, 2)
	require.Contains(t, surface.rows[0], "hello")
	require.Equal(t, &ttyapi.Cursor{Column: 5, Row: 0, Visible: true}, surface.cursor)

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "resize", Width: 20, Height: 4}))
	require.Equal(t, 20, process.width)
	require.Equal(t, 4, process.height)
	require.Len(t, surface.rows, 4)
	require.ErrorIs(t, proxy.handle(ttyapi.Event{Type: "resize", Width: execapi.MaxPTYCells, Height: 2}), execapi.ErrInvalidPTYSize)
	require.Equal(t, 20, proxy.screen.Width())
	require.Equal(t, 4, proxy.screen.Height())
}

func TestProxyEncodesTerminalKeys(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "up", Key: "up", Action: "press"}))
	select {
	case got := <-process.input:
		require.Equal(t, "\x1b[A", string(got))
	case <-time.After(time.Second):
		t.Fatal("terminal key was not forwarded")
	}
}

func TestProxyEncodesApplicationCursorKey(t *testing.T) {
	process := &testProcess{stdout: io.NopCloser(strings.NewReader("")), input: make(chan []byte, 1)}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	proxy.input.appCursor.Store(true)
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "down", Key: "down", Action: "press"}))
	require.Equal(t, "\x1bOB", string(<-process.input))
}

func TestProxyAnswersTerminalQueriesWithoutBlockingParser(t *testing.T) {
	process := &testProcess{
		stdout: io.NopCloser(strings.NewReader("\x1b[c")),
		input:  make(chan []byte, 1),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()

	select {
	case response := <-process.input:
		require.NotEmpty(t, response)
		require.Equal(t, byte('\x1b'), response[0])
		require.Equal(t, byte('c'), response[len(response)-1])
	case <-time.After(time.Second):
		t.Fatal("terminal capability query blocked the output parser")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("terminal proxy did not finish after answering query")
	}
}

func TestProxyAnswersKittyKeyboardQuery(t *testing.T) {
	process := &testProcess{
		stdout: io.NopCloser(strings.NewReader("\x1b[?u")),
		input:  make(chan []byte, 1),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()
	require.Equal(t, "\x1b[?0u", string(<-process.input))
	require.NoError(t, <-done)
}

func TestProxyCloseWaitsForProcessAndOutput(t *testing.T) {
	reader, writer := io.Pipe()
	process := &shutdownProcess{
		testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
		wait:        make(chan error, 1),
		signals:     make(chan int, 1),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	events := make(chan ttyapi.Event, 1)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), events) }()
	events <- ttyapi.Event{Type: "close"}
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)

	select {
	case err := <-done:
		t.Fatalf("proxy returned before child exit: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	process.wait <- errors.New("signal: terminated")
	require.NoError(t, writer.Close())
	select {
	case err := <-done:
		require.NoError(t, err, "explicit close suppresses the expected signal exit")
	case <-time.After(time.Second):
		t.Fatal("proxy did not finish after process and output closed")
	}
}

func TestProxyCloseDuringStartIsDeliveredAfterStartup(t *testing.T) {
	reader, writer := io.Pipe()
	process := &gatedProcess{
		shutdownProcess: &shutdownProcess{
			testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
			wait:        make(chan error, 1),
			signals:     make(chan int, 1),
		},
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()
	<-process.startEntered
	closeRequested := make(chan struct{})
	go func() {
		proxy.RequestClose()
		close(closeRequested)
	}()
	select {
	case <-closeRequested:
	case <-time.After(time.Second):
		t.Fatal("close request blocked behind process startup")
	}
	close(process.startRelease)
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	process.wait <- errors.New("signal: terminated")
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}

func TestRequestCloseInterruptsBlockedInput(t *testing.T) {
	reader, writer := io.Pipe()
	process := &blockingInputProcess{
		shutdownProcess: &shutdownProcess{
			testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
			wait:        make(chan error, 1),
			signals:     make(chan int, 1),
		},
		writeEntered: make(chan struct{}),
		writeRelease: make(chan struct{}),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	events := make(chan ttyapi.Event, 1)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), events) }()
	events <- ttyapi.Event{Type: "key", KeyType: "runes", Key: "x", Action: "press"}
	<-process.writeEntered
	proxy.RequestClose()
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	process.wait <- errors.New("signal: terminated")
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}

func TestRequestCloseArmsShutdownEscalation(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	process := &cancelableWaitProcess{
		shutdownProcess: &shutdownProcess{
			testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
			wait:        make(chan error),
			signals:     make(chan int, 2),
		},
		canceled: make(chan struct{}),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	proxy.shutdownGrace = 10 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()

	proxy.RequestClose()
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	require.Equal(t, int(syscall.SIGKILL), <-process.signals)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrShutdownTimeout)
	case <-time.After(time.Second):
		t.Fatal("direct close request did not drive shutdown escalation")
	}
}

func TestProxyRejectsOversizedScreenBeforeAllocation(t *testing.T) {
	proxy, err := New(&testProcess{}, &testSurface{}, execapi.MaxPTYCells, 2)
	require.Nil(t, proxy)
	require.ErrorIs(t, err, ErrInvalidProxy)
}

func TestProxyPresentFailureTerminatesAndReapsProcess(t *testing.T) {
	reader, writer := io.Pipe()
	process := &shutdownProcess{
		testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
		wait:        make(chan error, 1),
		signals:     make(chan int, 2),
	}
	renderErr := errors.New("render failed")
	proxy, err := New(process, &failingSurface{err: renderErr}, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), make(chan ttyapi.Event)) }()
	_, err = writer.Write([]byte("output"))
	require.NoError(t, err)
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	process.wait <- errors.New("signal: terminated")
	require.NoError(t, writer.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, renderErr)
	case <-time.After(time.Second):
		t.Fatal("proxy did not reap the process after a presentation failure")
	}
}

func TestProxyCancelsAbandonedRemoteWait(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	process := &cancelableWaitProcess{
		shutdownProcess: &shutdownProcess{
			testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
			wait:        make(chan error),
			signals:     make(chan int, 2),
		},
		canceled: make(chan struct{}),
	}
	proxy, err := New(process, &testSurface{}, 10, 2)
	require.NoError(t, err)
	proxy.shutdownGrace = 10 * time.Millisecond
	events := make(chan ttyapi.Event, 1)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), events) }()

	events <- ttyapi.Event{Type: "close"}
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	require.Equal(t, int(syscall.SIGKILL), <-process.signals)
	select {
	case <-process.canceled:
	case <-time.After(time.Second):
		t.Fatal("proxy did not cancel an abandoned remote wait")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrShutdownTimeout)
	case <-time.After(time.Second):
		t.Fatal("proxy remained blocked after abandoning remote wait")
	}
	_, err = writer.Write([]byte("orphan check"))
	require.Error(t, err, "abandoning a proxy must close its owned output reader")
}

func TestProxyRunsNativeTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY integration requires Unix")
	}
	executor := native.NewNativeExecutor(zap.NewNop(), &execapi.NativeExecutorConfig{})
	process, err := executor.NewProcess(
		"sh -c 'printf ready; read value; stty size'",
		execapi.ProcessOptions{PTY: &execapi.PTYOptions{Width: 40, Height: 6, Term: "xterm-256color"}},
	)
	require.NoError(t, err)
	ptyProcess, ok := process.(execapi.PTYProcess)
	require.True(t, ok)
	surface := &testSurface{}
	proxy, err := New(ptyProcess, surface, 40, 6)
	require.NoError(t, err)
	events := make(chan ttyapi.Event, 4)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), events) }()

	require.Eventually(t, func() bool {
		surface.mu.Lock()
		defer surface.mu.Unlock()
		return strings.Contains(strings.Join(surface.rows, "\n"), "ready")
	}, 3*time.Second, 10*time.Millisecond)
	events <- ttyapi.Event{Type: "resize", Width: 100, Height: 30}
	events <- ttyapi.Event{Type: "key", KeyType: "runes", Key: "x", Action: "press"}
	events <- ttyapi.Event{Type: "key", KeyType: "enter", Key: "enter", Action: "press"}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("native terminal proxy did not finish")
	}
	surface.mu.Lock()
	rendered := strings.Join(surface.rows, "\n")
	surface.mu.Unlock()
	require.Contains(t, rendered, "30 100")
}

func TestProxyRunsNativeInteractiveEditingKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY integration requires Unix")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("interactive PTY integration requires Bash")
	}
	executor := native.NewNativeExecutor(zap.NewNop(), &execapi.NativeExecutorConfig{})
	process, err := executor.NewProcess(
		"/bin/bash --noprofile --norc",
		execapi.ProcessOptions{PTY: &execapi.PTYOptions{Width: 80, Height: 20, Term: "xterm-256color"}},
	)
	require.NoError(t, err)
	ptyProcess, ok := process.(execapi.PTYProcess)
	require.True(t, ok)
	surface := &testSurface{}
	proxy, err := New(ptyProcess, surface, 80, 20)
	require.NoError(t, err)
	events := make(chan ttyapi.Event, 32)
	done := make(chan error, 1)
	go func() { done <- proxy.Run(context.Background(), events) }()

	sendText := func(value string) {
		events <- ttyapi.Event{Type: "key", KeyType: "runes", Key: value, Action: "press"}
	}
	sendKey := func(name string) {
		events <- ttyapi.Event{Type: "key", KeyType: name, Key: name, Action: "press"}
	}
	require.Eventually(t, func() bool {
		surface.mu.Lock()
		defer surface.mu.Unlock()
		return strings.Contains(strings.Join(surface.rows, "\n"), "bash-")
	}, 3*time.Second, 10*time.Millisecond)

	// Left inserts in the middle instead of echoing its escape sequence.
	sendText("printf ac")
	sendKey("left")
	sendText("b")
	sendKey("enter")

	// Backspace edits the line according to the PTY's configured erase byte.
	sendText("printf backX")
	sendKey("backspace")
	sendText("-ok")
	sendKey("enter")

	// Home followed by Ctrl+K replaces the whole current input line.
	sendText("printf discarded")
	sendKey("home")
	events <- ttyapi.Event{Type: "key", KeyType: "runes", Key: "k", Ctrl: true, Action: "press"}
	sendText("printf home-ok")
	sendKey("enter")
	sendText("exit")
	sendKey("enter")

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("interactive terminal proxy did not finish")
	}
	surface.mu.Lock()
	rendered := strings.Join(surface.rows, "\n")
	surface.mu.Unlock()
	require.Contains(t, rendered, "abc")
	require.Contains(t, rendered, "back-ok")
	require.Contains(t, rendered, "home-ok")
}
