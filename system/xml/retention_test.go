// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entries counts everything the namespace structure holds.
func (n *namespaces) entries() int {
	total := len(n.list) + len(n.shadow) + len(n.hidden) + len(n.slot) + len(n.top) + len(n.byURI)
	for _, h := range n.byURI {
		total += len(h.items)
	}
	return total
}

func liveHeap() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestEncoderNamespacesStayFlatOverSiblings(t *testing.T) {
	for _, form := range []string{"incremental", "tree"} {
		t.Run(form, func(t *testing.T) {
			w, err := NewEncoder(EncodeOptions{})
			require.NoError(t, err)
			require.NoError(t, w.StartNode("root", "urn:u", nil, []Binding{{"p", "urn:u"}}, false))
			child := &Element{Name: "c", NS: "urn:u", XMLNS: map[string]string{"p": "urn:u"}, Attrs: map[string]string{"{urn:u}k": "v"}}
			shadow := &Element{Name: "d", XMLNS: map[string]string{"": "urn:v", "p": "urn:u"}}
			write := func(i int) {
				switch {
				case form == "tree" && i%2 == 0:
					require.NoError(t, w.Element(child))
				case form == "tree":
					require.NoError(t, w.Element(shadow))
				case i%2 == 0:
					require.NoError(t, w.StartNode("c", "urn:u", []AttrValue{{"{urn:u}k", "v"}}, []Binding{{"p", "urn:u"}}, false))
					require.NoError(t, w.EndElement())
				default:
					require.NoError(t, w.StartNode("d", "", nil, []Binding{{"", "urn:v"}, {"p", "urn:u"}}, false))
					require.NoError(t, w.EndElement())
				}
				if i%1000 == 0 {
					require.NoError(t, w.Flush(io.Discard))
				}
			}
			for i := range 10_000 {
				write(i)
			}
			entries, heap := w.ns.entries(), liveHeap()
			for i := 10_000; i < 1_000_000; i++ {
				write(i)
			}
			require.NoError(t, w.Flush(io.Discard))
			assert.Equal(t, entries, w.ns.entries(), "the namespace structure holds only bindings in scope")
			assert.Less(t, int64(liveHeap())-int64(heap), int64(256<<10))
			runtime.KeepAlive(w)
		})
	}
}

type siblings struct {
	pending []byte
	n, i    int
}

func (s *siblings) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		switch {
		case s.i == 0:
			s.pending = []byte(`<r xmlns:p="urn:u">`)
		case s.i <= s.n:
			s.pending = []byte(`<p:c xmlns:p="urn:u" xmlns="urn:v"/>`)
		case s.i == s.n+1:
			s.pending = []byte(`</r>`)
		default:
			return 0, io.EOF
		}
		s.i++
	}
	k := copy(p, s.pending)
	s.pending = s.pending[k:]
	return k, nil
}

func TestDecoderNamespacesStayFlatOverSiblings(t *testing.T) {
	d, err := NewDecoder(context.Background(), &siblings{n: 1_000_000}, Options{})
	require.NoError(t, err)
	var entries int
	var heap uint64
	for count := 0; ; count++ {
		_, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if count == 20_000 {
			entries, heap = d.ns.entries(), liveHeap()
		}
		if count == 1_999_000 {
			assert.Equal(t, entries, d.ns.entries())
			assert.Less(t, int64(liveHeap())-int64(heap), int64(256<<10))
		}
	}
}

// prefixed streams elements whose long prefix is shared and whose local names differ.
type prefixed struct {
	prefix  string
	pending []byte
	n, i    int
}

func newPrefixed(prefixBytes, n int) *prefixed {
	return &prefixed{prefix: "p" + strings.Repeat("x", prefixBytes-1), n: n}
}

func (s *prefixed) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		switch {
		case s.i == 0:
			s.pending = []byte(`<r xmlns:` + s.prefix + `="urn:x">`)
		case s.i <= s.n:
			s.pending = []byte(fmt.Sprintf(`<%s:n%06d/>`, s.prefix, s.i))
		case s.i == s.n+1:
			s.pending = []byte(`</r>`)
		default:
			return 0, io.EOF
		}
		s.i++
	}
	k := copy(p, s.pending)
	s.pending = s.pending[k:]
	return k, nil
}

func TestShortNamesDoNotPinLongPrefixes(t *testing.T) {
	base := liveHeap()
	d, err := NewDecoder(context.Background(), newPrefixed(64<<10, 500), Options{})
	require.NoError(t, err)
	c, err := NewCursor(d, 64<<10)
	require.NoError(t, err)
	names := 0
	for {
		ev, ok := c.Next()
		if !ok {
			err := c.Fill()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			continue
		}
		if ev.Kind == StartEvent {
			names++
		}
	}
	growth := int64(liveHeap()) - int64(base)
	t.Logf("live growth with the cursor open: %d KiB", growth>>10)
	assert.Equal(t, 501, names)
	assert.Less(t, growth, int64(4<<20), "a 13-byte local name must not keep its 64 KiB qualified name alive")
	runtime.KeepAlive(c)
}

// distinct streams siblings with distinct names, attribute values and namespace URIs.
type distinct struct {
	pending []byte
	n, i    int
}

func (s *distinct) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		switch {
		case s.i == 0:
			s.pending = []byte(`<r>`)
		case s.i <= s.n:
			s.pending = []byte(fmt.Sprintf(`<n%07d%s xmlns="urn:%07d%s" v="%07d"/>`, s.i, strings.Repeat("x", 900), s.i, strings.Repeat("u", 900), s.i))
		case s.i == s.n+1:
			s.pending = []byte(`</r>`)
		default:
			return 0, io.EOF
		}
		s.i++
	}
	k := copy(p, s.pending)
	s.pending = s.pending[k:]
	return k, nil
}

func TestDecoderCachesStayBoundedOverDistinctNames(t *testing.T) {
	d, err := NewDecoder(context.Background(), &distinct{n: 200_000}, Options{})
	require.NoError(t, err)
	var heap uint64
	for count := 0; ; count++ {
		_, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if count == 40_000 {
			heap = liveHeap()
		}
	}
	assert.LessOrEqual(t, len(d.names), maxInternEntries)
	assert.LessOrEqual(t, len(d.interned), maxInternEntries)
	for _, slot := range d.recent {
		assert.LessOrEqual(t, len(slot.raw), maxInternBytes, "recent slots hold only cacheable names")
	}
	growth := int64(liveHeap()) - int64(heap)
	t.Logf("growth from 20k to 200k distinct siblings: %d KiB", growth>>10)
	assert.Less(t, growth, int64(1<<20))
	runtime.KeepAlive(d)
}

func TestEncoderRetainsNoCallerSubstrings(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{MaxTagBytes: 1 << 30, MaxNodeBytes: 1 << 30})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("root", nil, ""))
	base := liveHeap()
	for i := range 200 {
		uri := fmt.Sprintf("urn:%d:%s", i, strings.Repeat("u", 256<<10))
		key := "{" + uri + "}" + fmt.Sprintf("k%03d", i)
		require.NoError(t, w.StartElement(fmt.Sprintf("e%03d", i), map[string]string{key: "v"}, ""))
		require.NoError(t, w.EndElement())
		require.NoError(t, w.Flush(io.Discard))
	}
	open := int64(liveHeap()) - int64(base)
	t.Logf("retained after the elements closed: %d KiB", open>>10)
	assert.Less(t, open, int64(1<<20), "closed elements leave nothing of their keys or URIs behind")
	w.Close()
	closed := int64(liveHeap()) - int64(base)
	assert.Less(t, closed, int64(256<<10), "Close releases the name cache")
	runtime.KeepAlive(w)
}
