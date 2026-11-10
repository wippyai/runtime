// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"io"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	feedBytes   = 64 << 20
	sampleEvery = 4 << 20
	heapBound   = 8 << 20
)

// feed generates an XML document of at least size bytes on demand and samples the
// live heap after a collection every sampleEvery bytes it produces.
type feed struct {
	pending  []byte
	off      int
	produced int
	size     int
	next     int
	records  int
	sampled  int
	maxHeap  uint64
	done     bool
	started  bool
}

func newFeed(size int) *feed { return &feed{size: size, next: sampleEvery} }

func (f *feed) sample() {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	f.maxHeap = max(f.maxHeap, m.HeapAlloc)
	f.sampled++
}

func (f *feed) fill() {
	if !f.started {
		f.started = true
		f.pending = append(f.pending, `<feed xmlns="urn:feed" xmlns:x="urn:x">`...)
		return
	}
	if f.produced >= f.size {
		f.pending = append(f.pending, `</feed>`...)
		f.done = true
		return
	}
	for range 64 {
		f.records++
		id := strconv.Itoa(f.records)
		f.pending = append(f.pending, `<item id="`...)
		f.pending = append(f.pending, id...)
		f.pending = append(f.pending, `" x:status="active"><name>Item `...)
		f.pending = append(f.pending, id...)
		f.pending = append(f.pending, `</name><price currency="USD">12.50</price><tags><tag>a</tag><tag>b</tag></tags></item>`...)
	}
}

func (f *feed) Read(p []byte) (int, error) {
	for f.off == len(f.pending) {
		if f.done {
			return 0, io.EOF
		}
		f.pending = f.pending[:0]
		f.off = 0
		f.fill()
	}
	n := copy(p, f.pending[f.off:])
	f.off += n
	f.produced += n
	if f.produced >= f.next {
		f.next += sampleEvery
		f.sample()
	}
	return n, nil
}

func baseline() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestBoundedMemoryRecords(t *testing.T) {
	e := newEnv(t)
	src := newFeed(feedBytes)
	e.global("src", e.stream(src))
	base := baseline()
	vals := e.run(`local r = check(xml.reader(src))
		local records = check(r:select("*:feed/*:item"))
		local count, priced = 0, 0
		while true do
			local batch, err = records:read(100)
			check(err == nil, tostring(err))
			if not batch then break end
			for _, item in ipairs(batch) do
				count = count + 1
				if xml.child(item, "price") then priced = priced + 1 end
			end
		end
		records:close()
		return count, priced`)
	assert.Equal(t, strconv.Itoa(src.records), vals[0].String())
	assert.Equal(t, vals[0].String(), vals[1].String())
	require.Greater(t, src.sampled, 10)
	growth := int64(src.maxHeap) - int64(base)
	t.Logf("records: %d bytes, %d records, live heap growth %d KiB over %d samples", src.produced, src.records, growth>>10, src.sampled)
	assert.Less(t, growth, int64(heapBound))
}

func TestBoundedMemoryCopy(t *testing.T) {
	e := newEnv(t)
	src := newFeed(feedBytes)
	e.global("src", e.stream(src))
	dst := &discard{}
	sink, _ := e.sink(dst)
	e.global("dest", sink)
	base := baseline()
	e.run(`local r = check(xml.reader(src))
		local w = check(xml.writer(dest))
		eq(r:next(), "start")
		check(r:copy(w))
		local _, ferr = w:finish()
		check(ferr == nil, tostring(ferr))
		eq(r:next(), nil, "end of document")`)
	require.Greater(t, src.sampled, 10)
	growth := int64(src.maxHeap) - int64(base)
	t.Logf("copy: %d bytes in, %d bytes out, live heap growth %d KiB over %d samples", src.produced, dst.n, growth>>10, src.sampled)
	assert.Less(t, growth, int64(heapBound))
	assert.Greater(t, dst.n, int64(feedBytes))
}

type discard struct{ n int64 }

func (d *discard) Write(p []byte) (int, error) {
	d.n += int64(len(p))
	return len(p), nil
}
