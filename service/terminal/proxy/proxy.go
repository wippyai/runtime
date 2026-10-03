// SPDX-License-Identifier: MPL-2.0

// Package proxy bridges a byte-oriented PTY process to a structured TTY
// surface. Scheduling and window composition remain the caller's concern.
package proxy

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xterm "github.com/gitpod-io/xterm-go"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var (
	ErrInvalidProxy    = errors.New("terminal proxy requires a process, surface, and bounded positive size")
	ErrShutdownTimeout = errors.New("PTY process did not exit after forced shutdown")
)

const (
	// scrollbackCellBudget keeps history bounded even for a very wide PTY.
	scrollbackCellBudget = 20_480
	maxScrollbackLines   = 256

	// reflowLineCapacity is the line capacity the emulator is created with.
	// Narrowing the columns reflows history into more lines, and the emulator
	// cannot reflow into a list with less room than the result needs; the
	// result is at most half the cells of the largest PTY plus retained
	// history. The retained history itself is bounded by boundHistoryLocked.
	reflowLineCapacity = (execapi.MaxPTYCells+scrollbackCellBudget)/2 + maxScrollbackLines

	// scrollLinesPerWheel matches the usual terminal wheel notch behavior.
	scrollLinesPerWheel = 3
)

// Proxy owns the process/VT/surface pipeline. A native Wippy process, plugin,
// or standalone adapter can supply events without exposing its scheduler here.
type Proxy struct {
	surface         ttyapi.Surface
	closeErr        error
	closeCause      error
	process         execapi.PTYProcess
	screen          *xterm.Terminal
	responses       responseQueue
	capture         *strings.Builder
	closeNotify     chan struct{}
	input           inputState
	shutdownGrace   time.Duration
	height          atomic.Int64
	viewOffset      int
	closeNotifyOnce sync.Once
	closeSignalOnce sync.Once
	screenMu        sync.Mutex
	inputMu         sync.Mutex
	lifecycleMu     sync.Mutex
	closeRequested  atomic.Bool
	started         atomic.Bool
}

// RequestClose accepts asynchronous shutdown intent from an owner that has no
// failure to report. Run owns termination and reaping; the immediate signal
// also interrupts a blocked terminal write.
func (p *Proxy) RequestClose() { p.requestClose(nil) }

// requestClose records why shutdown starts before waking the run loop. A
// signaled child unwinds its terminal I/O with incidental closed-pipe and
// platform errors, and the recorded cause is what the run actually failed on.
func (p *Proxy) requestClose(cause error) {
	p.recordCloseCause(cause)
	p.closeRequested.Store(true)
	p.closeNotifyOnce.Do(func() { close(p.closeNotify) })
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.started.Load() {
		_ = p.signalCloseLocked()
	}
}

// recordCloseCause keeps the first cause observed. Later shutdown steps report
// their own failures through Run's shutdown error channel.
func (p *Proxy) recordCloseCause(cause error) {
	if cause == nil {
		return
	}
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.closeCause == nil {
		p.closeCause = cause
	}
}

func (p *Proxy) closeCauseError() error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	return p.closeCause
}

// Started reports whether the process startup phase has completed. It does not
// inspect the process or acquire its startup lock.
func (p *Proxy) Started() bool {
	return p.started.Load()
}

// start serializes process startup with an early close request. Once Start
// succeeds, a close accepted before or during startup is delivered exactly
// once before Run begins forwarding terminal traffic.
func (p *Proxy) start() error {
	if err := p.process.Start(); err != nil {
		return err
	}
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	p.started.Store(true)
	if p.closeRequested.Load() {
		_ = p.signalCloseLocked()
	}
	return nil
}

func (p *Proxy) signalCloseLocked() error {
	p.closeSignalOnce.Do(func() {
		p.closeErr = p.process.Signal(int(closeSignal))
	})
	return p.closeErr
}

func (p *Proxy) closeSignalError() error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	return p.signalCloseLocked()
}

func New(process execapi.PTYProcess, surface ttyapi.Surface, width, height int) (*Proxy, error) {
	if process == nil || surface == nil || execapi.ValidatePTYSize(width, height) != nil {
		return nil, ErrInvalidProxy
	}
	p := &Proxy{
		process: process, surface: surface,
		screen:        xterm.New(xterm.WithCols(width), xterm.WithRows(height), xterm.WithScrollback(reflowLineCapacity)),
		shutdownGrace: defaultShutdownGrace, closeNotify: make(chan struct{}),
	}
	// A bounded primary-screen history serves nested surfaces. Physical
	// terminals normally provide their own scrollback, but a virtual surface
	// cannot.
	p.responses.init()
	p.boundHistoryLocked(width)
	p.height.Store(int64(height))
	p.input.init()
	p.screen.OnData(p.reply)
	p.installModeHandlers()
	p.installKeyboardHandlers()
	p.installPageHandlers()
	return p, nil
}

func (p *Proxy) present() error {
	p.screenMu.Lock()
	height := int(p.height.Load())
	rows, scrolled := p.rowsLocked(height)
	buffer := p.screen.Buffer()
	width := p.screen.Cols()
	visible := !p.screen.IsCursorHidden() && !scrolled
	column, row := buffer.X, buffer.Y
	p.screenMu.Unlock()
	frame := ttyapi.Frame{Rows: rows, Cursor: &ttyapi.Cursor{
		Column:  min(max(column, 0), width-1),
		Row:     min(max(row, 0), height-1),
		Visible: visible,
	}}
	_, err := p.surface.Present(frame)
	return err
}

// rowsLocked renders the live screen unless the primary-screen viewport has
// been moved into scrollback. screenMu must be held by the caller.
func (p *Proxy) rowsLocked(height int) ([]string, bool) {
	buffer := p.screen.Buffer()
	if p.screen.IsAltBufferActive() {
		p.viewOffset = 0
	}
	p.viewOffset = min(p.viewOffset, buffer.YBase)
	start := buffer.YBase - p.viewOffset
	rows := make([]string, height)
	for y := range rows {
		rows[y] = renderLine(buffer, start+y)
	}
	return rows, p.viewOffset > 0
}

// historyLenLocked is the number of primary-screen history lines. screenMu
// must be held by the caller.
func (p *Proxy) historyLenLocked() int {
	return p.screen.NormalBuffer().YBase
}

func scrollbackSize(width int) int {
	return min(maxScrollbackLines, max(scrollbackCellBudget/max(width, 1), 1))
}

// boundHistoryLocked limits primary-screen history to scrollbackSize(width)
// lines by discarding the oldest, and bounds later growth to the same size.
// screenMu must be held by the caller.
func (p *Proxy) boundHistoryLocked(width int) {
	buffer := p.screen.NormalBuffer()
	target := p.screen.Rows() + scrollbackSize(width)
	if trim := buffer.Lines.Length() - target; trim > 0 {
		buffer.Lines.TrimStart(trim)
		buffer.YBase = max(buffer.YBase-trim, 0)
		buffer.YDisp = max(buffer.YDisp-trim, 0)
		buffer.SavedState.Y = max(buffer.SavedState.Y-trim, 0)
	}
	buffer.Lines.SetMaxLength(target)
}

// historyLimitLocked is the current maximum number of history lines.
func (p *Proxy) historyLimitLocked() int {
	return p.screen.NormalBuffer().Lines.MaxLength() - p.screen.Rows()
}
