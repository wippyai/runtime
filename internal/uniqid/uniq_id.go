package uniqid

import (
	"sync/atomic"
)

const hexDigits = "0123456789abcdef"

// Generator is a unique identifier generator using atomic counter.
type Generator struct {
	counter uint64
}

// NewGenerator creates a new generator instance.
func NewGenerator() *Generator {
	return &Generator{
		counter: 0,
	}
}

// Generate creates a new unique identifier.
// Format: "0x" followed by hex counter (minimum 5 digits, grows as needed).
// Example outputs: "0x00001", "0x00002", "0x100000"
func (g *Generator) Generate() string {
	count := atomic.AddUint64(&g.counter, 1)

	if count <= 0xFFFFF {
		// Fast path: 5 hex digits (covers first ~1M IDs)
		var buf [7]byte //nolint:gosec // fixed-size array, bounds are safe
		buf[0] = '0'
		buf[1] = 'x'
		buf[2] = hexDigits[(count>>16)&0xF]
		buf[3] = hexDigits[(count>>12)&0xF]
		buf[4] = hexDigits[(count>>8)&0xF]
		buf[5] = hexDigits[(count>>4)&0xF]
		buf[6] = hexDigits[count&0xF]
		return string(buf[:])
	}

	if count <= 0xFFFFFF {
		// 6 hex digits
		var buf [8]byte
		buf[0] = '0'
		buf[1] = 'x'
		buf[2] = hexDigits[(count>>20)&0xF]
		buf[3] = hexDigits[(count>>16)&0xF]
		buf[4] = hexDigits[(count>>12)&0xF]
		buf[5] = hexDigits[(count>>8)&0xF]
		buf[6] = hexDigits[(count>>4)&0xF]
		buf[7] = hexDigits[count&0xF]
		return string(buf[:])
	}

	if count <= 0xFFFFFFFF {
		// 8 hex digits (covers up to 4B IDs)
		var buf [10]byte //nolint:gosec // fixed-size array, bounds are safe
		buf[0] = '0'
		buf[1] = 'x'
		buf[2] = hexDigits[(count>>28)&0xF]
		buf[3] = hexDigits[(count>>24)&0xF]
		buf[4] = hexDigits[(count>>20)&0xF]
		buf[5] = hexDigits[(count>>16)&0xF]
		buf[6] = hexDigits[(count>>12)&0xF]
		buf[7] = hexDigits[(count>>8)&0xF]
		buf[8] = hexDigits[(count>>4)&0xF]
		buf[9] = hexDigits[count&0xF]
		return string(buf[:])
	}

	// 16 hex digits for very large counts
	var buf [18]byte
	buf[0] = '0'
	buf[1] = 'x'
	buf[2] = hexDigits[(count>>60)&0xF]
	buf[3] = hexDigits[(count>>56)&0xF]
	buf[4] = hexDigits[(count>>52)&0xF]
	buf[5] = hexDigits[(count>>48)&0xF]
	buf[6] = hexDigits[(count>>44)&0xF]
	buf[7] = hexDigits[(count>>40)&0xF]
	buf[8] = hexDigits[(count>>36)&0xF]
	buf[9] = hexDigits[(count>>32)&0xF]
	buf[10] = hexDigits[(count>>28)&0xF]
	buf[11] = hexDigits[(count>>24)&0xF]
	buf[12] = hexDigits[(count>>20)&0xF]
	buf[13] = hexDigits[(count>>16)&0xF]
	buf[14] = hexDigits[(count>>12)&0xF]
	buf[15] = hexDigits[(count>>8)&0xF]
	buf[16] = hexDigits[(count>>4)&0xF]
	buf[17] = hexDigits[count&0xF]
	return string(buf[:])
}

// Reset resets the counter to 0
// This can be called when the node restarts
func (g *Generator) Reset() {
	atomic.StoreUint64(&g.counter, 0)
}
