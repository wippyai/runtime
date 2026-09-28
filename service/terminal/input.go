// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	tty "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

// InputReader reads terminal input and delivers parsed events to a sink.
type InputReader struct {
	graphicsProbeAt time.Time
	output          io.Writer
	emitter         *inputEmitter
	raw             *RawManager
	sink            func(tty.Event)
	reader          terminalInputReader
	cancel          context.CancelFunc
	stopDone        chan struct{}
	stopErr         error
	done            chan struct{}
	err             error
	stdin           *os.File
	graphicsMode    string
	wg              sync.WaitGroup
	mu              sync.Mutex
	started         bool
	stopping        bool
	mouseEnabled    bool
	pasteEnabled    bool
	doneClosed      bool
}

// NewEventInputReader creates an InputReader that delivers events to the given sink
// without requiring an actor scheduler or PID routing.
//
// Start acquires terminal resources. A nil raw manager is created from stdin;
// nil output and sink discard their respective values.
//
// The sink is called serially and must return promptly. It must not call back
// into the reader or wait for network acknowledgments. Network consumers should
// enqueue events with bounded capacity and handle overflow outside the callback.
func NewEventInputReader(stdin *os.File, output io.Writer, raw *RawManager, sink func(tty.Event)) *InputReader {
	if output == nil {
		output = io.Discard
	}
	if sink == nil {
		sink = func(tty.Event) {}
	}
	if raw == nil && stdin != nil {
		raw = NewRawManager(stdin)
	}
	return &InputReader{
		stdin:  stdin,
		output: output,
		raw:    raw,
		sink:   sink,
		done:   make(chan struct{}),
	}
}

// NewInputReader creates an InputReader that delivers events to the given process
// via the actor scheduler. It is implemented as an adapter to NewEventInputReader.
func NewInputReader(stdin *os.File, output io.Writer, raw *RawManager, scheduler *actor.Scheduler, targetPID pid.PID) *InputReader {
	return NewEventInputReader(stdin, output, raw, func(ev tty.Event) {
		if scheduler == nil {
			return
		}
		pkg := relay.AcquirePackage()
		pkg.Target = targetPID
		evCopy := ev
		pkg.AddMessage(relay.Topic(TopicTTYEvents), payload.New(&evCopy))
		if err := scheduler.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
		}
	})
}

// Done returns a completion channel that is closed when the reader terminates
// (via Stop, EOF, or a read error) for the current active Start session.
func (r *InputReader) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// Err returns the error that caused the reader to terminate, if any.
// It returns io.EOF if the input stream reached EOF, or a non-nil error if
// a reader error occurred. If the reader was stopped cleanly or is still running,
// Err returns nil.
func (r *InputReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Start enables raw mode and spawns the read loop and SIGWINCH goroutine.
func (r *InputReader) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started || r.stopping {
		return errors.New("input reader already started")
	}
	r.stopErr = nil
	if r.doneClosed || r.done == nil {
		r.done = make(chan struct{})
		r.doneClosed = false
	}
	r.err = nil

	if r.raw == nil {
		if r.stdin != nil {
			r.raw = NewRawManager(r.stdin)
		} else {
			return errors.New("terminal raw manager is required")
		}
	}
	if err := r.raw.Enable(); err != nil {
		return err
	}

	if r.stdin == nil {
		_ = r.raw.Disable()
		return errors.New("stdin is required")
	}

	reader, err := newTerminalInputReader(r.stdin, os.Getenv("TERM"))
	if err != nil {
		_ = r.raw.Disable()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.reader = reader
	r.emitter = newInputEmitter(r.deliverToSink)
	r.started = true
	pasteSequence := []byte("\033[?2004h")
	if n, err := r.output.Write(pasteSequence); err != nil || n != len(pasteSequence) {
		r.started = false
		cancel()
		r.cancel = nil
		_ = reader.Close()
		r.reader = nil
		if r.emitter != nil {
			r.emitter.stop()
			r.emitter = nil
		}
		_ = r.raw.Disable()
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	r.pasteEnabled = true

	// Send initial start event with terminal size
	cols, rows, sizeErr := r.screenSize()
	if sizeErr == nil {
		r.emitter.emit(&TTYEvent{
			Type:   "start",
			Width:  cols,
			Height: rows,
		})
	}

	r.wg.Add(2)
	go r.readLoop(ctx, reader, r.done)
	go r.sigwinchLoop(ctx)

	return nil
}

// Stop cancels the read loop, waits for goroutines, and restores the terminal.
func (r *InputReader) Stop() error {
	return r.stopWithCause(nil, nil)
}

func (r *InputReader) stopWithCause(cause error, session <-chan struct{}) error {
	r.mu.Lock()
	// A completed reader may finish after Stop and a subsequent Start.
	if session != nil && session != r.done {
		r.mu.Unlock()
		return nil
	}
	if cause != nil && r.err == nil {
		r.err = cause
	}
	if r.stopping {
		done := r.stopDone
		r.mu.Unlock()
		<-done
		r.mu.Lock()
		err := r.stopErr
		r.mu.Unlock()
		return err
	}
	if !r.started {
		err := r.stopErr
		r.mu.Unlock()
		return err
	}

	r.started = false
	r.stopping = true
	r.stopDone = make(chan struct{})

	emitter := r.emitter
	cancel := r.cancel
	r.cancel = nil
	reader := r.reader
	r.mu.Unlock()

	if emitter != nil {
		emitter.stop()
	}
	if cancel != nil {
		cancel()
	}
	if reader != nil {
		reader.Cancel()
	}

	r.wg.Wait()

	r.mu.Lock()
	if r.reader != nil {
		_ = r.reader.Close()
		r.reader = nil
	}
	r.emitter = nil

	// Disable mouse tracking if it was enabled
	if r.mouseEnabled {
		_, _ = r.output.Write([]byte("\033[?1006l\033[?1003l"))
		r.mouseEnabled = false
	}
	var pasteErr error
	if r.pasteEnabled {
		sequence := []byte("\033[?2004l")
		if n, err := r.output.Write(sequence); err != nil {
			pasteErr = err
		} else if n != len(sequence) {
			pasteErr = io.ErrShortWrite
		}
		r.pasteEnabled = false
	}
	var rawErr error
	if r.raw != nil {
		rawErr = r.raw.Disable()
	}
	r.stopping = false
	r.stopErr = errors.Join(pasteErr, rawErr)
	if r.stopErr != nil {
		if r.err == nil {
			r.err = r.stopErr
		} else {
			r.err = errors.Join(r.err, r.stopErr)
		}
	}
	if !r.doneClosed {
		r.doneClosed = true
		if r.done != nil {
			close(r.done)
		}
	}
	close(r.stopDone)
	r.stopDone = nil
	err := r.stopErr
	r.mu.Unlock()
	return err
}

// EnableMouse enables mouse event tracking (SGR mode).
func (r *InputReader) EnableMouse() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started && !r.stopping && !r.mouseEnabled {
		_, _ = r.output.Write([]byte("\033[?1003h\033[?1006h"))
		r.mouseEnabled = true
	}
}

// DisableMouse disables mouse event tracking.
func (r *InputReader) DisableMouse() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mouseEnabled {
		_, _ = r.output.Write([]byte("\033[?1006l\033[?1003l"))
		r.mouseEnabled = false
	}
}

// ScreenSize returns the current terminal dimensions.
func (r *InputReader) ScreenSize() (int, int, error) {
	return r.screenSize()
}

func (r *InputReader) screenSize() (int, int, error) {
	if r.stdin == nil {
		return 0, 0, errors.New("stdin is nil")
	}
	return term.GetSize(r.stdin.Fd())
}

func (r *InputReader) readLoop(ctx context.Context, reader terminalInputReader, session <-chan struct{}) {
	var readErr error
	defer func() {
		r.wg.Done()
		if readErr != nil {
			_ = r.stopWithCause(readErr, session)
		}
	}()

	readErr = streamTerminalInput(ctx, reader, r.sendEvent, r.graphicsReply)
}

func (r *InputReader) emitResize() {
	cols, rows, err := r.screenSize()
	if err == nil {
		r.sendEvent(&TTYEvent{
			Type:   "resize",
			Width:  cols,
			Height: rows,
		})
	}
}

func (r *InputReader) sendEvent(ev *TTYEvent) {
	r.mu.Lock()
	emitter := r.emitter
	r.mu.Unlock()
	if emitter != nil {
		emitter.emit(ev)
	}
}

func (r *InputReader) deliverToSink(ev *TTYEvent) {
	if ev == nil {
		return
	}
	if r.sink != nil {
		r.sink(*ev)
	}
}

var _ tty.InputController = (*InputReader)(nil)
