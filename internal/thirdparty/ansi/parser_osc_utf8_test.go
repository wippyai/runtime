package ansi

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/x/ansi/parser"
)

// collectOsc runs input through a fresh parser and returns every OSC payload in
// dispatch order, plus the final parser state.
func collectOsc(input string) ([][]byte, parser.State) {
	var payloads [][]byte
	p := NewParser()
	p.SetHandler(Handler{HandleOsc: func(_ int, data []byte) {
		payloads = append(payloads, append([]byte(nil), data...))
	}})
	for i := 0; i < len(input); i++ {
		p.Advance(input[i])
	}
	return payloads, p.State()
}

// TestOscUtf8ContinuationByteIsNotStringTerminator is the regression guard for
// OSC titles whose UTF-8 encoding contains a 0x9C continuation byte, such as
// U+2733 (E2 9C B3). Treating that byte as the 8-bit String Terminator
// truncated the payload mid rune and printed the remainder to the screen,
// leaving stale cells in an application's input box. Verified against tmux,
// which reports the U+2733 title intact.
func TestOscUtf8ContinuationByteIsNotStringTerminator(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "three_byte_rune_esc_st", input: "\x1b]0;\u2733 Pong reply\x1b\\", want: "0;\u2733 Pong reply"},
		{name: "three_byte_rune_bel", input: "\x1b]0;\u2733 Pong reply\x07", want: "0;\u2733 Pong reply"},
		{name: "two_byte_rune", input: "\x1b]2;\u00e9 title\x1b\\", want: "2;\u00e9 title"},
		{name: "four_byte_rune", input: "\x1b]2;\U0001F600 title\x1b\\", want: "2;\U0001F600 title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payloads, _ := collectOsc(tc.input)
			if len(payloads) != 1 || string(payloads[0]) != tc.want {
				t.Fatalf("OSC payloads = %q, want [%q]", payloads, tc.want)
			}
		})
	}
}

// TestOscEightBitStStillTerminatesString keeps the genuine 8-bit String
// Terminator working: a standalone 0x9C that does not continue a rune ends the
// string and returns the parser to ground.
func TestOscEightBitStStillTerminatesString(t *testing.T) {
	payloads, state := collectOsc("\x1b]2;hi\x9cZ")
	if len(payloads) != 1 || !bytes.Equal(payloads[0], []byte("2;hi")) {
		t.Fatalf("OSC payloads = %q, want [\"2;hi\"]", payloads)
	}
	if state != parser.GroundState {
		t.Fatalf("state after 8-bit ST = %s, want GroundState", parser.StateNames[state])
	}
}

// TestDcsUtf8ContinuationByteIsNotStringTerminator covers the same collision in
// DCS string data.
func TestDcsUtf8ContinuationByteIsNotStringTerminator(t *testing.T) {
	var got []byte
	seen := false
	p := NewParser()
	p.SetHandler(Handler{HandleDcs: func(_ Cmd, _ Params, data []byte) {
		if !seen {
			got = append([]byte(nil), data...)
			seen = true
		}
	}})
	in := "\x1bPq;\u2733 data\x1b\\"
	for i := 0; i < len(in); i++ {
		p.Advance(in[i])
	}
	if !seen || !bytes.Equal(got, []byte(";\u2733 data")) {
		t.Fatalf("DCS data = %q, want %q", got, ";\u2733 data")
	}
}
