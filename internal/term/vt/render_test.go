package vt

import "testing"

func TestRenderLineTrimsTrailingBlanksAndStyles(t *testing.T) {
	term := New(WithCols(10), WithRows(3))
	defer term.Dispose()
	term.WriteString("plain\r\n\x1b[31mred\x1b[0m  x\r\n\x1b[44m  \x1b[0m")

	buf := term.Buffer()
	if got := buf.RenderLine(0); got != "plain" {
		t.Fatalf("plain line = %q", got)
	}
	if got, want := buf.RenderLine(1), "\x1b[31mred\x1b[0m  x"; got != want {
		t.Fatalf("styled line = %q, want %q", got, want)
	}
	if got, want := buf.RenderLine(2), "\x1b[44m  \x1b[0m"; got != want {
		t.Fatalf("background-only line = %q, want %q", got, want)
	}
	if got := buf.RenderLine(99); got != "" {
		t.Fatalf("out of range line = %q", got)
	}
}

func TestRenderLineKeepsWideRunes(t *testing.T) {
	term := New(WithCols(6), WithRows(1))
	defer term.Dispose()
	term.WriteString("a世b")
	if got := term.Buffer().RenderLine(0); got != "a世b" {
		t.Fatalf("line = %q", got)
	}
}

func TestSetScrollbackTrimsOldestHistory(t *testing.T) {
	term := New(WithCols(10), WithRows(2), WithScrollback(10))
	defer term.Dispose()
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		term.WriteString(s + "\r\n")
	}
	if got := term.Buffer().YBase; got != 4 {
		t.Fatalf("history = %d", got)
	}
	term.SetScrollback(2)
	buf := term.Buffer()
	if buf.YBase != 2 || term.Scrollback() != 2 {
		t.Fatalf("history = %d, option = %d", buf.YBase, term.Scrollback())
	}
	if got := buf.RenderLine(0); got != "c" {
		t.Fatalf("oldest retained line = %q", got)
	}
	term.WriteString("f\r\ng\r\n")
	if got := term.Buffer().YBase; got != 2 {
		t.Fatalf("history grew past the limit: %d", got)
	}
}

func TestNarrowResizeOfOverlongHistoryStaysWithinCapacity(t *testing.T) {
	term := New(WithCols(4000), WithRows(1), WithScrollback(2))
	defer term.Dispose()
	term.WriteString("\x1b[1;3999HX\r\n")
	term.SetScrollback(2)
	term.Resize(10, 1)
	if got := term.Buffer().Lines.Length(); got > 3 {
		t.Fatalf("buffer holds %d lines", got)
	}
}

func TestDecoderPassesStrayC1ControlsAndDropsOtherInvalidBytes(t *testing.T) {
	var d Utf8ToUtf32
	target := make([]uint32, 8)
	n := d.Decode([]byte{'a', 0x9C, 0xFF, 'b'}, target)
	if n != 3 || target[0] != 'a' || target[1] != 0x9C || target[2] != 'b' {
		t.Fatalf("decoded %v", target[:n])
	}
}

func TestOSCTerminatedByStrayEightBitST(t *testing.T) {
	term := New(WithCols(10), WithRows(1))
	defer term.Dispose()
	term.Write([]byte("\x1b]0;✳ title\x07\x1b]0;hi\x9cZ"))
	if got := term.GetLine(0); got != "Z" {
		t.Fatalf("line = %q", got)
	}
}
