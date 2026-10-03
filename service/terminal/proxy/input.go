// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"strings"

	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/tty/vt"
)

func (p *Proxy) handle(event ttyapi.Event) error {
	switch event.Type {
	case "resize":
		if err := execapi.ValidatePTYSize(event.Width, event.Height); err != nil {
			return err
		}
		p.screenMu.Lock()
		p.height.Store(int64(event.Height))
		p.screen.SetScrollbackLimit(scrollbackSize(event.Width))
		p.screen.Resize(event.Width, event.Height)
		p.viewOffset = min(p.viewOffset, p.historyLenLocked())
		p.screenMu.Unlock()
		if err := p.process.Resize(event.Width, event.Height); err != nil {
			return err
		}
		return p.present()
	case "paste":
		return p.writeLive(p.encode(1, func(t *vt.Terminal) { t.SendPaste(event.Paste) }))
	case "focus":
		return p.write(p.encode(1, func(t *vt.Terminal) { t.SendFocus(event.Focused) }))
	case "key":
		key, ok := keyEvent(event)
		if !ok {
			return nil
		}
		return p.writeLive(p.encode(1, func(t *vt.Terminal) { t.SendKey(key) }))
	case "mouse":
		return p.mouse(event)
	}
	return nil
}

// mouse delivers a pointer event to a mouse-tracking child, translates the
// wheel into cursor keys for an alternate-screen child, and otherwise scrolls
// the proxy's own history.
func (p *Proxy) mouse(event ttyapi.Event) error {
	mouse, ok := mouseEvent(event)
	if !ok {
		return nil
	}
	p.screenMu.Lock()
	tracking := p.screen.Modes().MouseTracking != vt.MouseOff
	alternate := p.screen.Screen().Alternate()
	p.screenMu.Unlock()
	switch {
	case tracking:
		return p.writeLive(p.encode(1, func(t *vt.Terminal) { t.SendMouse(mouse) }))
	case alternate:
		return p.write(p.encode(scrollLinesPerWheel, func(t *vt.Terminal) { t.SendMouse(mouse) }))
	}
	return p.scroll(event)
}

// encode runs send count times against the emulator and returns the bytes it
// wrote for the child.
func (p *Proxy) encode(count int, send func(*vt.Terminal)) string {
	var encoded strings.Builder
	p.screenMu.Lock()
	defer p.screenMu.Unlock()
	p.capture = &encoded
	defer func() { p.capture = nil }()
	for range count {
		send(p.screen)
	}
	return encoded.String()
}

// writeLive returns a history viewport to the live screen before delivering
// user input. Output keeps a history viewport stable while the retained
// history is still growing; after eviction the buffer exposes no line identity
// with which to anchor a viewport. A resize keeps it where possible (clamped to
// retained history).
func (p *Proxy) writeLive(sequence string) error {
	if sequence == "" {
		return nil
	}
	p.screenMu.Lock()
	changed := p.viewOffset != 0
	p.viewOffset = 0
	p.screenMu.Unlock()
	if changed {
		if err := p.present(); err != nil {
			return err
		}
	}
	return p.write(sequence)
}

// scroll consumes a primary-screen wheel event for the proxy's native history.
// Mouse-tracking children and alternate-screen applications are handled before
// this function is called.
func (p *Proxy) scroll(event ttyapi.Event) error {
	if event.Action != "wheel" {
		return nil
	}
	var delta int
	switch event.Button {
	case "wheel_up":
		delta = scrollLinesPerWheel
	case "wheel_down":
		delta = -scrollLinesPerWheel
	default:
		return nil
	}
	p.screenMu.Lock()
	old := p.viewOffset
	p.viewOffset = min(max(p.viewOffset+delta, 0), p.historyLenLocked())
	changed := p.viewOffset != old
	p.screenMu.Unlock()
	if !changed {
		return nil
	}
	return p.present()
}

func (p *Proxy) write(sequence string) error {
	if sequence == "" {
		return nil
	}
	return p.writeBytes([]byte(sequence))
}

func (p *Proxy) writeBytes(data []byte) error {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	return p.process.WriteStdin(data)
}
