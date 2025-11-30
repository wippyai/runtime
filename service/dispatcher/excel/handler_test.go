package excel

import (
	"bytes"
	"context"
	"io"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	streamhandler "github.com/wippyai/runtime/service/dispatcher/stream"
	"github.com/xuri/excelize/v2"
)

func TestExcelOpenStreamHandler(t *testing.T) {
	// Create a simple Excel file in memory
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "Hello")
	f.SetCellValue("Sheet1", "B1", 123)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("failed to write excel: %v", err)
	}
	f.Close()

	// Setup context with stream registry
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	registry := streamhandler.GetOrCreateStreamRegistry(ctx)

	// Register the buffer as a stream
	streamID := registry.RegisterStream(io.NopCloser(bytes.NewReader(buf.Bytes())))

	// Test the handler
	h := NewExcelOpenStreamHandler()
	var resp excelapi.ExcelOpenStreamResponse

	err := h.Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: streamID,
	}, func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}
	if resp.File == nil {
		t.Fatal("expected file, got nil")
	}

	// Verify we can read the data
	val, err := resp.File.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("failed to get cell: %v", err)
	}
	if val != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", val)
	}

	resp.File.Close()
}

func TestExcelOpenStreamHandlerNoRegistry(t *testing.T) {
	ctx := context.Background()

	h := NewExcelOpenStreamHandler()
	var resp excelapi.ExcelOpenStreamResponse

	err := h.Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: 1,
	}, func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error for missing registry")
	}
}

func TestExcelOpenStreamHandlerInvalidStream(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	streamhandler.GetOrCreateStreamRegistry(ctx)

	// Register invalid data as a stream
	invalidData := []byte("not an excel file")
	registry := streamhandler.GetStreamRegistry(ctx)
	streamID := registry.RegisterStream(io.NopCloser(bytes.NewReader(invalidData)))

	h := NewExcelOpenStreamHandler()
	var resp excelapi.ExcelOpenStreamResponse

	err := h.Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: streamID,
	}, func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error for invalid excel data")
	}
}

// mockWriteCloser wraps a buffer to implement io.WriteCloser
type mockWriteCloser struct {
	buf    *bytes.Buffer
	closed bool
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.closed = true
	return nil
}

func TestExcelWriteStreamHandler(t *testing.T) {
	// Create a simple Excel file
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "Test")
	f.SetCellValue("Sheet1", "B1", 456)

	// Setup context with stream registry
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	registry := streamhandler.GetOrCreateStreamRegistry(ctx)

	// Register a write buffer as a stream
	buf := &bytes.Buffer{}
	writeCloser := &mockWriteCloser{buf: buf}
	streamID := registry.RegisterStream(writeCloser)

	// Test the handler
	h := NewExcelWriteStreamHandler()
	var resp excelapi.ExcelWriteStreamResponse

	err := h.Handle(ctx, &excelapi.ExcelWriteStreamCmd{
		File:     f,
		StreamID: streamID,
	}, func(data any) {
		resp = data.(excelapi.ExcelWriteStreamResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}

	// Verify data was written
	if buf.Len() == 0 {
		t.Error("expected data to be written")
	}

	// Verify we can read it back
	readFile, err := excelize.OpenReader(buf)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}

	val, err := readFile.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("failed to get cell: %v", err)
	}
	if val != "Test" {
		t.Errorf("expected 'Test', got '%s'", val)
	}

	readFile.Close()
	f.Close()
}

func TestExcelWriteStreamHandlerNoRegistry(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	ctx := context.Background()

	h := NewExcelWriteStreamHandler()
	var resp excelapi.ExcelWriteStreamResponse

	err := h.Handle(ctx, &excelapi.ExcelWriteStreamCmd{
		File:     f,
		StreamID: 1,
	}, func(data any) {
		resp = data.(excelapi.ExcelWriteStreamResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error for missing registry")
	}
}

func TestExcelService(t *testing.T) {
	svc := NewService()
	if svc.OpenStream == nil {
		t.Error("OpenStream handler not initialized")
	}
	if svc.WriteStream == nil {
		t.Error("WriteStream handler not initialized")
	}
}
