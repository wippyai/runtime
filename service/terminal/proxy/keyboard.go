// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"io"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// keyboardState records protocols negotiated by the child terminal. x/vt
// intentionally does not encode Kitty/CSI-u or modifyOtherKeys yet, so the
// proxy owns this input-side state until the emulator exposes it natively.
type keyboardState struct {
	kitty           []int
	modifyOtherKeys int
	mu              sync.RWMutex
}

func (s *keyboardState) flags() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.kitty) == 0 {
		return 0
	}
	return s.kitty[len(s.kitty)-1]
}

func (s *keyboardState) push(flags int) {
	s.mu.Lock()
	s.kitty = append(s.kitty, flags&ansi.KittyAllFlags)
	s.mu.Unlock()
}

func (s *keyboardState) pop(count int) {
	s.mu.Lock()
	if count < 1 {
		count = 1
	}
	if count >= len(s.kitty) {
		s.kitty = nil
	} else {
		s.kitty = s.kitty[:len(s.kitty)-count]
	}
	s.mu.Unlock()
}

func (s *keyboardState) set(flags, mode int) {
	s.mu.Lock()
	if len(s.kitty) == 0 {
		s.kitty = append(s.kitty, 0)
	}
	current := s.kitty[len(s.kitty)-1]
	switch mode {
	case 2:
		current |= flags
	case 3:
		current &^= flags
	default:
		current = flags
	}
	s.kitty[len(s.kitty)-1] = current & ansi.KittyAllFlags
	s.mu.Unlock()
}

func (s *keyboardState) setModifyOtherKeys(level int) {
	s.mu.Lock()
	s.modifyOtherKeys = max(0, min(level, 2))
	s.mu.Unlock()
}

func (s *keyboardState) encode(event ttyapi.Event) (string, bool) {
	s.mu.RLock()
	flags := 0
	if len(s.kitty) != 0 {
		flags = s.kitty[len(s.kitty)-1]
	}
	modifyOtherKeys := s.modifyOtherKeys
	s.mu.RUnlock()
	if flags != 0 {
		name := event.KeyType
		if name == "" || name == "runes" {
			name = event.Key
		}
		_, special := kittyKeyCodes[name]
		_, functional := kittyFunctionalKeys[name]
		special = special || functional
		plainText := !special && !event.Shift && !event.Alt && !event.Ctrl &&
			event.Action != "release" && flags&(ansi.KittyReportAllKeysAsEscapeCodes|ansi.KittyReportEventTypes) == 0
		if plainText {
			return "", false
		}
		return encodeKittyKey(event, flags), true
	}
	if modifyOtherKeys == 2 && event.Action != "release" && (event.Shift || event.Alt || event.Ctrl) {
		name := event.KeyType
		if name == "" || name == "runes" {
			name = event.Key
		}
		if _, special := kittyKeyCodes[name]; special {
			return "", false
		}
		if _, functional := kittyFunctionalKeys[name]; functional {
			return "", false
		}
		if code, ok := keyCode(event); ok {
			return "\x1b[27;" + strconv.Itoa(modifier(event)) + ";" + strconv.Itoa(code) + "~", true
		}
	}
	return "", false
}

func encodeKittyKey(event ttyapi.Event, flags int) string {
	if event.Action == "release" && flags&ansi.KittyReportEventTypes == 0 {
		return ""
	}
	code, ok := keyCode(event)
	if !ok {
		return ""
	}
	mods := modifier(event)
	name := event.KeyType
	if name == "" || name == "runes" {
		name = event.Key
	}
	// These controls retain their legacy press encoding until report-all is
	// requested, including when event-type reporting is enabled.
	if flags&ansi.KittyReportAllKeysAsEscapeCodes == 0 && (name == "enter" || name == "tab" || name == "backspace") {
		if event.Action == "release" {
			return ""
		}
		if mods == 1 {
			return fixedKeys[name]
		}
	}
	eventType := 1
	if event.Action == "release" {
		eventType = 3
	}
	final := byte('u')
	if key, ok := kittyFunctionalKeys[name]; ok {
		code, final = key.code, key.final
	}
	params := strconv.Itoa(code)
	if final != 'u' && final != '~' && mods == 1 && flags&ansi.KittyReportEventTypes == 0 {
		params = ""
	}
	if mods != 1 || flags&ansi.KittyReportEventTypes != 0 {
		params += ";" + strconv.Itoa(mods)
		if flags&ansi.KittyReportEventTypes != 0 {
			params += ":" + strconv.Itoa(eventType)
		}
	}
	return "\x1b[" + params + string(final)
}

// Functional keys retain their specified CSI final byte under Kitty
// enhancements. Private-use CSI-u numbers are not their wire encoding.
var kittyFunctionalKeys = map[string]struct {
	code  int
	final byte
}{
	"insert": {2, '~'}, "delete": {3, '~'},
	"left": {1, 'D'}, "right": {1, 'C'}, "up": {1, 'A'}, "down": {1, 'B'},
	"home": {1, 'H'}, "end": {1, 'F'}, "pgup": {5, '~'}, "pgdown": {6, '~'},
	"f1": {1, 'P'}, "f2": {1, 'Q'}, "f3": {13, '~'}, "f4": {1, 'S'},
	"f5": {15, '~'}, "f6": {17, '~'}, "f7": {18, '~'}, "f8": {19, '~'},
	"f9": {20, '~'}, "f10": {21, '~'}, "f11": {23, '~'}, "f12": {24, '~'},
}

func keyCode(event ttyapi.Event) (int, bool) {
	name := event.KeyType
	if name == "" || name == "runes" {
		name = event.Key
	}
	if code, ok := kittyKeyCodes[name]; ok {
		return code, true
	}
	if key, ok := kittyFunctionalKeys[name]; ok {
		return key.code, true
	}
	if event.Key == "" {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(event.Key)
	if r == utf8.RuneError {
		return 0, false
	}
	return int(r), true
}

var kittyKeyCodes = map[string]int{
	"esc": 27, "enter": 13, "tab": 9, "backspace": 127, "space": 32,
}

func (p *Proxy) installKeyboardHandlers() {
	p.screen.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(ansi.Params) bool {
		_, _ = io.WriteString(p.screen.InputPipe(), "\x1b[?"+
			strconv.Itoa(p.input.keyboard.flags())+"u")
		return true
	})
	p.screen.RegisterCsiHandler(ansi.Command('>', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		p.input.keyboard.push(flags)
		return true
	})
	p.screen.RegisterCsiHandler(ansi.Command('<', 0, 'u'), func(params ansi.Params) bool {
		count, _, _ := params.Param(0, 1)
		p.input.keyboard.pop(count)
		return true
	})
	p.screen.RegisterCsiHandler(ansi.Command('=', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		mode, _, _ := params.Param(1, 1)
		p.input.keyboard.set(flags, mode)
		return true
	})
	p.screen.RegisterCsiHandler(ansi.Command('>', 0, 'm'), func(params ansi.Params) bool {
		resource, _, _ := params.Param(0, 0)
		if resource != 4 {
			return false
		}
		level, _, _ := params.Param(1, 0)
		p.input.keyboard.setModifyOtherKeys(level)
		return true
	})
}
