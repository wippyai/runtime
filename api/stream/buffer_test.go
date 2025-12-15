package stream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/dispatcher"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool(1024)

	buf := pool.Acquire()
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	if len(buf.Data) != 1024 {
		t.Errorf("expected buffer size 1024, got %d", len(buf.Data))
	}
	if buf.N != 0 {
		t.Errorf("expected N=0, got %d", buf.N)
	}

	buf.N = 100
	buf.Release()

	buf2 := pool.Acquire()
	if buf2.N != 0 {
		t.Errorf("expected N reset to 0 after reuse, got %d", buf2.N)
	}
	buf2.Release()
}

func TestBufferBytes(t *testing.T) {
	pool := NewBufferPool(100)
	buf := pool.Acquire()
	defer buf.Release()

	copy(buf.Data, "hello")
	buf.N = 5

	bytes := buf.Bytes()
	if string(bytes) != "hello" {
		t.Errorf("expected 'hello', got '%s'", bytes)
	}
}

func TestBufferNilSafe(t *testing.T) {
	// Buffer methods are nil-safe by design (check method implementations)
	var buf *Buffer

	t.Run("Release", func(t *testing.T) {
		buf.Release() // should not panic
	})

	t.Run("Bytes", func(t *testing.T) {
		bytes := buf.Bytes()
		if bytes != nil {
			t.Error("expected nil bytes from nil buffer")
		}
	})
}

func TestAcquireBuffer(t *testing.T) {
	tests := []struct {
		size     int
		expected int
	}{
		{0, 32 * 1024},
		{-1, 32 * 1024},
		{100, 4 * 1024},
		{4 * 1024, 4 * 1024},
		{5 * 1024, 32 * 1024},
		{32 * 1024, 32 * 1024},
		{33 * 1024, 64 * 1024},
	}

	for _, tt := range tests {
		buf := AcquireBuffer(tt.size)
		if len(buf.Data) != tt.expected {
			t.Errorf("AcquireBuffer(%d): expected size %d, got %d", tt.size, tt.expected, len(buf.Data))
		}
		buf.Release()
	}
}

func TestBufferPoolSize(t *testing.T) {
	pool := NewBufferPool(2048)
	if pool.Size() != 2048 {
		t.Errorf("expected Size()=2048, got %d", pool.Size())
	}
}

func BenchmarkBufferPoolAcquireRelease(b *testing.B) {
	pool := NewBufferPool(4096)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Acquire()
		buf.Release()
	}
}

func BenchmarkAcquireBuffer(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := AcquireBuffer(1024)
		buf.Release()
	}
}

func BenchmarkBufferPoolParallel(b *testing.B) {
	pool := NewBufferPool(4096)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Acquire()
			buf.Release()
		}
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(50), Read)
	assert.Equal(t, dispatcher.CommandID(51), Close)
	assert.Equal(t, dispatcher.CommandID(52), Write)
	assert.Equal(t, dispatcher.CommandID(53), Seek)
	assert.Equal(t, dispatcher.CommandID(54), Flush)
	assert.Equal(t, dispatcher.CommandID(55), Stat)
	assert.Equal(t, dispatcher.CommandID(56), ScannerCreate)
	assert.Equal(t, dispatcher.CommandID(57), ScannerScan)
}

func TestSeekConstants(t *testing.T) {
	assert.Equal(t, 0, SeekStart)
	assert.Equal(t, 1, SeekCurrent)
	assert.Equal(t, 2, SeekEnd)
}

func TestSplitConstants(t *testing.T) {
	assert.Equal(t, 0, SplitLines)
	assert.Equal(t, 1, SplitWords)
	assert.Equal(t, 2, SplitBytes)
	assert.Equal(t, 3, SplitRunes)
}

func TestReadCmd(t *testing.T) {
	cmd := ReadCmd{StreamID: 123, Size: 1024}
	assert.Equal(t, Read, cmd.CmdID())
	assert.Equal(t, uint64(123), cmd.StreamID)
	assert.Equal(t, int64(1024), cmd.Size)
}

func TestCloseCmd(t *testing.T) {
	cmd := CloseCmd{StreamID: 456}
	assert.Equal(t, Close, cmd.CmdID())
	assert.Equal(t, uint64(456), cmd.StreamID)
}

func TestWriteCmd(t *testing.T) {
	cmd := WriteCmd{StreamID: 789, Data: []byte("hello")}
	assert.Equal(t, Write, cmd.CmdID())
	assert.Equal(t, uint64(789), cmd.StreamID)
	assert.Equal(t, []byte("hello"), cmd.Data)
}

func TestSeekCmd(t *testing.T) {
	cmd := SeekCmd{StreamID: 100, Offset: 50, Whence: SeekCurrent}
	assert.Equal(t, Seek, cmd.CmdID())
	assert.Equal(t, uint64(100), cmd.StreamID)
	assert.Equal(t, int64(50), cmd.Offset)
	assert.Equal(t, SeekCurrent, cmd.Whence)
}

func TestFlushCmd(t *testing.T) {
	cmd := FlushCmd{StreamID: 200}
	assert.Equal(t, Flush, cmd.CmdID())
	assert.Equal(t, uint64(200), cmd.StreamID)
}

func TestStatCmd(t *testing.T) {
	cmd := StatCmd{StreamID: 300}
	assert.Equal(t, Stat, cmd.CmdID())
	assert.Equal(t, uint64(300), cmd.StreamID)
}

func TestInfo(t *testing.T) {
	info := Info{
		Size:     1024,
		Position: 50,
		Readable: true,
		Writable: true,
		Seekable: true,
	}
	assert.Equal(t, int64(1024), info.Size)
	assert.Equal(t, int64(50), info.Position)
	assert.True(t, info.Readable)
	assert.True(t, info.Writable)
	assert.True(t, info.Seekable)
}

func TestScannerCreateCmd(t *testing.T) {
	cmd := ScannerCreateCmd{StreamID: 400, SplitType: SplitLines}
	assert.Equal(t, ScannerCreate, cmd.CmdID())
	assert.Equal(t, uint64(400), cmd.StreamID)
	assert.Equal(t, SplitLines, cmd.SplitType)
}

func TestScannerScanCmd(t *testing.T) {
	cmd := ScannerScanCmd{ScannerID: 500}
	assert.Equal(t, ScannerScan, cmd.CmdID())
	assert.Equal(t, uint64(500), cmd.ScannerID)
}

func TestScanResult(t *testing.T) {
	result := ScanResult{
		HasToken: true,
		Text:     "test token",
		Error:    "",
	}
	assert.True(t, result.HasToken)
	assert.Equal(t, "test token", result.Text)
	assert.Empty(t, result.Error)
}

func TestStreamErrors(t *testing.T) {
	assert.EqualError(t, ErrNotFound, "stream not found")
	assert.EqualError(t, ErrClosed, "stream closed")
	assert.EqualError(t, ErrNotReadable, "stream is not readable")
	assert.EqualError(t, ErrNotWritable, "stream is not writable")
	assert.EqualError(t, ErrNotSeekable, "stream is not seekable")
	assert.EqualError(t, ErrNoTable, "resource table not available")
	assert.EqualError(t, ErrScannerNotFound, "scanner not found")
}
