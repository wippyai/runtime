// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestProxyDoesNotRetainInaccessibleScrollback(t *testing.T) {
	proxy, err := New(
		&testProcess{input: make(chan []byte, 1)},
		&testSurface{},
		80,
		24,
	)
	require.NoError(t, err)
	require.Equal(t, retainedScrollbackLines, proxy.screen.Scrollback().MaxLines())
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
	closeRequested := make(chan error, 1)
	go func() { closeRequested <- proxy.RequestClose() }()
	select {
	case err := <-closeRequested:
		require.NoError(t, err, "close request must not wait for process startup")
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
	require.NoError(t, proxy.RequestClose())
	require.Equal(t, int(syscall.SIGTERM), <-process.signals)
	process.wait <- errors.New("signal: terminated")
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
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
