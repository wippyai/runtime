// SPDX-License-Identifier: MPL-2.0

// Package proxy bridges a byte-oriented PTY process to a structured TTY
// surface. Scheduling and window composition remain the caller's concern.
package proxy

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	vt "github.com/charmbracelet/x/vt"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var (
	ErrInvalidProxy    = errors.New("terminal proxy requires a process, surface, and bounded positive size")
	ErrShutdownTimeout = errors.New("PTY process did not exit after forced shutdown")
)

const (
	// scrollbackCellBudget keeps history bounded even for a very wide PTY.
	// Keeping the cap small also bounds x/vt's line-slice eviction work, which
	// otherwise shifts its default 10,000 entries for every later output line.
	scrollbackCellBudget = 20_480
	maxScrollbackLines   = 256

	// scrollLinesPerWheel matches the usual terminal wheel notch behavior.
	scrollLinesPerWheel = 3
)

// Proxy owns the process/VT/surface pipeline. A native Wippy process, plugin,
// or standalone adapter can supply events without exposing its scheduler here.
type Proxy struct {
	surface         ttyapi.Surface
	closeErr        error
	process         execapi.PTYProcess
	screen          *vt.SafeEmulator
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
	cursorVisible   atomic.Bool
	started         bool
}

// RequestClose accepts asynchronous shutdown intent. Run owns termination and
// reaping; the immediate signal also interrupts a blocked terminal write.
func (p *Proxy) RequestClose() {
	p.closeRequested.Store(true)
	p.closeNotifyOnce.Do(func() { close(p.closeNotify) })
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.started {
		_ = p.signalCloseLocked()
	}
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
	p.started = true
	if p.closeRequested.Load() {
		_ = p.signalCloseLocked()
	}
	return nil
}

func (p *Proxy) signalCloseLocked() error {
	p.closeSignalOnce.Do(func() {
		p.closeErr = p.process.Signal(int(syscall.SIGTERM))
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
		process: process, surface: surface, screen: vt.NewSafeEmulator(width, height),
		shutdownGrace: defaultShutdownGrace, closeNotify: make(chan struct{}),
	}
	// Retain a bounded primary-screen history for nested surfaces. Physical
	// terminals normally provide their own scrollback, but a virtual surface
	// cannot. The cap avoids x/vt's costly default 10,000-line retention.
	p.resizeScrollbackLocked(width)
	p.height.Store(int64(height))
	p.cursorVisible.Store(true)
	p.input.init()
	p.screen.SetCallbacks(vt.Callbacks{
		EnableMode: p.input.enable, DisableMode: p.input.disable,
		CursorVisibility: p.cursorVisible.Store,
	})
	p.installKeyboardHandlers()
	p.installPageHandlers()
	return p, nil
}

func (p *Proxy) present() error {
	p.screenMu.Lock()
	height := int(p.height.Load())
	rows, scrolled := p.rowsLocked(height)
	position := p.screen.CursorPosition()
	width := p.screen.Width()
	visible := p.cursorVisible.Load() && !scrolled
	p.screenMu.Unlock()
	frame := ttyapi.Frame{Rows: rows, Cursor: &ttyapi.Cursor{
		Column:  min(max(position.X, 0), width-1),
		Row:     min(max(position.Y, 0), height-1),
		Visible: visible,
	}}
	_, err := p.surface.Present(frame)
	return err
}

// rowsLocked renders the live screen unless the primary-screen viewport has
// been moved into scrollback. screenMu must be held by the caller.
func (p *Proxy) rowsLocked(height int) ([]string, bool) {
	if p.input.altScreen.Load() {
		p.viewOffset = 0
		return paddedRows(p.screen.Render(), height), false
	}

	history := p.screen.ScrollbackLen()
	p.viewOffset = min(p.viewOffset, history)
	if p.viewOffset == 0 {
		return paddedRows(p.screen.Render(), height), false
	}

	width := p.screen.Width()
	start := history - p.viewOffset
	rows := make([]string, height)
	for y := range rows {
		line := uv.NewLine(width)
		lineIndex := start + y
		for x := range width {
			var cell *uv.Cell
			if lineIndex < history {
				cell = p.screen.ScrollbackCellAt(x, lineIndex)
			} else {
				cell = p.screen.CellAt(x, lineIndex-history)
			}
			if cell != nil {
				line[x] = *cell.Clone()
			}
		}
		rows[y] = line.Render()
	}
	return rows, true
}

func paddedRows(rendered string, height int) []string {
	rows := strings.Split(rendered, "\n")
	if len(rows) > height {
		rows = rows[:height]
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return rows
}

func scrollbackSize(width int) int {
	return min(maxScrollbackLines, max(scrollbackCellBudget/max(width, 1), 1))
}

// resizeScrollbackLocked retains as much recent history as fits both the
// current line cap and cell budget. x/vt stores old lines at their original
// widths, so changing from a very wide terminal to a narrow one needs more
// than simply recomputing the number of retained lines. Once the proxy is
// live, callers hold screenMu while changing this state.
func (p *Proxy) resizeScrollbackLocked(width int) {
	target := scrollbackSize(width)
	p.screen.SetScrollbackSize(target)

	lines := p.screen.Scrollback().Lines()
	used, retained := 0, 0
	for index := len(lines) - 1; index >= 0; index-- {
		cells := len(lines[index])
		if cells > scrollbackCellBudget-used {
			break
		}
		used += cells
		retained++
	}
	if len(lines) == 0 {
		return
	}
	if retained == 0 {
		p.screen.ClearScrollback()
		return
	}
	// Existing wide rows consume part of the budget. Bound the number of new
	// rows at the current width, without paying to rescan history on output.
	// SetMaxLines only removes old entries, so trim first if necessary and then
	// install the conservative cap for subsequent output at this geometry.
	capacity := min(target, retained+(scrollbackCellBudget-used)/max(width, 1))
	if retained < len(lines) {
		p.screen.SetScrollbackSize(retained)
	}
	p.screen.SetScrollbackSize(capacity)
}
