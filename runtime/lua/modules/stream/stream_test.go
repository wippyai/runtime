package stream

import (
	"errors"
	"io"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
)

func TestStreamTableIntegration(t *testing.T) {
	table := resource.NewTable()

	data := "hello world stream data"
	reader := io.NopCloser(strings.NewReader(data))
	id := streamsys.Insert(table, reader)
	if id == 0 {
		t.Fatal("expected non-zero stream ID")
	}

	chunk1, err := streamsys.Read(table, id, 5)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk1) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk1))
	}

	chunk2, err := streamsys.Read(table, id, 6)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(chunk2) != " world" {
		t.Errorf("expected ' world', got '%s'", string(chunk2))
	}

	err = streamsys.Close(table, id)
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	_, err = streamsys.Read(table, id, 10)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestYieldToCommand(t *testing.T) {
	readYield := AcquireReadYield(42, 1024)
	readCmd := readYield.ToCommand()
	if readCmd == nil {
		t.Fatal("expected non-nil command for ReadYield")
	}
	if readCmd.CmdID() != 50 {
		t.Errorf("expected CmdID=50, got %v", readCmd.CmdID())
	}
	ReleaseReadYield(readYield)

	closeYield := AcquireCloseYield(99)
	closeCmd := closeYield.ToCommand()
	if closeCmd == nil {
		t.Fatal("expected non-nil command for CloseYield")
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

func TestWriteYieldPool(t *testing.T) {
	y1 := AcquireWriteYield(123, []byte("data1"))
	if y1.StreamID != 123 {
		t.Errorf("expected StreamID=123, got %v", y1.StreamID)
	}
	if string(y1.Data) != "data1" {
		t.Errorf("expected Data='data1', got %s", string(y1.Data))
	}
	ReleaseWriteYield(y1)

	y2 := AcquireWriteYield(456, []byte("data2"))
	defer ReleaseWriteYield(y2)
	if y2.StreamID != 456 {
		t.Errorf("expected StreamID=456, got %v", y2.StreamID)
	}
}

func TestWriteYieldString(t *testing.T) {
	y := AcquireWriteYield(1, []byte("test"))
	defer ReleaseWriteYield(y)

	if y.String() != "<stream_write_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestSeekYieldPool(t *testing.T) {
	y1 := AcquireSeekYield(100, 200, 1)
	if y1.StreamID != 100 {
		t.Errorf("expected StreamID=100, got %v", y1.StreamID)
	}
	if y1.Offset != 200 {
		t.Errorf("expected Offset=200, got %v", y1.Offset)
	}
	if y1.Whence != 1 {
		t.Errorf("expected Whence=1, got %v", y1.Whence)
	}
	ReleaseSeekYield(y1)

	y2 := AcquireSeekYield(200, 300, 2)
	defer ReleaseSeekYield(y2)
	if y2.StreamID != 200 {
		t.Errorf("expected StreamID=200, got %v", y2.StreamID)
	}
}

func TestSeekYieldString(t *testing.T) {
	y := AcquireSeekYield(1, 0, 0)
	defer ReleaseSeekYield(y)

	if y.String() != "<stream_seek_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestFlushYieldPool(t *testing.T) {
	y1 := AcquireFlushYield(50)
	if y1.StreamID != 50 {
		t.Errorf("expected StreamID=50, got %v", y1.StreamID)
	}
	ReleaseFlushYield(y1)

	y2 := AcquireFlushYield(60)
	defer ReleaseFlushYield(y2)
	if y2.StreamID != 60 {
		t.Errorf("expected StreamID=60, got %v", y2.StreamID)
	}
}

func TestFlushYieldString(t *testing.T) {
	y := AcquireFlushYield(1)
	defer ReleaseFlushYield(y)

	if y.String() != "<stream_flush_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestStatYieldPool(t *testing.T) {
	y1 := AcquireStatYield(70)
	if y1.StreamID != 70 {
		t.Errorf("expected StreamID=70, got %v", y1.StreamID)
	}
	ReleaseStatYield(y1)

	y2 := AcquireStatYield(80)
	defer ReleaseStatYield(y2)
	if y2.StreamID != 80 {
		t.Errorf("expected StreamID=80, got %v", y2.StreamID)
	}
}

func TestStatYieldString(t *testing.T) {
	y := AcquireStatYield(1)
	defer ReleaseStatYield(y)

	if y.String() != "<stream_stat_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestReadYieldHandleResult(t *testing.T) {
	buf := streamapi.AcquireBuffer(100)
	copy(buf.Data, "buffered data")
	buf.N = 13

	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success with data",
			data:    []byte("test data"),
			err:     nil,
			wantErr: false,
		},
		{
			name:    "success with buffer",
			data:    buf,
			err:     nil,
			wantErr: false,
		},
		{
			name:    "success with nil (EOF)",
			data:    nil,
			err:     nil,
			wantErr: false,
		},
		{
			name:    "read error",
			data:    nil,
			err:     errors.New("read failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireReadYield(1, 100)
			defer ReleaseReadYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestWriteYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    int64(10),
			err:     nil,
			wantErr: false,
		},
		{
			name:    "write error",
			data:    nil,
			err:     errors.New("write failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireWriteYield(1, []byte("test"))
			defer ReleaseWriteYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestSeekYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    int64(100),
			err:     nil,
			wantErr: false,
		},
		{
			name:    "seek error",
			data:    nil,
			err:     errors.New("seek failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireSeekYield(1, 0, 0)
			defer ReleaseSeekYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestFlushYieldHandleResult(t *testing.T) {
	tests := []struct {
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "flush error",
			err:     errors.New("flush failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireFlushYield(1)
			defer ReleaseFlushYield(y)

			result := y.HandleResult(l, nil, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestCloseYieldHandleResult(t *testing.T) {
	tests := []struct {
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "close error",
			err:     errors.New("close failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireCloseYield(1)
			defer ReleaseCloseYield(y)

			result := y.HandleResult(l, nil, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	if table == nil {
		t.Fatal("Build() returned nil table")
	}

	if !table.Immutable {
		t.Error("module table should be immutable")
	}

	if len(yields) != 8 {
		t.Errorf("expected 8 yield types, got %d", len(yields))
	}
}

func TestModuleInfo(t *testing.T) {
	if Module.Name != "stream" {
		t.Errorf("expected name 'stream', got '%s'", Module.Name)
	}
	if Module.Description == "" {
		t.Error("module should have a description")
	}
	if len(Module.Class) == 0 {
		t.Error("module should have at least one class")
	}
}

func TestScannerCreateYieldPool(t *testing.T) {
	y1 := AcquireScannerCreateYield(42, 0)
	if y1.StreamID != 42 {
		t.Errorf("expected StreamID=42, got %v", y1.StreamID)
	}
	if y1.SplitType != 0 {
		t.Errorf("expected SplitType=0, got %v", y1.SplitType)
	}
	ReleaseScannerCreateYield(y1)

	y2 := AcquireScannerCreateYield(99, 1)
	if y2.StreamID != 99 {
		t.Errorf("expected StreamID=99, got %v", y2.StreamID)
	}
	if y2.SplitType != 1 {
		t.Errorf("expected SplitType=1, got %v", y2.SplitType)
	}
	ReleaseScannerCreateYield(y2)
}

func TestScannerCreateYieldString(t *testing.T) {
	y := AcquireScannerCreateYield(1, 0)
	defer ReleaseScannerCreateYield(y)

	if y.String() != "<scanner_create_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestScannerScanYieldPool(t *testing.T) {
	scanner := &Scanner{ID: 100}
	y1 := AcquireScannerScanYield(100, scanner)
	if y1.ScannerID != 100 {
		t.Errorf("expected ScannerID=100, got %v", y1.ScannerID)
	}
	if y1.scanner != scanner {
		t.Error("expected scanner reference to be stored")
	}
	ReleaseScannerScanYield(y1)

	y2 := AcquireScannerScanYield(200, nil)
	if y2.ScannerID != 200 {
		t.Errorf("expected ScannerID=200, got %v", y2.ScannerID)
	}
	ReleaseScannerScanYield(y2)
}

func TestScannerScanYieldString(t *testing.T) {
	y := AcquireScannerScanYield(1, nil)
	defer ReleaseScannerScanYield(y)

	if y.String() != "<scanner_scan_yield>" {
		t.Errorf("unexpected String(): %s", y.String())
	}
}

func TestNewScanner(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	scanner := NewScanner(l, 42)
	if scanner == lua.LNil {
		t.Fatal("NewScanner returned nil")
	}

	ud, ok := scanner.(*lua.LUserData)
	if !ok {
		t.Fatal("expected LUserData")
	}

	s, ok := ud.Value.(*Scanner)
	if !ok {
		t.Fatal("expected *Scanner")
	}
	if s.ID != 42 {
		t.Errorf("expected ID=42, got %v", s.ID)
	}
}

func TestScannerServiceIntegration(t *testing.T) {
	table := resource.NewTable()

	data := "line1\nline2\nline3"
	reader := io.NopCloser(strings.NewReader(data))
	streamID := streamsys.Insert(table, reader)
	if streamID == 0 {
		t.Fatal("expected non-zero stream ID")
	}

	scannerID, err := streamsys.CreateScanner(table, streamID, 0)
	if err != nil {
		t.Fatalf("CreateScanner error: %v", err)
	}
	if scannerID == 0 {
		t.Fatal("expected non-zero scanner ID")
	}

	result1, err := streamsys.ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("ScanNext error: %v", err)
	}
	if !result1.HasToken {
		t.Error("expected HasToken=true for first line")
	}
	if result1.Text != "line1" {
		t.Errorf("expected 'line1', got '%s'", result1.Text)
	}

	result2, err := streamsys.ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("ScanNext error: %v", err)
	}
	if !result2.HasToken {
		t.Error("expected HasToken=true for second line")
	}
	if result2.Text != "line2" {
		t.Errorf("expected 'line2', got '%s'", result2.Text)
	}

	result3, err := streamsys.ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("ScanNext error: %v", err)
	}
	if !result3.HasToken {
		t.Error("expected HasToken=true for third line")
	}
	if result3.Text != "line3" {
		t.Errorf("expected 'line3', got '%s'", result3.Text)
	}

	result4, err := streamsys.ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("ScanNext error: %v", err)
	}
	if result4.HasToken {
		t.Error("expected HasToken=false at EOF")
	}
	if result4.Error != "" {
		t.Errorf("expected no error at EOF, got '%s'", result4.Error)
	}
}

func TestScannerSplitWords(t *testing.T) {
	table := resource.NewTable()

	data := "hello world foo bar"
	reader := io.NopCloser(strings.NewReader(data))
	streamID := streamsys.Insert(table, reader)

	scannerID, err := streamsys.CreateScanner(table, streamID, 1)
	if err != nil {
		t.Fatalf("CreateScanner error: %v", err)
	}

	var words []string
	for {
		result, err := streamsys.ScanNext(table, scannerID)
		if err != nil {
			t.Fatalf("ScanNext error: %v", err)
		}
		if !result.HasToken {
			break
		}
		words = append(words, result.Text)
	}

	expected := []string{"hello", "world", "foo", "bar"}
	if len(words) != len(expected) {
		t.Fatalf("expected %d words, got %d", len(expected), len(words))
	}
	for i, w := range words {
		if w != expected[i] {
			t.Errorf("word[%d]: expected '%s', got '%s'", i, expected[i], w)
		}
	}
}
