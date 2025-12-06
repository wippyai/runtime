package testnet

import (
	"math/rand"
	"sync"
	"time"
)

// Link represents a directional network link between two nodes.
type Link struct {
	mu         sync.RWMutex
	blocked    bool
	latency    time.Duration
	packetLoss float64
}

func newLink() *Link {
	return &Link{}
}

// Block prevents all traffic on this link.
func (l *Link) Block() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blocked = true
}

// Allow permits traffic on this link.
func (l *Link) Allow() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blocked = false
}

// IsBlocked returns whether the link is blocked.
func (l *Link) IsBlocked() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.blocked
}

// SetLatency adds delay to packets on this link.
func (l *Link) SetLatency(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.latency = d
}

// Latency returns the configured latency.
func (l *Link) Latency() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.latency
}

// SetPacketLoss sets the probability of dropping packets (0.0-1.0).
func (l *Link) SetPacketLoss(rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	l.packetLoss = rate
}

// PacketLoss returns the configured packet loss rate.
func (l *Link) PacketLoss() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.packetLoss
}

// ShouldDeliver returns whether a packet should be delivered.
// Returns false if blocked or randomly dropped due to packet loss.
func (l *Link) ShouldDeliver() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.blocked {
		return false
	}

	if l.packetLoss > 0 && rand.Float64() < l.packetLoss {
		return false
	}

	return true
}

// Reset clears all link configuration.
func (l *Link) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blocked = false
	l.latency = 0
	l.packetLoss = 0
}
