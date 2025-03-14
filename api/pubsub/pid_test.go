package pubsub

import (
	"encoding/json"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
)

func TestPIDJSONMarshaling(t *testing.T) {
	// Create test cases
	testCases := []struct {
		name   string
		pid    PID
		expect string
	}{
		{
			name: "with node",
			pid: PID{
				Node:   "node1",
				Host:   "host1",
				ID:     registry.ParseID("namespace:name"),
				UniqID: "proc1",
			},
			expect: `"{node1@host1|namespace:name|proc1}"`,
		},
		{
			name: "without node",
			pid: PID{
				Host:   "host1",
				ID:     registry.ParseID("namespace:name"),
				UniqID: "proc1",
			},
			expect: `"{host1|namespace:name|proc1}"`,
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tc.pid)
			if err != nil {
				t.Fatalf("failed to marshal PID: %v", err)
			}
			if string(data) != tc.expect {
				t.Errorf("marshaled PID doesn't match expectation\nexpected: %s\ngot: %s", tc.expect, string(data))
			}

			// Test unmarshaling
			var pid PID
			if err := json.Unmarshal(data, &pid); err != nil {
				t.Fatalf("failed to unmarshal PID: %v", err)
			}

			// Compare unmarshaled PID with original
			if pid.Node != tc.pid.Node {
				t.Errorf("Node mismatch: expected %s, got %s", tc.pid.Node, pid.Node)
			}
			if pid.Host != tc.pid.Host {
				t.Errorf("Host mismatch: expected %s, got %s", tc.pid.Host, pid.Host)
			}
			if pid.ID.String() != tc.pid.ID.String() {
				t.Errorf("ID mismatch: expected %s, got %s", tc.pid.ID.String(), pid.ID.String())
			}
			if pid.UniqID != tc.pid.UniqID {
				t.Errorf("UniqID mismatch: expected %s, got %s", tc.pid.UniqID, pid.UniqID)
			}
		})
	}

	// Test with a struct containing PID
	type Container struct {
		ThePID PID `json:"pid"`
		Value  int `json:"value"`
	}

	original := Container{
		ThePID: PID{
			Node:   "node1",
			Host:   "host1",
			ID:     registry.ParseID("namespace:name"),
			UniqID: "proc1",
		},
		Value: 42,
	}

	// Marshal and unmarshal in a struct context
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal container: %v", err)
	}

	var decoded Container
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal container: %v", err)
	}

	// Verify everything matches
	if original.ThePID.String() != decoded.ThePID.String() {
		t.Errorf("PID mismatch in container: expected %s, got %s",
			original.ThePID.String(), decoded.ThePID.String())
	}
	if original.Value != decoded.Value {
		t.Errorf("Value mismatch in container: expected %d, got %d",
			original.Value, decoded.Value)
	}
}
