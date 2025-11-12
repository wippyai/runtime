package uniqid

import (
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// PIDGenerator wraps Generator for PID creation with auto-generated UniqID
type PIDGenerator struct {
	gen    *Generator
	nodeID pubsub.NodeID
}

// NewPIDGenerator creates a PID generator using a uniqid generator and optional node ID
func NewPIDGenerator(gen *Generator, nodeID pubsub.NodeID) *PIDGenerator {
	return &PIDGenerator{
		gen:    gen,
		nodeID: nodeID,
	}
}

// Generate creates a PID with host and id, auto-generating UniqID.
// Uses the configured node ID if set.
func (p *PIDGenerator) Generate(host pubsub.HostID, id registry.ID) pubsub.PID {
	return pubsub.PID{
		Node:   p.nodeID,
		Host:   host,
		UniqID: p.gen.Generate(),
	}.Precomputed()
}
