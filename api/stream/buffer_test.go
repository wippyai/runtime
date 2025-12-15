package stream

import (
	"testing"
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
	var buf *Buffer
	buf.Release() // should not panic

	bytes := buf.Bytes()
	if bytes != nil {
		t.Error("expected nil bytes from nil buffer")
	}
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
