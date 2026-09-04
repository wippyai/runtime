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

type inputState struct {
	keyboard       keyboardState
	appCursor      atomic.Bool
	bracketedPaste atomic.Bool
	focusEvents    atomic.Bool
	mouseEnabled   atomic.Bool
	mouseSGR       atomic.Bool
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
		return p.write(text)
	case "focus":
		if p.input.focusEvents.Load() {
			if event.Focused {
				return p.write("\x1b[I")
			}
			return p.write("\x1b[O")
		}
	case "key":
		return p.write(p.input.key(event))
	case "mouse":
		return p.write(p.input.mouse(event))
	}
	return nil
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

var cursorKeys = map[string]string{"up": "A", "down": "B", "right": "C", "left": "D"}
var fixedKeys = map[string]string{
	"enter": "\r", "tab": "\t", "backspace": "\x7f", "esc": "\x1b", "space": " ",
	"insert": "\x1b[2~", "delete": "\x1b[3~", "home": "\x1b[H", "end": "\x1b[F",
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
		return ""
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
