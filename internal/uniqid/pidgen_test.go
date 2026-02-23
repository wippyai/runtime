// SPDX-License-Identifier: MPL-2.0

package uniqid_test

import (
	"testing"

	"github.com/wippyai/runtime/api/pid"
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

	host := "functions"

	p := pidGen.Generate(host)

	if p.Host != host {
		t.Errorf("Expected host %q, got %q", host, p.Host)
	}
	if p.UniqID == "" {
		t.Error("Expected UniqID to be generated, got empty string")
	}
	if p.UniqID != "0x00001" {
		t.Errorf("Expected first UniqID to be '0x00001', got %q", p.UniqID)
	}
	if p.Node != "" {
		t.Errorf("Expected empty node, got %q", p.Node)
	}
}

func TestPIDGenerator_GenerateWithConfiguredNode(t *testing.T) {
	gen := uniqid.NewGenerator()

	node := "node1"
	host := "functions"

	pidGen := uniqid.NewPIDGenerator(gen, node)
	p := pidGen.Generate(host)

	if p.Node != node {
		t.Errorf("Expected node %q, got %q", node, p.Node)
	}
	if p.Host != host {
		t.Errorf("Expected host %q, got %q", host, p.Host)
	}
	if p.UniqID == "" {
		t.Error("Expected UniqID to be generated, got empty string")
	}
	if p.UniqID != "0x00001" {
		t.Errorf("Expected first UniqID to be '0x00001', got %q", p.UniqID)
	}
}

func TestPIDGenerator_SequentialUniqID(t *testing.T) {
	gen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(gen, "")

	host := "functions"

	expected := []string{"0x00001", "0x00002", "0x00003", "0x00004", "0x00005"}

	for i, expectedUniqID := range expected {
		p := pidGen.Generate(host)
		if p.UniqID != expectedUniqID {
			t.Errorf("Iteration %d: expected UniqID %q, got %q", i, expectedUniqID, p.UniqID)
		}
	}
}

func TestPIDGenerator_StringFormat(t *testing.T) {
	tests := []struct {
		name        string
		node        string
		host        string
		expectedFmt string
		useNode     bool
	}{
		{
			name:        "without node",
			host:        "functions",
			expectedFmt: "{functions|0x00001}",
			useNode:     false,
		},
		{
			name:        "with node",
			node:        "node1",
			host:        "functions",
			expectedFmt: "{node1@functions|0x00001}",
			useNode:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := uniqid.NewGenerator()
			var p pid.PID
			if tt.useNode {
				pidGenWithNode := uniqid.NewPIDGenerator(gen, tt.node)
				p = pidGenWithNode.Generate(tt.host)
			} else {
				pidGen := uniqid.NewPIDGenerator(gen, "")
				p = pidGen.Generate(tt.host)
			}

			pidStr := p.String()
			if pidStr != tt.expectedFmt {
				t.Errorf("Expected PID string %q, got %q", tt.expectedFmt, pidStr)
			}
		})
	}
}
