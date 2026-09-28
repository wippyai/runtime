// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// modeAlternateScroll is xterm's alternate scroll mode; the vendored ansi
// package defines no constant for it.
const modeAlternateScroll = ansi.DECMode(1007)

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
	mouseSGR       atomic.Bool
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
		p.screen.Resize(event.Width, event.Height)
		p.resizeScrollbackLocked(event.Width)
		p.viewOffset = min(p.viewOffset, p.screen.ScrollbackLen())
		p.screenMu.Unlock()
		if err := p.process.Resize(event.Width, event.Height); err != nil {
			return err
		}
		return p.present()
	case "paste":
		text := event.Paste
		if p.input.bracketedPaste.Load() {
			text = ansi.BracketedPasteStart + text + ansi.BracketedPasteEnd
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
			return p.writeLive(p.input.mouse(event))
		}
		if p.input.altScreen.Load() {
			return p.write(p.input.alternateScroll(event))
		}
		return p.scroll(event)
	}
	return nil
}

// writeLive returns a history viewport to the live screen before delivering
// user input. Output keeps a history viewport stable while x/vt's retained
// history is still growing; after eviction x/vt exposes no line identity with
// which to anchor a viewport. A resize keeps it where possible (clamped to
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
	p.viewOffset = min(max(p.viewOffset+delta, 0), p.screen.ScrollbackLen())
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

func (s *inputState) mouse(event ttyapi.Event) string {
	if !s.mouseEnabled.Load() {
		return s.alternateScroll(event)
	}
	button, ok := mouseButtons[event.Button]
	if !ok {
		return ""
	}
	release, motion := event.Action == "release", event.Action == "motion"
	encoded := ansi.EncodeMouseButton(button, motion, event.Shift, event.Alt, event.Ctrl)
	// Public tty events are one-based; ANSI helpers accept zero-based cells
	// and add the protocol offset while encoding.
	x, y := event.X-1, event.Y-1
	if s.mouseSGR.Load() {
		return ansi.MouseSgr(encoded, x, y, release)
	}
	return ansi.MouseX10(encoded, x, y)
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

var mouseButtons = map[string]ansi.MouseButton{
	"none": ansi.MouseNone, "left": ansi.MouseLeft, "middle": ansi.MouseMiddle,
	"right": ansi.MouseRight, "wheel_up": ansi.MouseWheelUp, "wheel_down": ansi.MouseWheelDown,
}

func (s *inputState) enable(mode ansi.Mode)  { s.set(mode, true) }
func (s *inputState) disable(mode ansi.Mode) { s.set(mode, false) }
func (s *inputState) set(mode ansi.Mode, enabled bool) {
	switch mode {
	case ansi.ModeCursorKeys:
		s.appCursor.Store(enabled)
	case ansi.ModeAltScreen, ansi.ModeAltScreenSaveCursor:
		s.altScreen.Store(enabled)
	case modeAlternateScroll:
		s.altScroll.Store(enabled)
	case ansi.ModeBracketedPaste:
		s.bracketedPaste.Store(enabled)
	case ansi.ModeFocusEvent:
		s.focusEvents.Store(enabled)
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		s.mouseEnabled.Store(enabled)
	case ansi.ModeMouseExtSgr:
		s.mouseSGR.Store(enabled)
	}
}
