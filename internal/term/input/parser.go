// SPDX-License-Identifier: MPL-2.0

package input

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const (
	esc = 0x1b
	bel = 0x07
	can = 0x18
	sub = 0x1a
)

var pasteEnd = []byte("\x1b[201~")

// Parser decodes a terminal input byte stream into events. Input may arrive in
// arbitrary fragments: a sequence split across Feed calls is completed by
// later bytes. A Parser is not safe for concurrent use.
//
// A bare ESC is ambiguous with the first byte of a longer sequence. The parser
// keeps it pending and the caller resolves it with ResolveEscape once the read
// that delivered it has been fully fed. No timer is involved: a read that ends
// in ESC is the Escape key, an ESC followed by more bytes in the same read is
// the start of a sequence.
type Parser struct {
	buf     []byte
	scan    int
	inPaste bool
	console ConsoleDecoder
}

// NewParser returns an empty Parser.
func NewParser() *Parser { return &Parser{} }

// Feed appends data to the stream and emits every event that is complete.
func (p *Parser) Feed(data []byte, emit func(Event)) {
	p.buf = append(p.buf, data...)
	off := 0
	for off < len(p.buf) {
		if p.inPaste {
			n, ok := p.consumePaste(p.buf[off:], emit)
			if !ok {
				break
			}
			off += n
			continue
		}
		n, ev, ok := parseOne(p.buf[off:])
		if !ok {
			break
		}
		off += n
		switch e := ev.(type) {
		case pasteStart:
			p.inPaste = true
			p.scan = 0
		case win32Key:
			for _, out := range p.console.Key(e.rec) {
				emit(out)
			}
		default:
			emit(ev)
		}
	}
	rest := copy(p.buf, p.buf[off:])
	p.buf = p.buf[:rest]
}

// consumePaste looks for the end marker of the paste in b. When found it emits
// the paste and returns the bytes consumed; otherwise it reports ok=false and
// keeps accumulating.
func (p *Parser) consumePaste(b []byte, emit func(Event)) (int, bool) {
	from := max(p.scan-len(pasteEnd)+1, 0)
	idx := bytes.Index(b[from:], pasteEnd)
	if idx < 0 {
		p.scan = len(b)
		return 0, false
	}
	idx += from
	emit(PasteEvent(strings.ToValidUTF8(string(b[:idx]), "")))
	p.inPaste = false
	p.scan = 0
	return idx + len(pasteEnd), true
}

// ResolveEscape reports a pending bare ESC as the Escape key, ESC ESC as
// Alt+Escape, and ESC followed by O or a string introducer (P ] _ ^ X) as Alt plus
// that key. Other partial sequences stay pending.
func (p *Parser) ResolveEscape(emit func(Event)) {
	if p.inPaste {
		return
	}
	switch {
	case len(p.buf) == 1 && p.buf[0] == esc:
		p.buf = p.buf[:0]
		emit(KeyPressEvent{Code: KeyEscape})
	case len(p.buf) == 2 && p.buf[0] == esc && p.buf[1] == esc:
		p.buf = p.buf[:0]
		emit(KeyPressEvent{Code: KeyEscape, Mod: ModAlt})
	case len(p.buf) == 2 && p.buf[0] == esc && bytes.IndexByte([]byte("]P_^XO"), p.buf[1]) >= 0:
		// A terminal writes a SS3 or string sequence whole, so a read ending
		// right after its introducer is Alt plus that key. ESC [ is excluded
		// because a CSI split after the introducer is still in use.
		k := p.buf[1]
		p.buf = p.buf[:0]
		switch k {
		case 'P', 'X', 'O':
			emit(KeyPressEvent{Code: rune(k) + 32, ShiftedCode: rune(k), Mod: ModShift | ModAlt})
		default:
			emit(KeyPressEvent{Code: rune(k), Mod: ModAlt})
		}
	}
}

// Flush ends the stream: a pending escape is resolved and any other partial
// sequence is discarded.
func (p *Parser) Flush(emit func(Event)) {
	p.ResolveEscape(emit)
	p.buf = p.buf[:0]
	p.inPaste = false
	p.scan = 0
}

// Pending returns the number of bytes held for an incomplete sequence.
func (p *Parser) Pending() int { return len(p.buf) }

type pasteStart struct{}

func (pasteStart) isEvent() {}

// parseOne decodes the first event of b. ok is false when b holds the prefix
// of a longer sequence.
func parseOne(b []byte) (n int, ev Event, ok bool) {
	c := b[0]
	switch {
	case c == esc:
		return parseEscape(b)
	case c <= 0x1f || c == 0x7f || c == ' ':
		return 1, controlKey(c), true
	case c < 0x7f:
		return 1, asciiKey(rune(c)), true
	}
	return parseUTF8(b)
}

func controlKey(c byte) Event {
	switch c {
	case 0x00:
		return KeyPressEvent{Code: KeySpace, Mod: ModCtrl}
	case 0x08:
		return KeyPressEvent{Code: 'h', Mod: ModCtrl}
	case '\t':
		return KeyPressEvent{Code: KeyTab}
	case '\r':
		return KeyPressEvent{Code: KeyEnter}
	case 0x7f:
		return KeyPressEvent{Code: KeyBackspace}
	case ' ':
		return KeyPressEvent{Code: KeySpace, Text: " "}
	}
	if c <= 0x1a {
		return KeyPressEvent{Code: rune(c) + 0x60, Mod: ModCtrl}
	}
	return KeyPressEvent{Code: rune(c) + 0x40, Mod: ModCtrl}
}

func asciiKey(r rune) Event {
	k := Key{Code: r, Text: string(r)}
	if unicode.IsUpper(r) {
		k.Code = unicode.ToLower(r)
		k.ShiftedCode = r
		k.Mod = ModShift
	}
	return KeyPressEvent(k)
}

func parseUTF8(b []byte) (int, Event, bool) {
	if !utf8.FullRune(b) {
		return 0, nil, false
	}
	r, w := utf8.DecodeRune(b)
	if r == utf8.RuneError && w <= 1 {
		return 1, UnknownEvent(b[:1]), true
	}
	cluster, _, _, _ := uniseg.FirstGraphemeCluster(b, -1)
	k := Key{Code: r, Text: string(cluster)}
	switch {
	case len(cluster) > w:
		k.Code = KeyExtended
	case unicode.IsUpper(r):
		k.Code = unicode.ToLower(r)
		k.ShiftedCode = r
		k.Mod = ModShift
	}
	return len(cluster), KeyPressEvent(k), true
}

func parseEscape(b []byte) (int, Event, bool) {
	if len(b) == 1 {
		return 0, nil, false
	}
	switch b[1] {
	case '[':
		return parseCSI(b)
	case 'O':
		return parseSS3(b)
	case ']', 'P', '_', '^', 'X':
		if len(b) == 2 {
			return 0, nil, false
		}
		return parseString(b)
	case esc:
		if len(b) == 2 {
			return 0, nil, false
		}
	}
	n, ev, ok := parseOne(b[1:])
	if !ok {
		return 0, nil, false
	}
	if k, isKey := ev.(KeyPressEvent); isKey {
		k.Text = ""
		k.Mod |= ModAlt
		return n + 1, k, true
	}
	return 1, KeyPressEvent{Code: KeyEscape}, true
}

func parseSS3(b []byte) (int, Event, bool) {
	i := 2
	mod := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		mod = mod*10 + int(b[i]-'0')
		i++
	}
	if i >= len(b) {
		return 0, nil, false
	}
	gl := b[i]
	if gl < 0x21 || gl > 0x7e {
		return i, UnknownEvent(b[:i]), true
	}
	i++
	var k Key
	switch gl {
	case 'a', 'b', 'c', 'd':
		k = Key{Code: KeyUp + rune(gl-'a'), Mod: ModCtrl}
	case 'A', 'B', 'C', 'D':
		k = Key{Code: KeyUp + rune(gl-'A')}
	case 'E':
		k = Key{Code: KeyBegin}
	case 'F':
		k = Key{Code: KeyEnd}
	case 'H':
		k = Key{Code: KeyHome}
	case 'P', 'Q', 'R', 'S':
		k = Key{Code: KeyF1 + rune(gl-'P')}
	case 'M':
		k = Key{Code: KeyKpEnter}
	case 'X':
		k = Key{Code: KeyKpEqual}
	case 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y':
		k = Key{Code: KeyKpMultiply + rune(gl-'j')}
	default:
		return i, UnknownEvent(b[:i]), true
	}
	if mod > 1 {
		k.Mod |= xtermMod(mod)
	}
	return i, KeyPressEvent(k), true
}

// parseString consumes an OSC, DCS, APC, PM or SOS string. APC G replies are
// decoded as kitty graphics events; every other string is reported unknown.
func parseString(b []byte) (int, Event, bool) {
	intro := b[1]
	const start = 2
	for i := start; i < len(b); i++ {
		switch b[i] {
		case esc:
			if i+1 >= len(b) {
				return 0, nil, false
			}
			if b[i+1] != '\\' {
				return i, UnknownEvent(b[:i]), true
			}
			end := i + 2
			if intro == '_' {
				if ev, ok := graphicsReply(b[start:i]); ok {
					return end, ev, true
				}
			}
			return end, UnknownEvent(b[:end]), true
		case bel:
			if intro == ']' {
				return i + 1, UnknownEvent(b[:i+1]), true
			}
		case can, sub:
			return i + 1, UnknownEvent(b[:i+1]), true
		}
	}
	return 0, nil, false
}

func graphicsReply(body []byte) (Event, bool) {
	if len(body) == 0 || body[0] != 'G' {
		return nil, false
	}
	opts, payload, _ := bytes.Cut(body[1:], []byte{';'})
	g := GraphicsEvent{Payload: append([]byte(nil), payload...)}
	for _, kv := range bytes.Split(opts, []byte{','}) {
		if key, val, found := bytes.Cut(kv, []byte{'='}); found && string(key) == "i" {
			if id, err := strconv.Atoi(string(val)); err == nil {
				g.ID = id
			}
		}
	}
	return g, true
}

// csi holds the decoded fields of a control sequence.
type csi struct {
	// params are semicolon-separated groups of colon-separated sub-parameters;
	// -1 marks an omitted value.
	params       [][]int
	prefix       byte
	intermediate byte
	final        byte
}

const maxCSIParams = 32

func (c *csi) param(group, sub, def int) int {
	if group >= len(c.params) || sub >= len(c.params[group]) {
		return def
	}
	if v := c.params[group][sub]; v >= 0 {
		return v
	}
	return def
}

func parseCSI(b []byte) (int, Event, bool) {
	i := 2
	if i >= len(b) {
		return 0, nil, false
	}
	var c csi
	if b[i] >= '<' && b[i] <= '?' {
		c.prefix = b[i]
		i++
	}
	start := i
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
		i++
	}
	paramBytes := b[start:i]
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
		c.intermediate = b[i]
		i++
	}
	if i >= len(b) {
		return 0, nil, false
	}
	if b[i] < 0x40 || b[i] > 0x7e {
		switch {
		case i == 2:
			return 2, KeyPressEvent{Code: '[', Mod: ModAlt}, true
		case b[i] == can || b[i] == sub:
			return i + 1, UnknownEvent(b[:i+1]), true
		}
		return i, UnknownEvent(b[:i]), true
	}
	c.final = b[i]
	i++
	c.params = splitParams(paramBytes)

	if c.final == 'M' && c.prefix == 0 && c.intermediate == 0 && len(c.params) == 0 {
		if len(b) < i+3 {
			return 0, nil, false
		}
		return i + 3, x10Mouse(b[i], b[i+1], b[i+2]), true
	}
	return i, c.event(b[:i]), true
}

func splitParams(p []byte) [][]int {
	if len(p) == 0 {
		return nil
	}
	groups := make([][]int, 0, 4)
	cur := []int{-1}
	for _, ch := range p {
		switch {
		case ch >= '0' && ch <= '9':
			last := len(cur) - 1
			if cur[last] < 0 {
				cur[last] = 0
			}
			if cur[last] < 1<<24 {
				cur[last] = cur[last]*10 + int(ch-'0')
			}
		case ch == ':':
			cur = append(cur, -1)
		case ch == ';':
			groups = append(groups, cur)
			cur = []int{-1}
			if len(groups) >= maxCSIParams {
				return groups
			}
		}
	}
	return append(groups, cur)
}

func (c *csi) event(raw []byte) Event {
	unknown := UnknownEvent(raw)
	if c.intermediate != 0 {
		return unknown
	}
	switch c.prefix {
	case 0:
	case '<':
		if (c.final == 'M' || c.final == 'm') && len(c.params) == 3 {
			return sgrMouse(c)
		}
		return unknown
	default:
		return unknown
	}

	switch c.final {
	case 'I':
		if len(c.params) == 0 {
			return FocusEvent{}
		}
	case 'O':
		if len(c.params) == 0 {
			return BlurEvent{}
		}
	case 'u':
		if len(c.params) > 0 {
			return kittyKey(c)
		}
	case '_':
		if len(c.params) == 6 {
			return win32InputKey(c)
		}
	case 't':
		switch c.param(0, 0, 0) {
		case 8, 48:
			if len(c.params) >= 3 {
				return WindowSizeEvent{Height: c.param(1, 0, 0), Width: c.param(2, 0, 0)}
			}
		}
	case '~', '^', '@':
		if ev, ok := c.tildeKey(); ok {
			return ev
		}
	case 'R':
		if len(c.params) >= 2 && c.param(0, 0, 1) != 1 {
			return unknown
		}
		return c.letterKey(unknown)
	case 'A', 'B', 'C', 'D', 'E', 'F', 'H', 'P', 'Q', 'S', 'Z', 'a', 'b', 'c', 'd':
		return c.letterKey(unknown)
	}
	return unknown
}

// letterKey decodes the CSI forms of the cursor and function keys:
// CSI A, CSI 1 ; mod A, and CSI 1 ; mod : event A.
func (c *csi) letterKey(unknown Event) Event {
	var k Key
	switch f := c.final; f {
	case 'a', 'b', 'c', 'd':
		k = Key{Code: KeyUp + rune(f-'a'), Mod: ModShift}
	case 'A', 'B', 'C', 'D':
		k = Key{Code: KeyUp + rune(f-'A')}
	case 'E':
		k = Key{Code: KeyBegin}
	case 'F':
		k = Key{Code: KeyEnd}
	case 'H':
		k = Key{Code: KeyHome}
	case 'P', 'Q', 'R', 'S':
		k = Key{Code: KeyF1 + rune(f-'P')}
	case 'Z':
		k = Key{Code: KeyTab, Mod: ModShift}
	default:
		return unknown
	}
	if len(c.params) > 2 {
		return unknown
	}
	if len(c.params) > 0 {
		if first := c.param(0, 0, 1); first != 1 {
			return unknown
		}
	}
	return c.withModifiers(k, 1)
}

// withModifiers applies the xterm modifier parameter and the kitty event type
// found in params[group].
func (c *csi) withModifiers(k Key, group int) Event {
	if mod := c.param(group, 0, 1); mod > 1 {
		k.Mod |= xtermMod(mod)
	}
	switch c.param(group, 1, 1) {
	case 2:
		k.IsRepeat = true
	case 3:
		return KeyReleaseEvent(k)
	}
	return KeyPressEvent(k)
}

func (c *csi) tildeKey() (Event, bool) {
	if len(c.params) == 0 {
		return nil, false
	}
	n := c.param(0, 0, 0)
	if c.final == '~' {
		switch n {
		case 200:
			return pasteStart{}, true
		case 201:
			return UnknownEvent("\x1b[201~"), true
		case 27:
			if len(c.params) == 3 {
				return modifyOtherKeys(c), true
			}
			return nil, false
		}
	}
	code, ok := tildeCode(n)
	if !ok || len(c.params) > 2 {
		return nil, false
	}
	k := Key{Code: code}
	switch c.final {
	case '^':
		k.Mod |= ModCtrl
	case '@':
		k.Mod |= ModCtrl | ModShift
	}
	return c.withModifiers(k, 1), true
}

func tildeCode(n int) (rune, bool) {
	switch {
	case n == 1 || n == 7:
		return KeyHome, true
	case n == 2:
		return KeyInsert, true
	case n == 3:
		return KeyDelete, true
	case n == 4 || n == 8:
		return KeyEnd, true
	case n == 5:
		return KeyPgUp, true
	case n == 6:
		return KeyPgDown, true
	case n >= 11 && n <= 15:
		return KeyF1 + rune(n-11), true
	case n >= 17 && n <= 21:
		return KeyF6 + rune(n-17), true
	case n >= 23 && n <= 26:
		return KeyF11 + rune(n-23), true
	case n == 28 || n == 29:
		return KeyF15 + rune(n-28), true
	case n >= 31 && n <= 34:
		return KeyF17 + rune(n-31), true
	}
	return 0, false
}

// xtermMod converts an xterm modifier parameter (1 + bitmask of shift, alt,
// ctrl, meta) to modifiers.
func xtermMod(param int) Mod {
	bits := param - 1
	var m Mod
	if bits&1 != 0 {
		m |= ModShift
	}
	if bits&2 != 0 {
		m |= ModAlt
	}
	if bits&4 != 0 {
		m |= ModCtrl
	}
	if bits&8 != 0 {
		m |= ModMeta
	}
	return m
}

// kittyMod converts a kitty keyboard protocol modifier parameter (1 + bitmask).
func kittyMod(param int) Mod {
	bits := param - 1
	var m Mod
	for _, f := range [...]struct {
		bit int
		mod Mod
	}{
		{1, ModShift}, {2, ModAlt}, {4, ModCtrl}, {8, ModSuper},
		{16, ModHyper}, {32, ModMeta}, {64, ModCapsLock}, {128, ModNumLock},
	} {
		if bits&f.bit != 0 {
			m |= f.mod
		}
	}
	return m
}

// modifyOtherKeys decodes CSI 27 ; mod ; code ~.
func modifyOtherKeys(c *csi) Event {
	k := Key{Code: rune(c.param(2, 0, 0))}
	switch k.Code {
	case 0x7f, 0x08:
		k.Code = KeyBackspace
	case '\r':
		k.Code = KeyEnter
	case '\t':
		k.Code = KeyTab
	case esc:
		k.Code = KeyEscape
	}
	if mod := c.param(1, 0, 1); mod > 1 {
		k.Mod = xtermMod(mod)
	}
	if k.Mod&^ModShift == 0 && unicode.IsPrint(k.Code) {
		k.Text = string(k.Code)
	}
	return KeyPressEvent(k)
}

func kittyCodeKey(code int) Key {
	if k, ok := kittyKeys[code]; ok {
		return k
	}
	switch {
	case code == 0:
		return Key{Code: KeySpace, Mod: ModCtrl}
	case code >= 1 && code <= 0x1a:
		return Key{Code: rune(code) + 0x60, Mod: ModCtrl}
	case code >= 0x1c && code <= 0x1f:
		return Key{Code: rune(code) + 0x40, Mod: ModCtrl}
	}
	r := rune(code)
	if !utf8.ValidRune(r) {
		r = utf8.RuneError
	}
	return Key{Code: r}
}

// kittyKey decodes the kitty keyboard protocol form
// CSI code[:shifted[:base]] ; mod[:event] ; text[:text...] u.
func kittyKey(c *csi) Event {
	k := kittyCodeKey(c.param(0, 0, 1))
	if s := rune(c.param(0, 1, 0)); s != 0 && unicode.IsPrint(s) {
		k.ShiftedCode = s
	}
	if b := rune(c.param(0, 2, 0)); b != 0 && unicode.IsPrint(b) {
		k.BaseCode = b
	}
	var release bool
	if mod := c.param(1, 0, 1); mod > 1 {
		k.Mod |= kittyMod(mod)
	}
	switch c.param(1, 1, 1) {
	case 2:
		k.IsRepeat = true
	case 3:
		release = true
	}
	if len(c.params) > 2 {
		var text strings.Builder
		for _, cp := range c.params[2] {
			if cp > 0 && utf8.ValidRune(rune(cp)) {
				text.WriteRune(rune(cp))
			}
		}
		k.Text = text.String()
	}

	const locks = ModCapsLock | ModNumLock | ModScrollLock
	if k.Text == "" && unicode.IsPrint(k.Code) && k.Mod&^(ModShift|locks) == 0 {
		switch {
		case k.Mod&(ModShift|ModCapsLock) == 0:
			k.Text = string(k.Code)
		case k.ShiftedCode != 0:
			k.Text = string(k.ShiftedCode)
		default:
			k.Text = string(unicode.ToUpper(k.Code))
		}
	}
	if release {
		return KeyReleaseEvent(k)
	}
	return KeyPressEvent(k)
}

var kittyKeys = func() map[int]Key {
	m := map[int]Key{
		8: {Code: KeyBackspace}, 9: {Code: KeyTab}, 13: {Code: KeyEnter},
		27: {Code: KeyEscape}, 127: {Code: KeyBackspace},
		57344: {Code: KeyEscape}, 57345: {Code: KeyEnter}, 57346: {Code: KeyTab},
		57347: {Code: KeyBackspace}, 57348: {Code: KeyInsert}, 57349: {Code: KeyDelete},
		57350: {Code: KeyLeft}, 57351: {Code: KeyRight}, 57352: {Code: KeyUp}, 57353: {Code: KeyDown},
		57354: {Code: KeyPgUp}, 57355: {Code: KeyPgDown}, 57356: {Code: KeyHome}, 57357: {Code: KeyEnd},
		57358: {Code: KeyCapsLock}, 57359: {Code: KeyScrollLock}, 57360: {Code: KeyNumLock},
		57361: {Code: KeyPrintScreen}, 57362: {Code: KeyPause}, 57363: {Code: KeyMenu},
	}
	for i := 0; i < 35; i++ {
		m[57364+i] = Key{Code: KeyF1 + rune(i)}
	}
	for i, code := range [...]rune{
		KeyKp0, KeyKp1, KeyKp2, KeyKp3, KeyKp4, KeyKp5, KeyKp6, KeyKp7, KeyKp8, KeyKp9,
		KeyKpDecimal, KeyKpDivide, KeyKpMultiply, KeyKpMinus, KeyKpPlus, KeyKpEnter, KeyKpEqual,
		KeyKpSep, KeyKpLeft, KeyKpRight, KeyKpUp, KeyKpDown, KeyKpPgUp, KeyKpPgDown,
		KeyKpHome, KeyKpEnd, KeyKpInsert, KeyKpDelete, KeyKpBegin,
		KeyMediaPlay, KeyMediaPause, KeyMediaPlayPause, KeyMediaReverse, KeyMediaStop,
		KeyMediaFastForward, KeyMediaRewind, KeyMediaNext, KeyMediaPrev, KeyMediaRecord,
		KeyLowerVol, KeyRaiseVol, KeyMute,
		KeyLeftShift, KeyLeftCtrl, KeyLeftAlt, KeyLeftSuper, KeyLeftHyper, KeyLeftMeta,
		KeyRightShift, KeyRightCtrl, KeyRightAlt, KeyRightSuper, KeyRightHyper, KeyRightMeta,
		KeyIsoLevel3Shift, KeyIsoLevel5Shift,
	} {
		m[57399+i] = Key{Code: code}
	}
	return m
}()
