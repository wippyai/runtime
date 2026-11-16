// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func utf16Bytes(s string, le, bom bool) []byte {
	var b []byte
	var order binary.AppendByteOrder = binary.BigEndian
	if le {
		order = binary.LittleEndian
	}
	if bom {
		b = order.AppendUint16(b, 0xFEFF)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = order.AppendUint16(b, u)
	}
	return b
}
func TestEncodings(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		code string
		data []byte
	}{
		{name: "utf8", data: []byte("<a>é𐐀</a>"), text: "é𐐀", code: ""},
		{name: "bom", data: append([]byte{239, 187, 191}, []byte("<a>é</a>")...), text: "é", code: ""},
		{name: "le", data: utf16Bytes(`<?xml version="1.0" encoding="UTF-16"?><a>é𐐀</a>`, true, true), text: "é𐐀", code: ""},
		{name: "be", data: utf16Bytes(`<?xml version="1.0" encoding="UTF-16BE"?><a>é𐐀</a>`, false, true), text: "é𐐀", code: ""},
		{name: "no bom", data: utf16Bytes(`<?xml version="1.0" encoding="UTF-16LE"?><a>é</a>`, true, false), text: "é", code: ""},
		{name: "latin1", data: []byte("<?xml version='1.0' encoding='ISO-8859-1'?><a>\xe9</a>"), text: "é", code: ""},
		{name: "windows", data: []byte("<?xml version='1.0' encoding='windows-1252'?><a>\x80</a>"), text: "€", code: ""},
		{name: "ascii", data: []byte("<?xml version='1.0' encoding='US-ASCII'?><a>text</a>"), text: "text", code: ""},
		{name: "bad ascii", data: []byte("<?xml version='1.0' encoding='US-ASCII'?><a>\x80</a>"), text: "", code: "encoding"},
		{name: "undefined", data: []byte("<?xml version='1.0' encoding='windows-1252'?><a>\x81</a>"), text: "", code: "encoding"},
		{name: "conflict", data: append([]byte{239, 187, 191}, []byte("<?xml version='1.0' encoding='ISO-8859-1'?><a/>")...), text: "", code: "encoding"},
		{name: "utf16 conflict", data: utf16Bytes(`<?xml version="1.0" encoding="UTF-8"?><a/>`, true, true), text: "", code: "encoding"},
		{name: "unsupported", data: []byte(`<?xml version="1.0" encoding="shift-jis"?><a/>`), text: "", code: "encoding"},
		{name: "surrogate", data: []byte{255, 254, 0, 216}, text: "", code: "encoding"},
		{name: "odd utf16", data: []byte{255, 254, 60}, text: "", code: "encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"normal", "single", "random"} {
				t.Run(mode, func(t *testing.T) {
					e, err := decodeTestReader(testReader(string(tc.data), mode), Options{})
					if tc.code != "" {
						code(t, err, tc.code)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tc.text, e.Text)
					}
				})
			}
		})
	}
}
func TestSpaceScope(t *testing.T) {
	e, err := Decode([]byte(`<a xml:space="preserve"> <b xml:space="default"> <c/> </b> </a>`), Options{Whitespace: "strip"})
	require.NoError(t, err)
	assert.Equal(t, "  ", Text(e))
	assert.Len(t, Child(e, "b", nil).Children, 1)
}
func TestLimitBoundaries(t *testing.T) {
	for _, tc := range []struct {
		s string
		o Options
	}{
		{"<a/>", Options{MaxTagBytes: 4, MaxBytes: 4, MaxNodeBytes: 4, MaxDepth: 1}},
		{`<a x="1"/>`, Options{MaxAttrs: 1}},
		{"<a>𐐀</a>", Options{MaxTokenBytes: 4}},
	} {
		_, err := Decode([]byte(tc.s), tc.o)
		require.NoError(t, err)
	}
}
func TestEventPosition(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader("<a>é\r\n<b/>\r</a>"), Options{})
	require.NoError(t, err)
	var positions []Position
	for {
		ev, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		positions = append(positions, ev.Pos)
	}
	assert.Equal(t, []Position{{Line: 1, Column: 1, Offset: 0}, {Line: 1, Column: 4, Offset: 3}, {Line: 2, Column: 1, Offset: 7}, {Line: 2, Column: 5, Offset: 11}, {Line: 2, Column: 5, Offset: 11}, {Line: 3, Column: 1, Offset: 12}}, positions)
}

func TestMalformedEncodingLocations(t *testing.T) {
	for _, s := range []string{"\xff<a/>", "<a/>\xff", "<\xff/>", "<?\xff?><a/>", "<a \xff='x'/>", "<a/>\xef\xbb"} {
		t.Run(s, func(t *testing.T) { _, err := Decode([]byte(s), Options{}); code(t, err, "encoding") })
	}
}
