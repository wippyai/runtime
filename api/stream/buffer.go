package stream

import "sync"

// DefaultBufferSize is the default size for pooled buffers.
const DefaultBufferSize = 32 * 1024 // 32KB

// Buffer is a pooled byte buffer for stream operations.
// After use, call Release() to return it to the pool.
type Buffer struct {
	pool *sync.Pool
	Data []byte
	N    int
}

// Release returns the buffer to its pool.
// The buffer must not be used after calling Release.
func (b *Buffer) Release() {
	if b == nil || b.pool == nil {
		return
	}
	b.N = 0
	b.pool.Put(b)
}

// Bytes returns the valid portion of the buffer (Data[:N]).
func (b *Buffer) Bytes() []byte {
	if b == nil {
		return nil
	}
	return b.Data[:b.N]
}

// BufferPool manages pooled buffers of a specific size.
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a buffer pool for buffers of the given size.
func NewBufferPool(size int) *BufferPool {
	if size <= 0 {
		size = DefaultBufferSize
	}
	bp := &BufferPool{size: size}
	bp.pool.New = func() any {
		return &Buffer{
			Data: make([]byte, size),
			pool: &bp.pool,
		}
	}
	return bp
}

// Acquire gets a buffer from the pool.
func (bp *BufferPool) Acquire() *Buffer {
	buf := bp.pool.Get().(*Buffer)
	buf.N = 0
	return buf
}

// Size returns the buffer size for this pool.
func (bp *BufferPool) Size() int {
	return bp.size
}

// Common buffer pools for typical read sizes.
var (
	SmallBufferPool  = NewBufferPool(4 * 1024)  // 4KB
	MediumBufferPool = NewBufferPool(32 * 1024) // 32KB
	LargeBufferPool  = NewBufferPool(64 * 1024) // 64KB
)

// AcquireBuffer returns a buffer appropriate for the requested size.
// If size <= 0, uses DefaultBufferSize.
func AcquireBuffer(size int) *Buffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	switch {
	case size <= 4*1024:
		return SmallBufferPool.Acquire()
	case size <= 32*1024:
		return MediumBufferPool.Acquire()
	default:
		return LargeBufferPool.Acquire()
	}
}
