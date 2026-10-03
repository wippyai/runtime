// SPDX-License-Identifier: MPL-2.0

package input

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, in string) []Event {
	t.Helper()
	p := NewParser()
	var out []Event
	emit := func(e Event) { out = append(out, e) }
	p.Feed([]byte(in), emit)
	p.ResolveEscape(emit)
	return out
}

func press(code rune, mod Mod) Event { return KeyPressEvent{Code: code, Mod: mod} }

func TestPrintableAndUTF8(t *testing.T) {
	assert.Equal(t, []Event{KeyPressEvent{Code: 'a', Text: "a"}}, decode(t, "a"))
	assert.Equal(t, []Event{KeyPressEvent{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: ModShift}}, decode(t, "A"))
	assert.Equal(t, []Event{KeyPressEvent{Code: 'é', Text: "é"}}, decode(t, "é"))
	assert.Equal(t, []Event{KeyPressEvent{Code: '世', Text: "世"}}, decode(t, "世"))
	assert.Equal(t, []Event{KeyPressEvent{Code: KeyExtended, Text: "\u0438\u0306"}}, decode(t, "\u0438\u0306"))
	assert.Equal(t, []Event{KeyPressEvent{Code: KeySpace, Text: " "}}, decode(t, " "))
}

func TestInvalidUTF8IsDiscarded(t *testing.T) {
	events := decode(t, "\xffa")
	require.Len(t, events, 2)
	assert.IsType(t, UnknownEvent(""), events[0])
	assert.Equal(t, KeyPressEvent{Code: 'a', Text: "a"}, events[1])
}

func TestControlKeys(t *testing.T) {
	cases := map[string]Event{
		"\r":   press(KeyEnter, 0),
		"\t":   press(KeyTab, 0),
		"\x7f": press(KeyBackspace, 0),
		"\x00": press(KeySpace, ModCtrl),
		"\x01": press('a', ModCtrl),
		"\x03": press('c', ModCtrl),
		"\x08": press('h', ModCtrl),
		"\x1a": press('z', ModCtrl),
		"\x1c": press('\\', ModCtrl),
		"\x1f": press('_', ModCtrl),
	}
	for in, want := range cases {
		assert.Equal(t, []Event{want}, decode(t, in), "%q", in)
	}
}

func TestEscapeAndAlt(t *testing.T) {
	assert.Equal(t, []Event{press(KeyEscape, 0)}, decode(t, "\x1b"))
	assert.Equal(t, []Event{press('x', ModAlt)}, decode(t, "\x1bx"))
	assert.Equal(t, []Event{KeyPressEvent{Code: 'x', ShiftedCode: 'X', Mod: ModShift | ModAlt}}, decode(t, "\x1bX"))
	assert.Equal(t, []Event{press(KeyEnter, ModAlt)}, decode(t, "\x1b\r"))
	assert.Equal(t, []Event{press('a', ModCtrl|ModAlt)}, decode(t, "\x1b\x01"))
	assert.Equal(t, []Event{press(KeyEscape, ModAlt)}, decode(t, "\x1b\x1b"))
	assert.Equal(t, []Event{press(KeyUp, ModAlt)}, decode(t, "\x1b\x1b[A"))
	assert.Equal(t, []Event{press('é', ModAlt)}, decode(t, "\x1bé"))
	assert.Equal(t, []Event{press('[', ModAlt), press('a', ModCtrl)}, decode(t, "\x1b[\x01"))
	assert.Equal(t, []Event{KeyPressEvent{Code: 'p', ShiftedCode: 'P', Mod: ModShift | ModAlt}}, decode(t, "\x1bP"))
	assert.Equal(t, []Event{KeyPressEvent{Code: 'x', ShiftedCode: 'X', Mod: ModShift | ModAlt}}, decode(t, "\x1bX"))
	assert.Equal(t, []Event{press(']', ModAlt)}, decode(t, "\x1b]"))
	assert.Equal(t, []Event{press('_', ModAlt)}, decode(t, "\x1b_"))
	assert.Equal(t, []Event{press('^', ModAlt)}, decode(t, "\x1b^"))
}

func TestLoneEscapeIsResolvedOnlyOnRequest(t *testing.T) {
	p := NewParser()
	var out []Event
	emit := func(e Event) { out = append(out, e) }
	p.Feed([]byte("\x1b"), emit)
	assert.Empty(t, out)
	assert.Equal(t, 1, p.Pending())
	p.Feed([]byte("[A"), emit)
	assert.Equal(t, []Event{press(KeyUp, 0)}, out)

	out = nil
	p.Feed([]byte("\x1b["), emit)
	p.ResolveEscape(emit)
	assert.Empty(t, out, "a partial CSI is not an escape key")
	p.Feed([]byte("B"), emit)
	assert.Equal(t, []Event{press(KeyDown, 0)}, out)

	out = nil
	p.Feed([]byte("\x1b"), emit)
	p.Flush(emit)
	assert.Equal(t, []Event{press(KeyEscape, 0)}, out)
	assert.Zero(t, p.Pending())
}

func TestCSICursorAndFunctionKeys(t *testing.T) {
	cases := map[string]Event{
		"\x1b[A":     press(KeyUp, 0),
		"\x1b[B":     press(KeyDown, 0),
		"\x1b[C":     press(KeyRight, 0),
		"\x1b[D":     press(KeyLeft, 0),
		"\x1b[H":     press(KeyHome, 0),
		"\x1b[F":     press(KeyEnd, 0),
		"\x1b[E":     press(KeyBegin, 0),
		"\x1b[Z":     press(KeyTab, ModShift),
		"\x1b[P":     press(KeyF1, 0),
		"\x1b[Q":     press(KeyF2, 0),
		"\x1b[R":     press(KeyF3, 0),
		"\x1b[S":     press(KeyF4, 0),
		"\x1b[a":     press(KeyUp, ModShift),
		"\x1b[1;2A":  press(KeyUp, ModShift),
		"\x1b[1;3A":  press(KeyUp, ModAlt),
		"\x1b[1;5C":  press(KeyRight, ModCtrl),
		"\x1b[1;7D":  press(KeyLeft, ModCtrl|ModAlt),
		"\x1b[1;8H":  press(KeyHome, ModShift|ModAlt|ModCtrl),
		"\x1b[1;9A":  press(KeyUp, ModMeta),
		"\x1b[1;5P":  press(KeyF1, ModCtrl),
		"\x1b[1;2R":  press(KeyF3, ModShift),
		"\x1b[2~":    press(KeyInsert, 0),
		"\x1b[3~":    press(KeyDelete, 0),
		"\x1b[5~":    press(KeyPgUp, 0),
		"\x1b[6~":    press(KeyPgDown, 0),
		"\x1b[1~":    press(KeyHome, 0),
		"\x1b[4~":    press(KeyEnd, 0),
		"\x1b[7~":    press(KeyHome, 0),
		"\x1b[8~":    press(KeyEnd, 0),
		"\x1b[3;5~":  press(KeyDelete, ModCtrl),
		"\x1b[5;2~":  press(KeyPgUp, ModShift),
		"\x1b[11~":   press(KeyF1, 0),
		"\x1b[15~":   press(KeyF5, 0),
		"\x1b[17~":   press(KeyF6, 0),
		"\x1b[21~":   press(KeyF10, 0),
		"\x1b[23~":   press(KeyF11, 0),
		"\x1b[24~":   press(KeyF12, 0),
		"\x1b[24;5~": press(KeyF12, ModCtrl),
		"\x1b[28~":   press(KeyF15, 0),
		"\x1b[34~":   press(KeyF20, 0),
		"\x1b[3^":    press(KeyDelete, ModCtrl),
		"\x1b[3@":    press(KeyDelete, ModCtrl|ModShift),
	}
	for in, want := range cases {
		assert.Equal(t, []Event{want}, decode(t, in), "%q", in)
	}
}

func TestSS3Keys(t *testing.T) {
	cases := map[string]Event{
		"\x1bOA":  press(KeyUp, 0),
		"\x1bOD":  press(KeyLeft, 0),
		"\x1bOH":  press(KeyHome, 0),
		"\x1bOF":  press(KeyEnd, 0),
		"\x1bOP":  press(KeyF1, 0),
		"\x1bOS":  press(KeyF4, 0),
		"\x1bOM":  press(KeyKpEnter, 0),
		"\x1bOj":  press(KeyKpMultiply, 0),
		"\x1bO5A": press(KeyUp, ModCtrl),
		"\x1bO2P": press(KeyF1, ModShift),
		"\x1bOa":  press(KeyUp, ModCtrl),
	}
	for in, want := range cases {
		assert.Equal(t, []Event{want}, decode(t, in), "%q", in)
	}
}

func TestKittyKeyboard(t *testing.T) {
	cases := []struct {
		in   string
		want Event
	}{
		{"\x1b[97u", KeyPressEvent{Code: 'a', Text: "a"}},
		{"\x1b[97;5u", KeyPressEvent{Code: 'a', Mod: ModCtrl}},
		{"\x1b[97;2u", KeyPressEvent{Code: 'a', Mod: ModShift, Text: "A"}},
		{"\x1b[97:65;2u", KeyPressEvent{Code: 'a', ShiftedCode: 'A', Mod: ModShift, Text: "A"}},
		{"\x1b[1089:1057:99;5u", KeyPressEvent{Code: 'с', ShiftedCode: 'С', BaseCode: 'c', Mod: ModCtrl}},
		{"\x1b[97;1:3u", KeyReleaseEvent{Code: 'a', Text: "a"}},
		{"\x1b[97;5:3u", KeyReleaseEvent{Code: 'a', Mod: ModCtrl}},
		{"\x1b[97;1:2u", KeyPressEvent{Code: 'a', Text: "a", IsRepeat: true}},
		{"\x1b[97;;97u", KeyPressEvent{Code: 'a', Text: "a"}},
		{"\x1b[233;;233u", KeyPressEvent{Code: 'é', Text: "é"}},
		{"\x1b[13u", KeyPressEvent{Code: KeyEnter}},
		{"\x1b[9;2u", KeyPressEvent{Code: KeyTab, Mod: ModShift}},
		{"\x1b[27u", KeyPressEvent{Code: KeyEscape}},
		{"\x1b[127;3u", KeyPressEvent{Code: KeyBackspace, Mod: ModAlt}},
		{"\x1b[57364u", KeyPressEvent{Code: KeyF1}},
		{"\x1b[57376;1:3u", KeyReleaseEvent{Code: KeyF13}},
		{"\x1b[57399u", KeyPressEvent{Code: KeyKp0}},
		{"\x1b[57441u", KeyPressEvent{Code: KeyLeftShift}},
		{"\x1b[97;9u", KeyPressEvent{Code: 'a', Mod: ModSuper}},
		{"\x1b[97;65u", KeyPressEvent{Code: 'a', Mod: ModCapsLock, Text: "A"}},
		{"\x1b[97;129u", KeyPressEvent{Code: 'a', Mod: ModNumLock, Text: "a"}},
		{"\x1b[1;1:3A", KeyReleaseEvent{Code: KeyUp}},
		{"\x1b[1;5:2A", KeyPressEvent{Code: KeyUp, Mod: ModCtrl, IsRepeat: true}},
		{"\x1b[3;1:3~", KeyReleaseEvent{Code: KeyDelete}},
		{"\x1b[27;5;13~", KeyPressEvent{Code: KeyEnter, Mod: ModCtrl}},
		{"\x1b[27;2;97~", KeyPressEvent{Code: 'a', Mod: ModShift, Text: "a"}},
	}
	for _, tc := range cases {
		assert.Equal(t, []Event{tc.want}, decode(t, tc.in), "%q", tc.in)
	}
}

func TestSGRMouse(t *testing.T) {
	cases := []struct {
		in   string
		want Event
	}{
		{"\x1b[<0;10;20M", MouseClickEvent{X: 9, Y: 19, Button: MouseLeft}},
		{"\x1b[<0;10;20m", MouseReleaseEvent{X: 9, Y: 19, Button: MouseLeft}},
		{"\x1b[<1;1;1M", MouseClickEvent{Button: MouseMiddle}},
		{"\x1b[<2;1;1M", MouseClickEvent{Button: MouseRight}},
		{"\x1b[<2;5;6m", MouseReleaseEvent{X: 4, Y: 5, Button: MouseRight}},
		{"\x1b[<35;3;4M", MouseMotionEvent{X: 2, Y: 3, Button: MouseNone}},
		{"\x1b[<32;3;4M", MouseMotionEvent{X: 2, Y: 3, Button: MouseLeft}},
		{"\x1b[<64;5;5M", MouseWheelEvent{X: 4, Y: 4, Button: MouseWheelUp}},
		{"\x1b[<65;5;5M", MouseWheelEvent{X: 4, Y: 4, Button: MouseWheelDown}},
		{"\x1b[<66;5;5M", MouseWheelEvent{X: 4, Y: 4, Button: MouseWheelLeft}},
		{"\x1b[<67;5;5M", MouseWheelEvent{X: 4, Y: 4, Button: MouseWheelRight}},
		{"\x1b[<4;1;1M", MouseClickEvent{Button: MouseLeft, Mod: ModShift}},
		{"\x1b[<8;1;1M", MouseClickEvent{Button: MouseLeft, Mod: ModAlt}},
		{"\x1b[<16;1;1M", MouseClickEvent{Button: MouseLeft, Mod: ModCtrl}},
		{"\x1b[<128;1;1M", MouseClickEvent{Button: MouseBackward}},
		{"\x1b[<129;1;1M", MouseClickEvent{Button: MouseForward}},
		{"\x1b[<0;1000;2000M", MouseClickEvent{X: 999, Y: 1999, Button: MouseLeft}},
	}
	for _, tc := range cases {
		assert.Equal(t, []Event{tc.want}, decode(t, tc.in), "%q", tc.in)
	}
}

func TestX10Mouse(t *testing.T) {
	cases := []struct {
		in   string
		want Event
	}{
		{"\x1b[M !!", MouseClickEvent{Button: MouseLeft}},
		{"\x1b[M!*+", MouseClickEvent{X: 9, Y: 10, Button: MouseMiddle}},
		{"\x1b[M\"!!", MouseClickEvent{Button: MouseRight}},
		{"\x1b[M#!!", MouseReleaseEvent{Button: MouseNone}},
		{"\x1b[M`!!", MouseWheelEvent{Button: MouseWheelUp}},
		{"\x1b[Ma!!", MouseWheelEvent{Button: MouseWheelDown}},
		{"\x1b[M`\x7f\x7f", MouseWheelEvent{X: 94, Y: 94, Button: MouseWheelUp}},
		{"\x1b[M`\xff\xff", MouseWheelEvent{X: 222, Y: 222, Button: MouseWheelUp}},
		{"\x1b[M0!!", MouseClickEvent{Button: MouseLeft, Mod: ModCtrl}},
		{"\x1b[M(!!", MouseClickEvent{Button: MouseLeft, Mod: ModAlt}},
		{"\x1b[M@!!", MouseMotionEvent{Button: MouseLeft}},
		{"\x1b[MC!!", MouseMotionEvent{Button: MouseNone}},
	}
	for _, tc := range cases {
		assert.Equal(t, []Event{tc.want}, decode(t, tc.in), "%q", tc.in)
	}
}

func TestFocus(t *testing.T) {
	assert.Equal(t, []Event{FocusEvent{}}, decode(t, "\x1b[I"))
	assert.Equal(t, []Event{BlurEvent{}}, decode(t, "\x1b[O"))
}

func TestWindowSize(t *testing.T) {
	assert.Equal(t, []Event{WindowSizeEvent{Width: 120, Height: 40}}, decode(t, "\x1b[8;40;120t"))
	assert.Equal(t, []Event{WindowSizeEvent{Width: 120, Height: 40}}, decode(t, "\x1b[48;40;120;800;1200t"))
}

func TestBracketedPaste(t *testing.T) {
	assert.Equal(t, []Event{PasteEvent("hello\r\nworld\x1b[A")}, decode(t, "\x1b[200~hello\r\nworld\x1b[A\x1b[201~"))
	assert.Equal(t, []Event{PasteEvent("")}, decode(t, "\x1b[200~\x1b[201~"))
	assert.Equal(t, []Event{PasteEvent("é世\n"), KeyPressEvent{Code: 'z', Text: "z"}}, decode(t, "\x1b[200~é世\n\x1b[201~z"))
	assert.Equal(t, []Event{PasteEvent("ab")}, decode(t, "\x1b[200~a\xffb\x1b[201~"))
}

func TestBracketedPasteAcrossFeeds(t *testing.T) {
	p := NewParser()
	var out []Event
	emit := func(e Event) { out = append(out, e) }
	for _, part := range []string{"\x1b[20", "0~abc\x1b", "[20", "1", "~", "x"} {
		p.Feed([]byte(part), emit)
		p.ResolveEscape(emit)
	}
	assert.Equal(t, []Event{PasteEvent("abc"), KeyPressEvent{Code: 'x', Text: "x"}}, out)

	out = nil
	p.Feed([]byte("\x1b[200~"+strings.Repeat("q", 100000)), emit)
	assert.Empty(t, out)
	p.Feed([]byte("\x1b[201~"), emit)
	require.Len(t, out, 1)
	assert.Len(t, string(out[0].(PasteEvent)), 100000)
}

func TestEscapeInsidePasteIsContent(t *testing.T) {
	p := NewParser()
	var out []Event
	emit := func(e Event) { out = append(out, e) }
	p.Feed([]byte("\x1b[200~a\x1b"), emit)
	p.ResolveEscape(emit)
	assert.Empty(t, out)
	p.Feed([]byte("\x1b[201~"), emit)
	assert.Equal(t, []Event{PasteEvent("a\x1b")}, out)
}

func TestGraphicsReply(t *testing.T) {
	assert.Equal(t,
		[]Event{GraphicsEvent{ID: 4294967294, Payload: []byte("OK")}, KeyPressEvent{Code: 'z', Text: "z"}},
		decode(t, "\x1b_Gi=4294967294;OK\x1b\\z"))
	assert.Equal(t, []Event{GraphicsEvent{ID: 7, Payload: []byte("ENOENT:x")}},
		decode(t, "\x1b_Gi=7,I=2;ENOENT:x\x1b\\"))
	assert.Equal(t, []Event{GraphicsEvent{}}, decode(t, "\x1b_G\x1b\\"))
}

func TestStringSequencesAreSwallowed(t *testing.T) {
	for _, in := range []string{
		"\x1b]11;rgb:0000/0000/0000\x07",
		"\x1b]52;c;aGk=\x1b\\",
		"\x1bP>|term 1.0\x1b\\",
		"\x1b_Xother\x1b\\",
		"\x1b^pm\x1b\\",
		"\x1bXsos\x1b\\",
	} {
		events := decode(t, in+"z")
		require.Len(t, events, 2, "%q", in)
		assert.IsType(t, UnknownEvent(""), events[0], "%q", in)
		assert.Equal(t, KeyPressEvent{Code: 'z', Text: "z"}, events[1], "%q", in)
	}
}

func TestUnrecognizedSequencesAreUnknown(t *testing.T) {
	for _, in := range []string{"\x1b[?1;2c", "\x1b[?25;1$y", "\x1b[12;5R", "\x1b[999~", "\x1b[0 q", "\x1b[>1u"} {
		events := decode(t, in+"z")
		require.Len(t, events, 2, "%q", in)
		assert.IsType(t, UnknownEvent(""), events[0], "%q", in)
	}
}

func TestCancelledSequences(t *testing.T) {
	events := decode(t, "\x1b[1;\x18z")
	require.Len(t, events, 2)
	assert.IsType(t, UnknownEvent(""), events[0])
	assert.Equal(t, KeyPressEvent{Code: 'z', Text: "z"}, events[1])

	events = decode(t, "\x1b[1;\x1b[A")
	require.Len(t, events, 2)
	assert.IsType(t, UnknownEvent(""), events[0])
	assert.Equal(t, press(KeyUp, 0), events[1])
}

func TestFragmentationIsTransparent(t *testing.T) {
	stream := "a\x1b[1;5A\x1b[<64;5;5M\x1b[M@\"\"é\x1b[97;5:3u\x1b[200~p\x1b[201~\x1bOP\x1b[I\x1bx\x1b_Gi=3;OK\x1b\\\x1b[24;5~"
	want := decode(t, stream)
	require.Len(t, want, 12)

	for size := 1; size <= 7; size++ {
		p := NewParser()
		var got []Event
		emit := func(e Event) { got = append(got, e) }
		for i := 0; i < len(stream); i += size {
			p.Feed([]byte(stream[i:min(i+size, len(stream))]), emit)
		}
		p.ResolveEscape(emit)
		assert.Equal(t, want, got, "chunk size %d", size)
	}
}

func TestWin32InputMode(t *testing.T) {
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: 'a', BaseCode: 'a', Text: "a"}},
		decode(t, "\x1b[65;30;97;1;0;1_"))
	assert.Equal(t,
		[]Event{KeyReleaseEvent{Code: 'a', BaseCode: 'a', Text: "a"}},
		decode(t, "\x1b[65;30;97;0;0;1_"))
	assert.Equal(t,
		[]Event{KeyPressEvent{Code: KeyUp, BaseCode: KeyUp}, KeyPressEvent{Code: KeyUp, BaseCode: KeyUp}},
		decode(t, "\x1b[38;72;0;1;256;2_"))
	// A UTF-16 surrogate pair split over two records forms one character.
	events := decode(t, "\x1b[0;0;55357;1;0;1_\x1b[0;0;56832;1;0;1_")
	assert.Equal(t, []Event{KeyPressEvent{Code: '\U0001F600', Text: "\U0001F600"}}, events)
}

func TestStreamDeliversEventsAndEOF(t *testing.T) {
	var got []Event
	err := Stream(context.Background(), strings.NewReader("a\x1b[A\x1b"), func(e Event) { got = append(got, e) })
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []Event{KeyPressEvent{Code: 'a', Text: "a"}, press(KeyUp, 0), press(KeyEscape, 0)}, got)
}

type scriptedReader struct {
	chunks [][]byte
	final  error
}

func (r *scriptedReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, r.final
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

func TestStreamEscapeRuleIsPerRead(t *testing.T) {
	var got []Event
	r := &scriptedReader{chunks: [][]byte{[]byte("\x1b"), []byte("\x1b"), []byte("[A")}, final: io.EOF}
	_ = Stream(context.Background(), r, func(e Event) { got = append(got, e) })
	assert.Equal(t, []Event{press(KeyEscape, 0), press(KeyEscape, 0), KeyPressEvent{Code: '[', Text: "["}, KeyPressEvent{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: ModShift}}, got)
}

func TestStreamPartialSequenceAcrossReads(t *testing.T) {
	var got []Event
	r := &scriptedReader{chunks: [][]byte{[]byte("\x1b["), []byte("1;5"), []byte("A")}, final: io.EOF}
	_ = Stream(context.Background(), r, func(e Event) { got = append(got, e) })
	assert.Equal(t, []Event{press(KeyUp, ModCtrl)}, got)
}

func TestStreamReturnsReadError(t *testing.T) {
	boom := errors.New("device failure")
	var got []Event
	r := &scriptedReader{chunks: [][]byte{[]byte("a")}, final: boom}
	err := Stream(context.Background(), r, func(e Event) { got = append(got, e) })
	assert.ErrorIs(t, err, boom)
	assert.Len(t, got, 1)
}

func TestStreamCancellationReturnsNilAndDropsEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var got []Event
	r := &cancelOnRead{cancel: cancel}
	err := Stream(ctx, r, func(e Event) { got = append(got, e) })
	assert.NoError(t, err)
	assert.Empty(t, got)
}

type cancelOnRead struct{ cancel context.CancelFunc }

func (r *cancelOnRead) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "abc"), errors.New("canceled")
}

func TestKeystroke(t *testing.T) {
	assert.Equal(t, "ctrl+alt+up", Key{Code: KeyUp, Mod: ModCtrl | ModAlt}.Keystroke())
	assert.Equal(t, "shift+a", Key{Code: 'a', Mod: ModShift}.Keystroke())
	assert.Equal(t, "f13", Key{Code: KeyF13}.Keystroke())
	assert.Equal(t, "space", Key{Code: KeySpace}.Keystroke())
	assert.Equal(t, "esc", Key{Code: KeyEscape}.Keystroke())
	assert.Equal(t, "leftshift", Key{Code: KeyLeftShift}.Keystroke())
}
