package stream

import (
	"io"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/runtime/resource"
	streamsvc "github.com/wippyai/runtime/service/fs/stream"
	lua "github.com/yuin/gopher-lua"
)

func TestStreamTableIntegration(t *testing.T) {
	table := resource.NewTable()

	data := "hello world stream data"
	reader := io.NopCloser(strings.NewReader(data))
	id := streamsvc.Insert(table, reader)
	if id == 0 {
		t.Fatal("expected non-zero stream ID")
	}

	chunk1, err := streamsvc.Read(table, id, 5)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk1) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk1))
	}

	chunk2, err := streamsvc.Read(table, id, 6)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk2) != " world" {
		t.Errorf("expected ' world', got '%s'", string(chunk2))
	}

	err = streamsvc.Close(table, id)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	_, err = streamsvc.Read(table, id, 10)
	if err != streamsvc.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestYieldToCommand(t *testing.T) {
	readYield := AcquireReadYield(42, 1024)
	readCmd := readYield.ToCommand()
	if readCmd == nil {
		t.Error("expected non-nil command for ReadYield")
	}
	if readCmd.CmdID() != 50 {
		t.Errorf("expected CmdID=50, got %v", readCmd.CmdID())
	}
	ReleaseReadYield(readYield)

	closeYield := AcquireCloseYield(99)
	closeCmd := closeYield.ToCommand()
	if closeCmd == nil {
		t.Error("expected non-nil command for CloseYield")
	}
	if closeCmd.CmdID() != 51 {
		t.Errorf("expected CmdID=51, got %v", closeCmd.CmdID())
	}
	ReleaseCloseYield(closeYield)
}

func BenchmarkReadYieldPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := AcquireReadYield(42, 1024)
		ReleaseReadYield(y)
	}
}

func BenchmarkCloseYieldPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		y := AcquireCloseYield(42)
		ReleaseCloseYield(y)
	}
}

func TestReadYieldPool(t *testing.T) {
	y1 := AcquireReadYield(42, 1024)
	if y1.StreamID != 42 {
		t.Errorf("expected StreamID=42, got %v", y1.StreamID)
	}
	if y1.Size != 1024 {
		t.Errorf("expected Size=1024, got %v", y1.Size)
	}
	ReleaseReadYield(y1)

	y2 := AcquireReadYield(99, 2048)
	if y2.StreamID != 99 {
		t.Errorf("expected StreamID=99, got %v", y2.StreamID)
	}
	ReleaseReadYield(y2)
}

func TestReadYieldString(t *testing.T) {
	y := AcquireReadYield(123, 4096)
	defer ReleaseReadYield(y)

	if y.String() != "<stream_read_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestCloseYieldPool(t *testing.T) {
	y1 := AcquireCloseYield(10)
	if y1.StreamID != 10 {
		t.Errorf("expected StreamID=10, got %v", y1.StreamID)
	}
	ReleaseCloseYield(y1)

	y2 := AcquireCloseYield(20)
	if y2.StreamID != 20 {
		t.Errorf("expected StreamID=20, got %v", y2.StreamID)
	}
	ReleaseCloseYield(y2)
}

func TestCloseYieldString(t *testing.T) {
	y := AcquireCloseYield(456)
	defer ReleaseCloseYield(y)

	if y.String() != "<stream_close_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestNewStream(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	stream := NewStream(l, 42)
	if stream == lua.LNil {
		t.Fatal("NewStream returned nil")
	}

	ud, ok := stream.(*lua.LUserData)
	if !ok {
		t.Fatal("expected LUserData")
	}

	s, ok := ud.Value.(*Stream)
	if !ok {
		t.Fatal("expected *Stream")
	}
	if s.ID != 42 {
		t.Errorf("expected ID=42, got %v", s.ID)
	}
}

func TestStreamMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	stream := NewStream(l, 42)
	l.SetGlobal("test_stream", stream)

	err := l.DoString(`
		if test_stream == nil then
			error("stream is nil")
		end
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}
}
