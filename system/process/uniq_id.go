package process

import (
	"fmt"
	"sync/atomic"
)

type UniqIDGenerator struct {
	counter uint64
}

// NewUniqIDGenerator creates a new generator instance
// prefix is the system identifier (e.g. "terminal", "discord:app")
func NewUniqIDGenerator() *UniqIDGenerator {
	return &UniqIDGenerator{
		counter: 0,
	}
}

// Generate creates a new unique identifier
// Format: {system}|{addr} where:
// - system is the prefix like "terminal" or "discord:app"
// - addr is the memory address with counter
// Example outputs: "terminal|0xc001", "discord:app|0xc002"
func (g *UniqIDGenerator) Generate() string {
	count := atomic.AddUint64(&g.counter, 1)
	return fmt.Sprintf("0x%05x", count)
}

// Reset resets the counter to 0
// This can be called when the node restarts
func (g *UniqIDGenerator) Reset() {
	atomic.StoreUint64(&g.counter, 0)
}
