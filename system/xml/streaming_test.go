// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestStripRunBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		limit             int
	}{
		{"at limit", "<a>        <b/> </a>", "", 8},
		{"over limit", "<a>         <b/> </a>", "         ", 8},
		{"source byte limit", "<a>\r\n\r\n\r\n<b/></a>", "\n\n\n", 4},
		{"refill", "<a>" + strings.Repeat(" ", bufferBytes) + "x<b/> </a>", strings.Repeat(" ", bufferBytes) + "x", bufferBytes + 1},
		{"cdata", "<a> <![CDATA[   ]]> <b/> </a>", "   ", 8},
		{"reference", "<a> &#32; <b/> </a>", "   ", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Decode([]byte(tc.input), Options{Whitespace: "strip", MaxTokenBytes: tc.limit})
			require.NoError(t, err)
			require.Equal(t, tc.want, Text(e))
		})
	}
}

func TestCopyIncrementalIndentAndDrain(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r xmlns:p="u"><p:a z="2" a="1"/><p:b>text<c/></p:b></r>`), Options{})
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	w, err := NewEncoder(EncodeOptions{Indent: "  "})
	require.NoError(t, err)
	var out strings.Builder
	require.NoError(t, d.CopyTo(w, func(w *Encoder) error { out.Write(w.Drain()); return nil }))
	require.NoError(t, w.Finish())
	require.Equal(t, "<r xmlns:p=\"u\">\n  <p:a a=\"1\" z=\"2\"/>\n  <p:b>text<c/></p:b>\n</r>", out.String())
}

func TestCopyPreservesCallerValidation(t *testing.T) {
	for _, tc := range []struct {
		write func(*Encoder) error
		name  string
	}{
		{name: "text", write: func(w *Encoder) error { return w.Text("\x00") }},
		{name: "name", write: func(w *Encoder) error { return w.StartElement("bad:name", nil, "") }},
		{name: "attribute", write: func(w *Encoder) error { return w.StartElement("a", map[string]string{"x": "\x00"}, "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDecoder(context.Background(), strings.NewReader(`<a/>`), Options{})
			require.NoError(t, err)
			_, err = d.Next()
			require.NoError(t, err)
			w, err := NewEncoder(EncodeOptions{})
			require.NoError(t, err)
			require.NoError(t, w.StartElement("root", nil, ""))
			require.NoError(t, d.Copy(w))
			var e apierror.Error
			require.ErrorAs(t, tc.write(w), &e)
			require.Equal(t, "element", e.Details().GetString("code", ""))
		})
	}
}

func TestCopyCommentsDoNotCountAsText(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<a><!--c--><?p x?><b/></a>`), Options{Comments: true, Instructions: true})
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	w, err := NewEncoder(EncodeOptions{Indent: "  "})
	require.NoError(t, err)
	require.NoError(t, d.Copy(w))
	require.NoError(t, w.Finish())
	require.Equal(t, "<a><!--c--><?p x?>\n  <b/>\n</a>", string(w.Drain()))
}

func TestInvalidDeclarationCharacters(t *testing.T) {
	for _, uri := range []string{"\x00", "\xff", "\ufffe"} {
		t.Run(uri, func(t *testing.T) {
			_, err := Encode(&Element{Name: "a", XMLNS: map[string]string{"p": uri}}, EncodeOptions{})
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			require.Equal(t, "namespace", e.Details().GetString("code", ""))
			_, err = NewEncoder(EncodeOptions{NS: map[string]string{"p": uri}})
			require.ErrorAs(t, err, &e)
			require.Equal(t, "namespace", e.Details().GetString("code", ""))
		})
	}
}
