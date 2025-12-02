package uniqid

import (
	"github.com/wippyai/runtime/api/relay"
)

// PIDGenerator wraps Generator for PID creation with auto-generated UniqID
type PIDGenerator struct {
	gen    *Generator
	nodeID relay.NodeID
}

// NewPIDGenerator creates a PID generator using a uniqid generator and optional node ID
func NewPIDGenerator(gen *Generator, nodeID relay.NodeID) *PIDGenerator {
	return &PIDGenerator{
		gen:    gen,
		nodeID: nodeID,
	}
}

// Generate creates a PID with host, auto-generating UniqID.
// Uses the configured node ID if set.
func (p *PIDGenerator) Generate(host relay.HostID) relay.PID {
	return relay.PID{
		Node:   p.nodeID,
		Host:   host,
		UniqID: p.gen.Generate(),
	}.Precomputed()
}
