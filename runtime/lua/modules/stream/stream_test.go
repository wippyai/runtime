package stream

import (
	"context"
	"io"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/resource"
	streamsvc "github.com/wippyai/runtime/service/fs/stream"
	lua "github.com/yuin/gopher-lua"
)

func TestStreamRegistryIntegration(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)

	registry := streamsvc.GetOrCreateStreamRegistry(ctx)
	if registry == nil {
		t.Fatal("expected registry")
	}

	data := "hello world stream data"
	reader := io.NopCloser(strings.NewReader(data))
	id := registry.Register(reader)
	if id == 0 {
		t.Fatal("expected non-zero stream ID")
	}

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

	err = registry.Close(id)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	_, err = registry.Read(id, 10)
	if err != streamsvc.ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestStreamYieldToCommand(t *testing.T) {
	readYield := AcquireStreamReadYield(42, 1024)
	readCmd := readYield.ToCommand()
	if readCmd == nil {
		t.Error("expected non-nil command for StreamReadYield")
	}
	if readCmd.CmdID() != 50 {
		t.Errorf("expected CmdID=50, got %v", readCmd.CmdID())
	}
	ReleaseStreamReadYield(readYield)

	closeYield := AcquireStreamCloseYield(99)
	closeCmd := closeYield.ToCommand()
	if closeCmd == nil {
		t.Error("expected non-nil command for StreamCloseYield")
	}
	if closeCmd.CmdID() != 51 {
		t.Errorf("expected CmdID=51, got %v", closeCmd.CmdID())
	}
	ReleaseStreamCloseYield(closeYield)
}

func BenchmarkStreamReadYieldPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := AcquireStreamReadYield(42, 1024)
		ReleaseStreamReadYield(y)
	}
}

func BenchmarkStreamCloseYieldPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := AcquireStreamCloseYield(42)
		ReleaseStreamCloseYield(y)
	}
}

func TestStreamReadYieldPool(t *testing.T) {
	y1 := AcquireStreamReadYield(42, 1024)
	if y1.StreamID != 42 {
		t.Errorf("expected StreamID=42, got %v", y1.StreamID)
	}
	if y1.Size != 1024 {
		t.Errorf("expected Size=1024, got %v", y1.Size)
	}
	ReleaseStreamReadYield(y1)

	y2 := AcquireStreamReadYield(99, 2048)
	if y2.StreamID != 99 {
		t.Errorf("expected StreamID=99, got %v", y2.StreamID)
	}
	ReleaseStreamReadYield(y2)
}

func TestStreamReadYieldString(t *testing.T) {
	y := AcquireStreamReadYield(123, 4096)
	defer ReleaseStreamReadYield(y)

	if y.String() != "<stream_read_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestStreamCloseYieldPool(t *testing.T) {
	y1 := AcquireStreamCloseYield(10)
	if y1.StreamID != 10 {
		t.Errorf("expected StreamID=10, got %v", y1.StreamID)
	}
	ReleaseStreamCloseYield(y1)

	y2 := AcquireStreamCloseYield(20)
	if y2.StreamID != 20 {
		t.Errorf("expected StreamID=20, got %v", y2.StreamID)
	}
	ReleaseStreamCloseYield(y2)
}

func TestStreamCloseYieldString(t *testing.T) {
	y := AcquireStreamCloseYield(456)
	defer ReleaseStreamCloseYield(y)

	if y.String() != "<stream_close_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestBindStream(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindStream(l)

	fn := l.GetGlobal("__stream_new")
	if fn == lua.LNil {
		t.Fatal("__stream_new not registered")
	}
}

func TestStreamMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindStream(l)

	err := l.DoString(`
		local stream = __stream_new(42)
		if stream == nil then
			error("stream is nil")
		end
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}
}
