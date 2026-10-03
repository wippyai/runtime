// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	xterm "github.com/gitpod-io/xterm-go"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

const (
	// modeAlternateScroll is xterm's alternate scroll mode (DECSET 1007).
	modeAlternateScroll = 1007

	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// xterm delivers three cursor key presses per wheel notch under alternate scroll.
const alternateScrollLines = 3

type inputState struct {
	keyboard       keyboardState
	appCursor      atomic.Bool
	altScreen      atomic.Bool
	altScroll      atomic.Bool
	bracketedPaste atomic.Bool
	focusEvents    atomic.Bool
	mouseEnabled   atomic.Bool
}

// init applies the power-on mode defaults that differ from the zero value.
func (s *inputState) init() {
	s.altScroll.Store(true)
}

func (p *Proxy) handle(event ttyapi.Event) error {
	switch event.Type {
	case "resize":
		if err := execapi.ValidatePTYSize(event.Width, event.Height); err != nil {
			return err
		}
		p.screenMu.Lock()
		p.height.Store(int64(event.Height))
		// Widening multiplies the cells of every retained line, so history is
		// bounded for the new width first. The emulator then reflows within
		// its reflow capacity and history is bounded again for the new rows.
		p.boundHistoryLocked(event.Width)
		p.screen.Resize(event.Width, event.Height)
		p.boundHistoryLocked(event.Width)
		p.syncModesLocked()
		p.viewOffset = min(p.viewOffset, p.historyLenLocked())
		p.screenMu.Unlock()
		if err := p.process.Resize(event.Width, event.Height); err != nil {
			return err
		}
		return p.present()
	case "paste":
		text := event.Paste
		if p.input.bracketedPaste.Load() {
			text = bracketedPasteStart + text + bracketedPasteEnd
		}
		return p.writeLive(text)
	case "focus":
		if p.input.focusEvents.Load() {
			if event.Focused {
				return p.write("\x1b[I")
			}
			return p.write("\x1b[O")
		}
	case "key":
		return p.writeLive(p.input.key(event))
	case "mouse":
		if p.input.mouseEnabled.Load() {
			return p.writeLive(p.encodeMouse(event))
		}
		if p.input.altScreen.Load() {
			return p.write(p.input.alternateScroll(event))
		}
		return p.scroll(event)
	}
	return nil
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

func (s *inputState) key(event ttyapi.Event) string {
	if sequence, handled := s.keyboard.encode(event); handled {
		return sequence
	}
	if event.Action == "release" {
		return ""
	}
	name := event.KeyType
	if name == "" || name == "runes" {
		name = event.Key
	}
	if final, ok := cursorKeys[name]; ok {
		if event.Shift || event.Alt || event.Ctrl {
			return "\x1b[1;" + strconv.Itoa(modifier(event)) + final
		}
		if s.appCursor.Load() {
			return "\x1bO" + final
		}
		return "\x1b[" + final
	}
	if sequence, ok := fixedKeys[name]; ok {
		if name == "tab" && event.Shift {
			if event.Alt || event.Ctrl {
				return "\x1b[1;" + strconv.Itoa(modifier(event)) + "Z"
			}
			return "\x1b[Z"
		}
		if event.Shift || event.Alt || event.Ctrl {
			if strings.HasPrefix(sequence, "\x1b[") && strings.HasSuffix(sequence, "~") {
				return strings.TrimSuffix(sequence, "~") + ";" + strconv.Itoa(modifier(event)) + "~"
			}
			if strings.HasPrefix(sequence, "\x1bO") {
				return "\x1b[1;" + strconv.Itoa(modifier(event)) + sequence[2:]
			}
		}
		if event.Alt {
			return "\x1b" + sequence
		}
		return sequence
	}
	if event.Key == "" {
		return ""
	}
	sequence := event.Key
	if event.Ctrl {
		r, _ := utf8.DecodeRuneInString(strings.ToLower(event.Key))
		if r >= '@' && r <= '_' || r >= 'a' && r <= 'z' {
			sequence = string(byte(r) & 0x1f)
		}
	}
	if event.Alt {
		sequence = "\x1b" + sequence
	}
	return sequence
}

var cursorKeys = map[string]string{"up": "A", "down": "B", "right": "C", "left": "D", "home": "H", "end": "F"}
var fixedKeys = map[string]string{
	"enter": "\r", "tab": "\t", "backspace": "\x7f", "esc": "\x1b", "space": " ",
	"insert": "\x1b[2~", "delete": "\x1b[3~",
	"pgup": "\x1b[5~", "pgdown": "\x1b[6~", "f1": "\x1bOP", "f2": "\x1bOQ",
	"f3": "\x1bOR", "f4": "\x1bOS", "f5": "\x1b[15~", "f6": "\x1b[17~",
	"f7": "\x1b[18~", "f8": "\x1b[19~", "f9": "\x1b[20~", "f10": "\x1b[21~",
	"f11": "\x1b[23~", "f12": "\x1b[24~",
}

func modifier(event ttyapi.Event) int {
	value := 1
	if event.Shift {
		value++
	}
	if event.Alt {
		value += 2
	}
	if event.Ctrl {
		value += 4
	}
	return value
}

// encodeMouse encodes a public tty mouse event with the child's negotiated
// tracking protocol and coordinate encoding. Public coordinates are one-based,
// as the wire protocols are.
func (p *Proxy) encodeMouse(event ttyapi.Event) string {
	core := xterm.CoreMouseEvent{
		Col: event.X, Row: event.Y, Shift: event.Shift, Alt: event.Alt, Ctrl: event.Ctrl,
	}
	switch event.Button {
	case "none":
		core.Button = xterm.MouseButtonNone
	case "left":
		core.Button = xterm.MouseButtonLeft
	case "middle":
		core.Button = xterm.MouseButtonMiddle
	case "right":
		core.Button = xterm.MouseButtonRight
	case "wheel_up":
		core.Button, core.Action = xterm.MouseButtonWheel, xterm.MouseActionUp
	case "wheel_down":
		core.Button, core.Action = xterm.MouseButtonWheel, xterm.MouseActionDown
	default:
		return ""
	}
	if core.Button != xterm.MouseButtonWheel {
		switch event.Action {
		case "press":
			core.Action = xterm.MouseActionDown
		case "release":
			core.Action = xterm.MouseActionUp
		case "motion":
			core.Action = xterm.MouseActionMove
		default:
			return ""
		}
	}
	var encoded strings.Builder
	p.screenMu.Lock()
	p.capture = &encoded
	p.screen.TriggerMouseEvent(core)
	p.capture = nil
	p.screenMu.Unlock()
	return encoded.String()
}

// alternateScroll implements xterm's DECSET 1007: while the alternate screen
// buffer is active and mouse tracking is off, a wheel notch reaches the child
// as cursor key presses. On the main screen the wheel belongs to the host
// terminal's own scrollback.
func (s *inputState) alternateScroll(event ttyapi.Event) string {
	if !s.altScreen.Load() || !s.altScroll.Load() {
		return ""
	}
	var name string
	switch event.Button {
	case "wheel_up":
		name = "up"
	case "wheel_down":
		name = "down"
	default:
		return ""
	}
	prefix := "\x1b["
	if s.appCursor.Load() {
		prefix = "\x1bO"
	}
	return strings.Repeat(prefix+cursorKeys[name], alternateScrollLines)
}

// reply routes emulator output to the child. While a mouse event is being
// encoded the output is the encoded event itself.
func (p *Proxy) reply(data string) {
	if p.capture != nil {
		p.capture.WriteString(data)
		return
	}
	p.responses.push([]byte(data))
}

// syncModesLocked publishes the emulator's input-affecting mode state for
// lock-free reads by the input path. screenMu must be held by the caller.
func (p *Proxy) syncModesLocked() {
	modes := p.screen.DecPrivateModes()
	s := &p.input
	s.appCursor.Store(modes.ApplicationCursorKeys)
	s.altScreen.Store(p.screen.IsAltBufferActive())
	s.bracketedPaste.Store(modes.BracketedPasteMode)
	s.focusEvents.Store(modes.SendFocus)
	s.mouseEnabled.Store(modes.MouseTrackingMode != "" && modes.MouseTrackingMode != "NONE")
}

// installModeHandlers tracks DECSET 1007, which the emulator does not
// implement. The handlers observe the sequence and leave it to the emulator.
func (p *Proxy) installModeHandlers() {
	for _, final := range []byte{'h', 'l'} {
		enabled := final == 'h'
		p.screen.RegisterCsiHandler(xterm.FunctionIdentifier{Prefix: '?', Final: final}, func(params *xterm.Params) bool {
			for i := 0; i < params.Length; i++ {
				if params.Params[i] == modeAlternateScroll {
					p.input.altScroll.Store(enabled)
				}
			}
			return false
		})
	}
}
