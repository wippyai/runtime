// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"math"
	"slices"
	"unicode/utf8"
)

func (w *Encoder) reserve(n int) {
	if cap(w.buf)-len(w.buf) < n {
		w.buf = slices.Grow(w.buf, max(n, cap(w.buf)))
	}
}

func (w *Encoder) escaped(s string, attribute bool) error {
	return escape(w, s, attribute, false)
}

const (
	escapeText = 1 << iota
	escapeAttribute
	validateCharacter
)

var escapeFlags = func() [256]byte {
	var flags [256]byte
	for i := range flags {
		if i < asciiSpace && i != '\n' && i != '\r' && i != '\t' || i >= utf8.RuneSelf {
			flags[i] = validateCharacter
		}
	}
	for _, c := range "&<>\r" {
		flags[c] |= escapeText | escapeAttribute
	}
	for _, c := range "\n\t\"" {
		flags[c] |= escapeAttribute
	}
	return flags
}()

var replacements = [256]string{
	'&': "&amp;", '<': "&lt;", '>': "&gt;", '\r': "&#13;",
	'\n': "&#10;", '\t': "&#9;", '"': "&quot;",
}

func escape[T ~string | ~[]byte](w *Encoder, s T, attribute, trusted bool) error {
	w.reserve(len(s))
	mask := byte(escapeText)
	if attribute {
		mask = escapeAttribute
	}
	if !trusted {
		mask |= validateCharacter
	}
	start := 0
	for i := 0; i < len(s); {
		for i < len(s) && escapeFlags[s[i]]&mask == 0 {
			i++
		}
		if i == len(s) {
			break
		}
		c := s[i]
		if escapeFlags[c]&validateCharacter != 0 {
			r, n := utf8.DecodeRuneInString(string(s[i:min(i+utf8.UTFMax, len(s))]))
			if !validChar(r) || r == utf8.RuneError && n == 1 {
				return w.fail("element", "invalid XML character")
			}
			i += n
			continue
		}
		w.buf = append(w.buf, s[start:i]...)
		w.buf = append(w.buf, replacements[c]...)
		i++
		start = i
	}
	w.buf = append(w.buf, s[start:]...)
	return nil
}

// Every byte count in admission and accounting is an int64: lengths are converted once
// where they enter, limits are stored as int64, and worst cases are compared by division,
// so no count can overflow for any string on any architecture.

// maxEscaped is the longest replacement of one input byte.
const maxEscaped int64 = int64(len("&quot;"))

// escapedLen returns the length of s after escaping.
func escapedLen[T ~string | ~[]byte](s T, attribute bool) int64 {
	mask := byte(escapeText)
	if attribute {
		mask = escapeAttribute
	}
	n := int64(len(s))
	for i := 0; i < len(s); i++ {
		if escapeFlags[s[i]]&mask != 0 {
			n += int64(len(replacements[s[i]]) - 1)
		}
	}
	return n
}

// worstFits reports whether fixed bytes plus n value bytes, each escaped to at most
// maxEscaped bytes, fit in room.
func worstFits(room, fixed, n int64) bool {
	room -= fixed
	return room >= 0 && n <= room/maxEscaped
}

// nodeRoom returns the bytes the current tree may still take, or the largest count when
// no tree is being written.
func (w *Encoder) nodeRoom() int64 {
	if w.treeStart < 0 {
		return math.MaxInt64
	}
	return w.maxNode - (w.written + int64(len(w.buf)) - w.treeStart)
}

// exceeds reports whether n more bytes would take the current tree past MaxNodeBytes.
func (w *Encoder) exceeds(n int64) bool { return n > w.nodeRoom() }

// admit checks a unit of the start tag that began at offset tag before it is appended:
// fixed literal bytes plus value after escaping. The tag keeps one byte for its closing
// bracket. The escaped length is computed only when the worst case does not fit.
func admit[T ~string | ~[]byte](w *Encoder, tag int, fixed int64, value T, attribute bool) error {
	tagRoom := w.maxTag - int64(len(w.buf)-tag+1)
	nodeRoom := w.nodeRoom()
	if n := int64(len(value)); worstFits(tagRoom, fixed, n) && worstFits(nodeRoom, fixed, n) {
		return nil
	}
	n := fixed + int64(len(value))
	if n <= tagRoom && n <= nodeRoom {
		n = fixed + escapedLen(value, attribute)
	}
	if n > tagRoom {
		return w.limit("max_tag_bytes")
	}
	if n > nodeRoom {
		return w.limit("max_node_bytes")
	}
	return nil
}

// admitMarkup checks a comment or processing instruction of fixed literal bytes plus data
// bytes, and the parent's pending bracket, before they are appended.
func (w *Encoder) admitMarkup(fixed, data int64) error {
	if data > w.maxTag-fixed {
		return w.limit("max_tag_bytes")
	}
	if data > w.nodeRoom()-fixed-w.pendingBytes() {
		return w.limit("max_node_bytes")
	}
	return nil
}

// admitText checks character data, with the parent's pending bracket, before it is appended.
func admitText[T ~string | ~[]byte](w *Encoder, data T) error {
	if w.treeStart < 0 {
		return nil
	}
	pre := w.contentBytes(true)
	room := w.nodeRoom()
	n := int64(len(data))
	if worstFits(room, pre, n) || pre+n <= room && pre+escapedLen(data, false) <= room {
		return nil
	}
	return w.limit("max_node_bytes")
}

// maxIndent bounds the indent option.
const maxIndent = 256

// qualifiedLen returns the length of prefix:name, or of name without a prefix.
func qualifiedLen(prefix, name string) int64 {
	if prefix == "" {
		return int64(len(name))
	}
	return int64(len(prefix)) + int64(len(":")) + int64(len(name))
}

// declarationLen returns the literal bytes of a declaration, without its URI.
func declarationLen(prefix string) int64 {
	if prefix == "" {
		return int64(len(` xmlns=""`))
	}
	return int64(len(` xmlns:=""`)) + int64(len(prefix))
}

// attributeLen returns the literal bytes of an attribute, without its value.
func attributeLen(prefix, name string) int64 {
	return int64(len(` =""`)) + qualifiedLen(prefix, name)
}

// reserveWorst takes the worst case of fixed bytes plus n escaped value bytes from both
// rooms and reports whether it fit.
func reserveWorst(tagRoom, nodeRoom *int64, fixed, n int64) bool {
	if !worstFits(*tagRoom, fixed, n) || !worstFits(*nodeRoom, fixed, n) {
		return false
	}
	worst := fixed + n*maxEscaped
	*tagRoom -= worst
	*nodeRoom -= worst
	return true
}
