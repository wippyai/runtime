package engine2

import (
	"context"
	"io"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	streamsvc "github.com/wippyai/runtime/service/dispatcher/stream"
)

// TestStreamRegistryIntegration tests stream registry operations.
func TestStreamRegistryIntegration(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	// Get or create registry
	registry := streamsvc.GetOrCreateStreamRegistry(ctx)
	if registry == nil {
		t.Fatal("expected registry")
	}

	// Register a stream
	data := "hello world stream data"
	reader := io.NopCloser(strings.NewReader(data))
	id := registry.Register(reader)
	if id == 0 {
		t.Fatal("expected non-zero stream ID")
	}

	// Read chunks
	chunk1, err := registry.Read(id, 5)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk1) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk1))
	}

	chunk2, err := registry.Read(id, 6)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk2) != " world" {
		t.Errorf("expected ' world', got '%s'", string(chunk2))
	}

	// Close stream
	err = registry.Close(id)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	// Should be gone
	_, err = registry.Read(id, 10)
	if err != streamsvc.ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

// TestConvertYieldToCommandStream tests stream yield conversion.
func TestConvertYieldToCommandStream(t *testing.T) {
	// Test StreamReadYield
	readYield := acquireStreamReadYield(42, 1024)
	readCmd := ConvertYieldToCommand(readYield)
	if readCmd == nil {
		t.Error("expected non-nil command for StreamReadYield")
	}
	if readCmd.CmdID() != 50 {
		t.Errorf("expected CmdID=50, got %v", readCmd.CmdID())
	}
	ReleaseStreamReadYield(readYield)

	// Test StreamCloseYield
	closeYield := acquireStreamCloseYield(99)
	closeCmd := ConvertYieldToCommand(closeYield)
	if closeCmd == nil {
		t.Error("expected non-nil command for StreamCloseYield")
	}
	if closeCmd.CmdID() != 51 {
		t.Errorf("expected CmdID=51, got %v", closeCmd.CmdID())
	}
	ReleaseStreamCloseYield(closeYield)
}

// BenchmarkStreamYieldAcquireRelease measures allocation for stream yields.
func BenchmarkStreamYieldAcquireRelease(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := acquireStreamReadYield(42, 1024)
		ReleaseStreamReadYield(y)
	}
}

// BenchmarkStreamCloseYieldAcquireRelease measures allocation for close yields.
func BenchmarkStreamCloseYieldAcquireRelease(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := acquireStreamCloseYield(42)
		ReleaseStreamCloseYield(y)
	}
}
