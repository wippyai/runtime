package internode

import (
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// mockTranscoder for testing
type mockTranscoder struct{}

func (mt *mockTranscoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	return p, nil // Pass through
}

func (mt *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	return nil
}

func TestMessageCodec_PackagePIDs_SourceTarget(t *testing.T) {
	codec := NewMessageCodec(&mockTranscoder{})

	// Create PIDs with actual values
	sourcePID := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("ns:source"),
		UniqID: "src123",
	}

	targetPID := pubsub.PID{
		Node:   "node2",
		Host:   "host2",
		ID:     registry.ParseID("ns:target"),
		UniqID: "tgt456",
	}

	// Create package with both Source and Target
	originalPkg := &pubsub.Package{
		Source: sourcePID,
		Target: targetPID,
		Messages: []*pubsub.Message{
			{
				Topic: "test.topic",
				Payloads: []payload.Payload{
					payload.NewString("test message"),
				},
			},
		},
	}

	t.Logf("Original Source: %s", originalPkg.Source.String())
	t.Logf("Original Target: %s", originalPkg.Target.String())

	// Encode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	t.Logf("Decoded Source: %s", decoded.Source.String())
	t.Logf("Decoded Target: %s", decoded.Target.String())

	// Verify Source PID
	if decoded.Source.Node != originalPkg.Source.Node {
		t.Errorf("Source Node mismatch. Expected %q, got %q", originalPkg.Source.Node, decoded.Source.Node)
	}
	if decoded.Source.Host != originalPkg.Source.Host {
		t.Errorf("Source Host mismatch. Expected %q, got %q", originalPkg.Source.Host, decoded.Source.Host)
	}
	if decoded.Source.ID.String() != originalPkg.Source.ID.String() {
		t.Errorf("Source ID mismatch. Expected %q, got %q", originalPkg.Source.ID.String(), decoded.Source.ID.String())
	}
	if decoded.Source.UniqID != originalPkg.Source.UniqID {
		t.Errorf("Source UniqID mismatch. Expected %q, got %q", originalPkg.Source.UniqID, decoded.Source.UniqID)
	}

	// Verify Target PID
	if decoded.Target.Node != originalPkg.Target.Node {
		t.Errorf("Target Node mismatch. Expected %q, got %q", originalPkg.Target.Node, decoded.Target.Node)
	}
	if decoded.Target.Host != originalPkg.Target.Host {
		t.Errorf("Target Host mismatch. Expected %q, got %q", originalPkg.Target.Host, decoded.Target.Host)
	}
	if decoded.Target.ID.String() != originalPkg.Target.ID.String() {
		t.Errorf("Target ID mismatch. Expected %q, got %q", originalPkg.Target.ID.String(), decoded.Target.ID.String())
	}
	if decoded.Target.UniqID != originalPkg.Target.UniqID {
		t.Errorf("Target UniqID mismatch. Expected %q, got %q", originalPkg.Target.UniqID, decoded.Target.UniqID)
	}

	// Verify message content
	if len(decoded.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(decoded.Messages))
	}
	if decoded.Messages[0].Topic != "test.topic" {
		t.Errorf("Topic mismatch. Expected 'test.topic', got %q", decoded.Messages[0].Topic)
	}
}

func TestMessageCodec_EmptyPIDs(t *testing.T) {
	codec := NewMessageCodec(&mockTranscoder{})

	// Package with empty PIDs (this is what we're seeing in logs)
	originalPkg := &pubsub.Package{
		Source: pubsub.PID{}, // Empty
		Target: pubsub.PID{}, // Empty
		Messages: []*pubsub.Message{
			{
				Topic: "test.topic",
				Payloads: []payload.Payload{
					payload.NewString("test message"),
				},
			},
		},
	}

	t.Logf("Original Source (empty): %s", originalPkg.Source.String())
	t.Logf("Original Target (empty): %s", originalPkg.Target.String())

	// This should be {|:|} for both
	if originalPkg.Source.String() != "{|:|}" {
		t.Errorf("Expected empty source to be {|:|}, got %s", originalPkg.Source.String())
	}
	if originalPkg.Target.String() != "{|:|}" {
		t.Errorf("Expected empty target to be {|:|}, got %s", originalPkg.Target.String())
	}

	// Encode/decode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	t.Logf("Decoded Source (should be empty): %s", decoded.Source.String())
	t.Logf("Decoded Target (should be empty): %s", decoded.Target.String())

	// Verify they remain empty after round-trip
	if decoded.Source.String() != "{|:|}" {
		t.Errorf("Expected decoded source to be {|:|}, got %s", decoded.Source.String())
	}
	if decoded.Target.String() != "{|:|}" {
		t.Errorf("Expected decoded target to be {|:|}, got %s", decoded.Target.String())
	}
}
