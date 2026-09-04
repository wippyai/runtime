// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/charmbracelet/x/input"
	"github.com/charmbracelet/x/term"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

// InputReader reads terminal input and delivers parsed events via the scheduler.
type InputReader struct {
	output       io.Writer
	emitter      *inputEmitter
	raw          *RawManager
	scheduler    *actor.Scheduler
	reader       *input.Reader
	cancel       context.CancelFunc
	stopDone     chan struct{}
	stopErr      error
	stdin        *os.File
	targetPID    pid.PID
	wg           sync.WaitGroup
	mu           sync.Mutex
	started      bool
	stopping     bool
	mouseEnabled bool
	pasteEnabled bool
}

// NewInputReader creates an InputReader that delivers events to the given process.
func NewInputReader(stdin *os.File, output io.Writer, raw *RawManager, scheduler *actor.Scheduler, targetPID pid.PID) *InputReader {
	if output == nil {
		output = io.Discard
	}
	return &InputReader{
		stdin:     stdin,
		output:    output,
		raw:       raw,
		scheduler: scheduler,
		targetPID: targetPID,
	}
}

// Start enables raw mode and spawns the read loop and SIGWINCH goroutine.
func (r *InputReader) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started || r.stopping {
		return errors.New("input reader already started")
	}
	r.stopErr = nil

	if err := r.raw.Enable(); err != nil {
		return err
	}

	termType := os.Getenv("TERM")
	reader, err := input.NewReader(r.stdin, termType, 0)
	if err != nil {
		_ = r.raw.Disable()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.reader = reader
	r.emitter = newInputEmitter(r.sendNow)
	r.started = true
	pasteSequence := []byte("\033[?2004h")
	if n, err := r.output.Write(pasteSequence); err != nil || n != len(pasteSequence) {
		r.started = false
		cancel()
		_ = reader.Close()
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
	go r.readLoop(ctx, reader)
	go r.sigwinchLoop(ctx)

	return nil
}

// Stop cancels the read loop, waits for goroutines, and restores the terminal.
func (r *InputReader) Stop() error {
	r.mu.Lock()
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
		r.mu.Unlock()
		return nil
	}

	r.started = false
	r.stopping = true
	r.stopDone = make(chan struct{})
	if r.emitter != nil {
		r.emitter.stop()
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.reader != nil {
		r.reader.Cancel()
	}
	r.mu.Unlock()

	r.wg.Wait()

	r.mu.Lock()
	if r.reader != nil {
		_ = r.reader.Close()
		r.reader = nil
	}

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
	r.stopping = false
	r.stopErr = errors.Join(pasteErr, r.raw.Disable())
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
	return term.GetSize(r.stdin.Fd())
}

func (r *InputReader) readLoop(ctx context.Context, reader *input.Reader) {
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		events, err := reader.ReadEvents()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		for _, ev := range events {
			ttyEv := ConvertInputEvent(ev)
			if ttyEv != nil {
				r.sendEvent(ttyEv)
			}
		}
	}
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

func (r *InputReader) sendNow(ev *TTYEvent) {
	pkg := relay.AcquirePackage()
	pkg.Target = r.targetPID
	pkg.AddMessage(relay.Topic(TopicTTYEvents), payload.New(ev))
	if err := r.scheduler.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

var _ ttyapi.InputController = (*InputReader)(nil)
