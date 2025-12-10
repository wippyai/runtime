package uniqid

import (
	"fmt"
	"sync/atomic"
)

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
	return fmt.Sprintf("0x%05x", count)
}

// Reset resets the counter to 0
// This can be called when the node restarts
func (g *Generator) Reset() {
	atomic.StoreUint64(&g.counter, 0)
}
