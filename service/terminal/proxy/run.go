// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

const (
	defaultShutdownGrace = 3 * time.Second
	// PTY reads are transport chunks, not presentation boundaries. A short,
	// demand-driven frame interval lets cursor moves and the cells they follow
	// land atomically without keeping an idle ticker alive.
	frameInterval = 8 * time.Millisecond
)

// Run starts the external terminal and owns its I/O until completion,
// cancellation, or a close event. The caller owns the events channel.
func (p *Proxy) Run(ctx context.Context, events <-chan ttyapi.Event) error {
	if err := p.start(); err != nil {
		return err
	}
	if err := p.process.Resize(p.screen.Width(), p.screen.Height()); err != nil {
		return stopStartedProcess(p.process, err, p.shutdownTimeout())
	}
	output := p.process.Stdout()
	if output == nil {
		return stopStartedProcess(p.process, execapi.ErrPTYUnavailable, p.shutdownTimeout())
	}
	defer func() { _ = output.Close() }()
	defer func() {
		// Closing the response pipe wakes copyResponses without racing x/vt's
		// output parser through Emulator.Close's unsynchronized closed flag.
		if closer, ok := p.screen.InputPipe().(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	dirty := make(chan struct{}, 1)
	outputDone := make(chan error, 1)
	responseDone := make(chan error, 1)
	waitDone := make(chan error, 1)
	// Terminal applications issue synchronous capability and cursor queries.
	// x/vt answers them through its input pipe; that pipe must be drained while
	// output is parsed or io.Pipe correctly blocks the parser forever.
	go p.copyResponses(responseDone)
	go p.copyOutput(output, dirty, outputDone)
	go func() { waitDone <- p.process.Wait() }()
	ctxDone := ctx.Done()
	closeRequested := p.closeNotify
	var waitErr, shutdownCause error
	processDone, outputClosed := false, false
	closing := false
	var frameTimer *time.Timer
	var frameReady <-chan time.Time
	var shutdownTimer <-chan time.Time
	var forcedShutdownTimer <-chan time.Time
	stopFrameTimer := func() {
		if frameTimer != nil {
			if !frameTimer.Stop() {
				select {
				case <-frameTimer.C:
				default:
				}
			}
		}
		frameReady = nil
	}
	defer stopFrameTimer()

	suppressExitError := false
	beginShutdown := func(cause error, suppressExit bool) {
		if suppressExit {
			// Once the owner has requested shutdown, terminal I/O commonly
			// unwinds with a closed-pipe or platform-specific process error.
			// RequestClose reports the signal failure directly; Run owns only
			// failures needed to terminate and reap the process from here.
			cause = nil
		}
		if closing {
			if shutdownCause == nil && cause != nil {
				shutdownCause = cause
			}
			return
		}
		closing, shutdownCause, ctxDone = true, cause, nil
		suppressExitError = suppressExit
		p.RequestClose()
		if err := p.closeSignalError(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			shutdownCause = errors.Join(shutdownCause, err)
		}
		shutdownTimer = time.After(p.shutdownTimeout())
	}
	if p.closeRequested.Load() {
		beginShutdown(nil, true)
	}

	for {
		select {
		case <-closeRequested:
			closeRequested = nil
			beginShutdown(nil, true)
		case <-ctxDone:
			beginShutdown(ctx.Err(), p.closeRequested.Load())
		case <-shutdownTimer:
			shutdownTimer = nil
			if err := p.process.Signal(int(syscall.SIGKILL)); err != nil && !errors.Is(err, os.ErrProcessDone) {
				shutdownCause = errors.Join(shutdownCause, err)
			}
			forcedShutdownTimer = time.After(p.shutdownTimeout())
		case <-forcedShutdownTimer:
			cancelProcessWait(p.process)
			return errors.Join(shutdownCause, ErrShutdownTimeout)
		case err := <-waitDone:
			waitErr, processDone, waitDone = err, true, nil
			if outputClosed {
				if err := p.present(); err != nil {
					return err
				}
				if closing && (shutdownCause != nil || suppressExitError) {
					return shutdownCause
				}
				if !closing && p.closeRequested.Load() {
					return nil
				}
				return waitErr
			}
		case err := <-outputDone:
			outputClosed, outputDone = true, nil
			if err != nil && !terminalEOF(err) {
				beginShutdown(err, false)
			}
			if processDone {
				if err := p.present(); err != nil {
					return err
				}
				if closing && (shutdownCause != nil || suppressExitError) {
					return shutdownCause
				}
				if !closing && p.closeRequested.Load() {
					return nil
				}
				return waitErr
			}
		case err := <-responseDone:
			responseDone = nil
			if err != nil && !terminalEOF(err) {
				beginShutdown(err, p.closeRequested.Load())
			}
		case <-dirty:
			if !closing && frameReady == nil {
				if frameTimer == nil {
					frameTimer = time.NewTimer(frameInterval)
				} else {
					frameTimer.Reset(frameInterval)
				}
				frameReady = frameTimer.C
			}
		case <-frameReady:
			frameReady = nil
			if !closing {
				if err := p.present(); err != nil {
					beginShutdown(err, false)
				}
			}
		case event, ok := <-events:
			if !ok || event.Type == "close" {
				beginShutdown(nil, true)
				continue
			}
			if closing {
				continue
			}
			if err := p.handle(event); err != nil {
				beginShutdown(err, p.closeRequested.Load())
			}
		}
	}
}

// stopStartedProcess closes the ownership gap between Start and the proxy event
// loop. It always reaps the child, escalating when graceful termination stalls.
func stopStartedProcess(process execapi.Process, cause error, grace time.Duration) error {
	if err := process.Signal(int(syscall.SIGTERM)); err != nil && !errors.Is(err, os.ErrProcessDone) {
		cause = errors.Join(cause, err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		return errors.Join(cause, err)
	case <-time.After(grace):
		if err := process.Signal(int(syscall.SIGKILL)); err != nil && !errors.Is(err, os.ErrProcessDone) {
			cause = errors.Join(cause, err)
		}
		select {
		case err := <-done:
			return errors.Join(cause, err)
		case <-time.After(grace):
			cancelProcessWait(process)
			return errors.Join(cause, ErrShutdownTimeout)
		}
	}
}

func (p *Proxy) shutdownTimeout() time.Duration {
	if p.shutdownGrace <= 0 {
		return defaultShutdownGrace
	}
	return p.shutdownGrace
}

func cancelProcessWait(process execapi.Process) {
	if canceler, ok := process.(execapi.WaitCanceler); ok {
		canceler.CancelWait()
	}
}

func (p *Proxy) copyResponses(done chan<- error) {
	buf := make([]byte, 4096)
	for {
		n, err := p.screen.Read(buf)
		if n > 0 {
			if writeErr := p.writeBytes(buf[:n]); writeErr != nil {
				done <- writeErr
				return
			}
		}
		if err != nil {
			done <- err
			return
		}
	}
}

func (p *Proxy) copyOutput(reader io.Reader, dirty chan<- struct{}, done chan<- error) {
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			p.screenMu.Lock()
			_, writeErr := p.screen.Write(buf[:n])
			p.screenMu.Unlock()
			if writeErr != nil {
				done <- writeErr
				return
			}
			select {
			case dirty <- struct{}{}:
			default:
			}
		}
		if err != nil {
			done <- err
			return
		}
	}
}

func terminalEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO)
}
