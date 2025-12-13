package uniqid

import (
	"github.com/wippyai/runtime/api/pid"
)

// PIDGenerator wraps Generator for PID creation with auto-generated UniqID
type PIDGenerator struct {
	gen    *Generator
	nodeID pid.NodeID
}

// NewPIDGenerator creates a PID generator using a uniqid generator and optional node ID
func NewPIDGenerator(gen *Generator, nodeID pid.NodeID) *PIDGenerator {
	return &PIDGenerator{
		gen:    gen,
		nodeID: nodeID,
	}
}

// Generate creates a PID with host, auto-generating UniqID.
// Uses the configured node ID if set.
func (p *PIDGenerator) Generate(host pid.HostID) pid.PID {
	return pid.PID{
		Node:   p.nodeID,
		Host:   host,
		UniqID: p.gen.Generate(),
	}.Precomputed()
}
