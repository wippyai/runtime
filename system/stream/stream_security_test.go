// SPDX-License-Identifier: MPL-2.0

package stream

import (
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/runtime/resource"
)

// ReadBuffered clamps oversized requests to actual buffer capacity.

func TestReadBuffered_OversizedReadClampedToBuffer(t *testing.T) {
	table := resource.NewTable()
	data := strings.Repeat("x", 70000)
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	// Request 100KB but buffer pool max is 64KB. Should read up to 64KB.
	buf, err := ReadBuffered(table, id, 100*1024)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	assert.Equal(t, 64*1024, buf.N, "read clamped to buffer capacity")
}

func TestReadBuffered_SizeAtPoolBoundary(t *testing.T) {
	table := resource.NewTable()
	data := strings.Repeat("x", 70000)
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	buf, err := ReadBuffered(table, id, 64*1024)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	assert.Equal(t, 64*1024, buf.N)
}

func TestReadBuffered_SizeJustOverPoolBoundarySafe(t *testing.T) {
	table := resource.NewTable()
	data := strings.Repeat("x", 70000)
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	// 64KB+1 exceeds pool buffer capacity but is clamped safely
	buf, err := ReadBuffered(table, id, 64*1024+1)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	assert.Equal(t, 64*1024, buf.N, "read clamped to 64KB buffer")
}

func TestReadBuffered_HugeSize(t *testing.T) {
	table := resource.NewTable()
	data := strings.Repeat("x", 1000)
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	// 1GB request must not panic or allocate excessive memory
	buf, err := ReadBuffered(table, id, 1024*1024*1024)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	assert.Equal(t, 1000, buf.N, "reads available data up to buffer capacity")
}

// Entry.Drop and Close share the closed field with no mutex.

func TestEntry_ConcurrentDropAndAccess(t *testing.T) {
	closed := false
	entry := &Entry{
		closer: &trackingCloser{Reader: strings.NewReader("data"), closed: &closed},
		reader: strings.NewReader("data"),
		caps:   Capabilities{Readable: true},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		entry.Drop()
	}()

	go func() {
		defer wg.Done()
		_ = entry.closed.Load()
	}()

	wg.Wait()
}
