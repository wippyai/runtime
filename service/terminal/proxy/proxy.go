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

	vt "github.com/charmbracelet/x/vt"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var (
	ErrInvalidProxy    = errors.New("terminal proxy requires a process, surface, and bounded positive size")
	ErrShutdownTimeout = errors.New("PTY process did not exit after forced shutdown")
)

const retainedScrollbackLines = 1

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
	// Proxy presents only the current terminal frame; its public contract has
	// no scrollback viewport. x/vt otherwise retains 10,000 cloned lines and
	// shifts that history for every line after it fills. Keep the one line its
	// scroll machinery requires without retaining inaccessible history.
	p.screen.SetScrollbackSize(retainedScrollbackLines)
	p.height.Store(int64(height))
	p.cursorVisible.Store(true)
	p.screen.SetCallbacks(vt.Callbacks{
		EnableMode: p.input.enable, DisableMode: p.input.disable,
		CursorVisibility: p.cursorVisible.Store,
	})
	p.installKeyboardHandlers()
	return p, nil
}

func (p *Proxy) present() error {
	p.screenMu.Lock()
	rows := strings.Split(p.screen.Render(), "\n")
	height := int(p.height.Load())
	if len(rows) > height {
		rows = rows[:height]
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	position := p.screen.CursorPosition()
	width := p.screen.Width()
	visible := p.cursorVisible.Load()
	p.screenMu.Unlock()
	frame := ttyapi.Frame{Rows: rows, Cursor: &ttyapi.Cursor{
		Column:  min(max(position.X, 0), width-1),
		Row:     min(max(position.Y, 0), height-1),
		Visible: visible,
	}}
	_, err := p.surface.Present(frame)
	return err
}
