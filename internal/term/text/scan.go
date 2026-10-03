// SPDX-License-Identifier: MPL-2.0

package text

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// TokenKind classifies a scanned run of terminal text.
type TokenKind uint8

const (
	// Cluster is one printable grapheme cluster.
	Cluster TokenKind = iota
	// Control is one C0, DEL or C1 control character.
	Control
	// CSI is a control sequence introduced by ESC [.
	CSI
	// OSC is an operating system command introduced by ESC ].
	OSC
	// Escape is any other escape sequence: string sequences (DCS, APC, PM,
	// SOS) and short ESC sequences.
	Escape
)

// Token is one element of a terminal string.
type Token struct {
	Kind TokenKind
	// Text is the exact bytes of the token.
	Text string
	// Width is the cell width of a Cluster and zero for every other kind.
	Width int
}

// Next scans the first token of s, which must be non-empty.
func Next(s string) Token {
	c := s[0]
	switch {
	case c == 0x1b:
		return scanEscape(s)
	case c < 0x20 || c == 0x7f:
		return Token{Kind: Control, Text: s[:1]}
	case c < 0x80:
		if len(s) == 1 || s[1] < 0x80 {
			return Token{Kind: Cluster, Text: s[:1], Width: 1}
		}
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 1 {
		return Token{Kind: Cluster, Text: s[:1], Width: 1}
	}
	if r >= 0x80 && r <= 0x9f {
		return Token{Kind: Control, Text: s[:size]}
	}
	cluster, _, width, _ := uniseg.FirstGraphemeClusterInString(s, -1)
	return Token{Kind: Cluster, Text: cluster, Width: width}
}

func scanEscape(s string) Token {
	if len(s) == 1 {
		return Token{Kind: Escape, Text: s}
	}
	switch s[1] {
	case '[':
		i := 2
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
			i++
		}
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
			i++
		}
		return Token{Kind: CSI, Text: s[:i]}
	case ']':
		return Token{Kind: OSC, Text: s[:stringEnd(s, true)]}
	case 'P', 'X', '^', '_':
		return Token{Kind: Escape, Text: s[:stringEnd(s, false)]}
	}
	i := 1
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
		i++
	}
	return Token{Kind: Escape, Text: s[:i]}
}

// stringEnd returns the end of a string sequence starting at s[0]=ESC. An
// unterminated sequence extends to the end of s.
func stringEnd(s string, bel bool) int {
	for i := 2; i < len(s); i++ {
		switch {
		case bel && s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
			return i + 2
		}
	}
	return len(s)
}

// SGR returns the parameter body of an SGR sequence token.
func (t Token) SGR() (body string, ok bool) {
	if t.Kind != CSI || len(t.Text) < 3 || t.Text[len(t.Text)-1] != 'm' {
		return "", false
	}
	body = t.Text[2 : len(t.Text)-1]
	for i := 0; i < len(body); i++ {
		if c := body[i]; !(c >= '0' && c <= '9' || c == ';' || c == ':') {
			return "", false
		}
	}
	return body, true
}

// Hyperlink decodes an OSC 8 token. An empty url closes the active link.
func (t Token) Hyperlink() (params, url string, ok bool) {
	if t.Kind != OSC || !strings.HasPrefix(t.Text, "\x1b]8;") {
		return "", "", false
	}
	body := t.Text[2:]
	switch {
	case strings.HasSuffix(body, "\x07"):
		body = body[:len(body)-1]
	case strings.HasSuffix(body, "\x1b\\"):
		body = body[:len(body)-2]
	default:
		return "", "", false
	}
	parts := strings.SplitN(body, ";", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// SetHyperlink returns the OSC 8 sequence opening a link; params are joined
// with ':'.
func SetHyperlink(url string, params ...string) string {
	return "\x1b]8;" + strings.Join(params, ":") + ";" + url + "\x07"
}

// ResetHyperlink returns the OSC 8 sequence closing the active link.
func ResetHyperlink() string { return SetHyperlink("") }
