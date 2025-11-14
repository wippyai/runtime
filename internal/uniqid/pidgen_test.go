package uniqid_test

import (
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/internal/uniqid"
)

func TestNewPIDGenerator(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	if pidGen == nil {
		t.Fatal("NewPIDGenerator returned nil")
	}
}

func TestPIDGenerator_Generate(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	host := relay.HostID("functions")
	id := registry.ID{NS: "process", Name: "worker"}

	pid := pidGen.Generate(host, id)

	if pid.Host != host {
		t.Errorf("Expected host %q, got %q", host, pid.Host)
	}
	if pid.UniqID == "" {
		t.Error("Expected UniqID to be generated, got empty string")
	}
	if pid.UniqID != "0x00001" {
		t.Errorf("Expected first UniqID to be '0x00001', got %q", pid.UniqID)
	}
	if pid.Node != "" {
		t.Errorf("Expected empty node, got %q", pid.Node)
	}
}

func TestPIDGenerator_GenerateWithConfiguredNode(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	node := relay.NodeID("node1")
	host := relay.HostID("functions")
	id := registry.ID{NS: "process", Name: "worker"}

	pidGen = uniqid.NewPIDGenerator(gen, node)
	pid := pidGen.Generate(host, id)

	if pid.Node != node {
		t.Errorf("Expected node %q, got %q", node, pid.Node)
	}
	if pid.Host != host {
		t.Errorf("Expected host %q, got %q", host, pid.Host)
	}
	if pid.UniqID == "" {
		t.Error("Expected UniqID to be generated, got empty string")
	}
	if pid.UniqID != "0x00001" {
		t.Errorf("Expected first UniqID to be '0x00001', got %q", pid.UniqID)
	}
}

func TestPIDGenerator_SequentialUniqID(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	host := relay.HostID("functions")
	id := registry.ID{NS: "process", Name: "worker"}

	expected := []string{"0x00001", "0x00002", "0x00003", "0x00004", "0x00005"}

	for i, expectedUniqID := range expected {
		pid := pidGen.Generate(host, id)
		if pid.UniqID != expectedUniqID {
			t.Errorf("Iteration %d: expected UniqID %q, got %q", i, expectedUniqID, pid.UniqID)
		}
	}
}

func TestPIDGenerator_StringFormat(t *testing.T) {
	tests := []struct {
		name        string
		node        relay.NodeID
		host        relay.HostID
		id          registry.ID
		expectedFmt string
		useNode     bool
	}{
		{
			name:        "without node",
			host:        relay.HostID("functions"),
			id:          registry.ID{NS: "process", Name: "worker"},
			expectedFmt: "{functions|0x00001}",
			useNode:     false,
		},
		{
			name:        "with node",
			node:        relay.NodeID("node1"),
			host:        relay.HostID("functions"),
			id:          registry.ID{NS: "process", Name: "worker"},
			expectedFmt: "{node1@functions|0x00001}",
			useNode:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := uniqid.NewGenerator()
			var pid relay.PID
			if tt.useNode {
				pidGenWithNode := uniqid.NewPIDGenerator(gen, tt.node)
				pid = pidGenWithNode.Generate(tt.host, tt.id)
			} else {
				pidGen := uniqid.NewPIDGenerator(gen, "")
				pid = pidGen.Generate(tt.host, tt.id)
			}

			pidStr := pid.String()
			if pidStr != tt.expectedFmt {
				t.Errorf("Expected PID string %q, got %q", tt.expectedFmt, pidStr)
			}
		})
	}
}
