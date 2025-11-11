package uniqid_test

import (
	"testing"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/uniqid"
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

	host := pubsub.HostID("functions")
	id := registry.ID{NS: "process", Name: "worker"}

	pid := pidGen.Generate(host, id)

	if pid.Host != host {
		t.Errorf("Expected host %q, got %q", host, pid.Host)
	}
	if pid.ID != id {
		t.Errorf("Expected id %v, got %v", id, pid.ID)
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

func TestPIDGenerator_GenerateWithNode(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	node := pubsub.NodeID("node1")
	host := pubsub.HostID("functions")
	id := registry.ID{NS: "process", Name: "worker"}

	pid := pidGen.GenerateWithNode(node, host, id)

	if pid.Node != node {
		t.Errorf("Expected node %q, got %q", node, pid.Node)
	}
	if pid.Host != host {
		t.Errorf("Expected host %q, got %q", host, pid.Host)
	}
	if pid.ID != id {
		t.Errorf("Expected id %v, got %v", id, pid.ID)
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

	host := pubsub.HostID("functions")
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
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	tests := []struct {
		name        string
		node        pubsub.NodeID
		host        pubsub.HostID
		id          registry.ID
		expectedFmt string
		useNode     bool
	}{
		{
			name:        "without node",
			host:        pubsub.HostID("functions"),
			id:          registry.ID{NS: "process", Name: "worker"},
			expectedFmt: "{functions|process:worker|0x00001}",
			useNode:     false,
		},
		{
			name:        "with node",
			node:        pubsub.NodeID("node1"),
			host:        pubsub.HostID("functions"),
			id:          registry.ID{NS: "process", Name: "worker"},
			expectedFmt: "{node1@functions|process:worker|0x00002}",
			useNode:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pid pubsub.PID
			if tt.useNode {
				pid = pidGen.GenerateWithNode(tt.node, tt.host, tt.id)
			} else {
				pid = pidGen.Generate(tt.host, tt.id)
			}

			pidStr := pid.String()
			if pidStr != tt.expectedFmt {
				t.Errorf("Expected PID string %q, got %q", tt.expectedFmt, pidStr)
			}
		})
	}
}
