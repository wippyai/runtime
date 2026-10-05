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

	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/text"
	"github.com/wippyai/tty/vt"
)

var (
	ErrInvalidProxy    = errors.New("terminal proxy requires a process, surface, and bounded positive size")
	ErrShutdownTimeout = errors.New("PTY process did not exit after forced shutdown")
)

const (
	// scrollbackCellBudget keeps history bounded even for a very wide PTY.
	scrollbackCellBudget = 20_480
	maxScrollbackLines   = 256

	// scrollLinesPerWheel matches the usual terminal wheel notch behavior.
	scrollLinesPerWheel = 3 // also the cursor key presses sent per wheel notch under alternate scroll
)

// Proxy owns the process/VT/surface pipeline. A native Wippy process, plugin,
// or standalone adapter can supply events without exposing its scheduler here.
type Proxy struct {
	surface         ttyapi.Surface
	closeErr        error
	closeCause      error
	process         execapi.PTYProcess
	screen          *vt.Terminal
	capture         *strings.Builder
	closeNotify     chan struct{}
	responses       responseQueue
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
	// The output parser can hold screenMu while waiting for reply capacity.
	// Release it independently of Run, which may need that same lock to exit.
	p.responses.close()
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
		shutdownGrace: defaultShutdownGrace, closeNotify: make(chan struct{}),
	}
	p.responses.init()
	// A bounded primary-screen history serves nested surfaces. Physical
	// terminals normally provide their own scrollback, but a virtual surface
	// cannot.
	p.screen = vt.New(vt.Options{
		Cols: width, Rows: height, ScrollbackLines: scrollbackSize(width),
		Reply: p.reply, Colors: p.pageColors,
	})
	// Alternate scroll is on at power-on, as in the terminals this proxy
	// stands in for.
	_, _ = p.screen.Write([]byte("\x1b[?1007h"))
	p.height.Store(int64(height))
	return p, nil
}

// pageColors supplies the surface page to color queries at reply time, so a
// reply never carries a superseded page.
func (p *Proxy) pageColors() (fg, bg, cursor text.Color) {
	provider, ok := p.surface.(ttyapi.PageProvider)
	if !ok {
		return fg, bg, cursor
	}
	page, present := provider.Page()
	if !present {
		return fg, bg, cursor
	}
	pageFg, pageBg := page.Colors()
	return text.ColorModel(pageFg), text.ColorModel(pageBg), cursor
}

func (p *Proxy) present() error {
	p.screenMu.Lock()
	height := int(p.height.Load())
	rows, scrolled := p.rowsLocked(height)
	screen := p.screen.Screen()
	width, _ := screen.Size()
	cursor := screen.Cursor()
	visible := cursor.Visible && !scrolled
	p.screenMu.Unlock()
	frame := ttyapi.Frame{Rows: rows, Cursor: &ttyapi.Cursor{
		Column:  min(max(cursor.X, 0), width-1),
		Row:     min(max(cursor.Y, 0), height-1),
		Visible: visible,
	}}
	_, err := p.surface.Present(frame)
	return err
}

// rowsLocked renders the live screen unless the primary-screen viewport has
// been moved into scrollback. screenMu must be held by the caller.
func (p *Proxy) rowsLocked(height int) ([]string, bool) {
	screen := p.screen.Screen()
	if screen.Alternate() {
		p.viewOffset = 0
	}
	history := screen.ScrollbackLen()
	p.viewOffset = min(p.viewOffset, history)
	_, live := screen.Size()
	start := history - p.viewOffset
	rows := make([]string, height)
	for y := range rows {
		switch i := start + y; {
		case i < history:
			rows[y] = vt.RenderLine(vt.TrimLine(screen.ScrollbackLine(i)))
		case i-history < live:
			rows[y] = vt.RenderLine(vt.TrimLine(screen.Line(i - history)))
		}
	}
	return rows, p.viewOffset > 0
}

// historyLenLocked is the number of primary-screen history lines. screenMu
// must be held by the caller.
func (p *Proxy) historyLenLocked() int {
	return p.screen.Screen().ScrollbackLen()
}

func scrollbackSize(width int) int {
	return min(maxScrollbackLines, max(scrollbackCellBudget/max(width, 1), 1))
}

// reply routes emulator output to the child. While an input event is being
// encoded the output is the encoded event itself.
func (p *Proxy) reply(data []byte) {
	if p.capture != nil {
		p.capture.Write(data)
		return
	}
	p.responses.push(append([]byte(nil), data...))
}

func (p *Proxy) screenSize() (int, int) {
	p.screenMu.Lock()
	defer p.screenMu.Unlock()
	return p.screen.Screen().Size()
}
