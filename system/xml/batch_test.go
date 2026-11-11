// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordsAndHelpers(t *testing.T) {
	src := `<r xmlns:p="u"><p:a id="1"><p:a/>x</p:a><p:a id="2"/></r>`
	for _, path := range []string{"r/p:a", "/r/*:a", "r/p:*", "r/*"} {
		t.Run(path, func(t *testing.T) {
			p, err := CompilePath(path, map[string]string{"p": "u"})
			require.NoError(t, err)
			d, err := NewDecoder(context.Background(), strings.NewReader(src), Options{})
			require.NoError(t, err)
			s, err := NewSelector(d, []Path{p, p})
			require.NoError(t, err)
			var b Batch
			n, err := s.Read(&b, 64)
			require.NoError(t, err)
			require.Equal(t, 2, n)
			trees, err := b.Elements()
			require.NoError(t, err)
			require.Len(t, trees, 2)
			assert.Equal(t, "1", trees[0].Attrs["id"])
			assert.Equal(t, map[string]string{"p": "u"}, trees[0].XMLNS)
			assert.Equal(t, "x", Text(trees[0]))
			local, ns, err := Resolve(trees[0], "p:T")
			require.NoError(t, err)
			assert.Equal(t, "T", local)
			assert.Equal(t, "u", ns)
			assert.NotNil(t, Child(trees[0], "a", nil))
			n, err = s.Read(&b, 1)
			assert.Zero(t, n)
			require.ErrorIs(t, err, io.EOF)
		})
	}
	p, err := CompilePath("p:a", map[string]string{"p": "u"})
	require.NoError(t, err)
	e, err := Decode([]byte(src), Options{})
	require.NoError(t, err)
	assert.Len(t, Find(e, p), 2)
	for _, bad := range []string{"", "//a", "a/", "a//b", "q:a", "a[1]", "a:b:c"} {
		_, err := CompilePath(bad, nil)
		require.Error(t, err, bad)
	}
}
func TestNodeAndBatchOwnership(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r xmlns="u" xmlns:p="v"><a>one<b/>two</a><a/></r>`), Options{})
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	var b Batch
	require.NoError(t, d.AppendNode(&b))
	require.Equal(t, EndEvent, d.Current().Kind)
	_, err = d.Next()
	require.NoError(t, err)
	require.NoError(t, d.Skip())
	d.Close()
	es, err := b.Elements()
	require.NoError(t, err)
	require.Len(t, es, 1)
	assert.Equal(t, map[string]string{"": "u", "p": "v"}, es[0].XMLNS)
	assert.Equal(t, "onetwo", Text(es[0]))
	assert.Nil(t, Child(es[0], "b", nil).XMLNS)
}

func TestSelectorOwnershipAndErrors(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r><a/><a></bad></r>`), Options{})
	require.NoError(t, err)
	p, err := CompilePath("r/a", nil)
	require.NoError(t, err)
	s, err := NewSelector(d, []Path{p})
	require.NoError(t, err)
	_, err = d.Next()
	require.Error(t, err)
	var b Batch
	n, err := s.Read(&b, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	b.Reset()
	_, first := s.Read(&b, 1)
	require.Error(t, first)
	_, err = s.Read(&b, 1)
	assert.Same(t, first, err)
}
func TestEventBatchReplay(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<a x="1">text<b/></a>`), Options{})
	require.NoError(t, err)
	var b Batch
	n, err := d.AppendEvents(&b, 32, 1<<20)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 5, n)
	d.Close()
	trees, err := b.Elements()
	require.NoError(t, err)
	require.Len(t, trees, 1)
	assert.Equal(t, "text", Text(trees[0]))
	b.Reset()
	assert.Zero(t, b.Len())
}
func TestOutermostRecords(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r><a><b/></a><a><b/></a></r>`), Options{})
	require.NoError(t, err)
	a, err := CompilePath("r/a", nil)
	require.NoError(t, err)
	b, err := CompilePath("r/a/b", nil)
	require.NoError(t, err)
	s, err := NewSelector(d, []Path{a, b})
	require.NoError(t, err)
	var batch Batch
	n, err := s.Read(&batch, 64)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	trees, err := batch.Elements()
	require.NoError(t, err)
	assert.Equal(t, "a", trees[0].Name)
}

func TestReplayConcurrent(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<a x="v"><b>text</b></a>`), Options{})
	require.NoError(t, err)
	var batch Batch
	_, err = d.AppendEvents(&batch, 32, 1<<20)
	require.ErrorIs(t, err, io.EOF)
	d.Close()
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := batch.Elements(); done <- err }()
	}
	for range 2 {
		require.NoError(t, <-done)
	}
}

func TestHelpers(t *testing.T) {
	e, err := Decode([]byte(`<r xmlns:p="u" id="1" p:id="2"><a/><p:a/>text<b>inner</b></r>`), Options{})
	require.NoError(t, err)
	v, ok := Attr(e, "id", "")
	assert.True(t, ok)
	assert.Equal(t, "1", v)
	v, ok = Attr(e, "id", "u")
	assert.True(t, ok)
	assert.Equal(t, "2", v)
	_, ok = Attr(e, "missing", "")
	assert.False(t, ok)
	empty := ""
	u := "u"
	a := "a"
	assert.Len(t, Children(e, nil, nil), 3)
	assert.Len(t, Children(e, &a, nil), 2)
	assert.Len(t, Children(e, &a, &empty), 1)
	assert.Equal(t, "u", Child(e, "a", &u).NS)
	assert.Nil(t, Child(e, "missing", nil))
	assert.Equal(t, "textinner", Text(e))
	local, ns, err := Resolve(e, "xml:lang")
	require.NoError(t, err)
	assert.Equal(t, "lang", local)
	assert.Equal(t, XMLNamespace, ns)
	_, _, err = Resolve(e, "q:x")
	require.Error(t, err)
}
