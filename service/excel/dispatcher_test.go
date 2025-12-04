package excel

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamhandler "github.com/wippyai/runtime/service/fs/stream"
	"github.com/xuri/excelize/v2"
)

type testCompleter struct {
	fn func(data any)
}

func (e *testCompleter) Complete(data any, _ error) {
	e.fn(data)
}

func newTestEmitter(fn func(data any)) dispatcher.Completer {
	return &testCompleter{fn: fn}
}

func setupTestContext() (context.Context, *resource.Store) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, store
}

func TestExcelOpenStreamHandler(t *testing.T) {
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "Hello")
	f.SetCellValue("Sheet1", "B1", 123)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("failed to write excel: %v", err)
	}
	f.Close()

	ctx, store := setupTestContext()
	defer store.Close()
	table := resource.GetTable(ctx)

	streamID := streamhandler.Insert(table, io.NopCloser(bytes.NewReader(buf.Bytes())))

	d := NewDispatcher(4)
	d.Start(ctx)
	defer d.Stop(ctx)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var resp excelapi.ExcelOpenStreamResponse
	done := make(chan struct{})

	err := handlers[excelapi.CmdExcelOpenStream].Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: streamID,
	}, newTestEmitter(func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
		close(done)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}
	if resp.File == nil {
		t.Fatal("expected file, got nil")
	}

	val, err := resp.File.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("failed to get cell: %v", err)
	}
	if val != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", val)
	}

	resp.File.Close()
}

func TestExcelOpenStreamHandlerNoTable(t *testing.T) {
	ctx := context.Background()

	d := NewDispatcher(4)
	d.Start(ctx)
	defer d.Stop(ctx)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var resp excelapi.ExcelOpenStreamResponse
	done := make(chan struct{})

	err := handlers[excelapi.CmdExcelOpenStream].Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: 1,
	}, newTestEmitter(func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
		close(done)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	if resp.Error == nil {
		t.Error("expected error for missing table")
	}
}

func TestExcelOpenStreamHandlerInvalidStream(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()
	table := resource.GetTable(ctx)

	invalidData := []byte("not an excel file")
	streamID := streamhandler.Insert(table, io.NopCloser(bytes.NewReader(invalidData)))

	d := NewDispatcher(4)
	d.Start(ctx)
	defer d.Stop(ctx)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var resp excelapi.ExcelOpenStreamResponse
	done := make(chan struct{})

	err := handlers[excelapi.CmdExcelOpenStream].Handle(ctx, &excelapi.ExcelOpenStreamCmd{
		StreamID: streamID,
	}, newTestEmitter(func(data any) {
		resp = data.(excelapi.ExcelOpenStreamResponse)
		close(done)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	if resp.Error == nil {
		t.Error("expected error for invalid excel data")
	}
}

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
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "Test")
	f.SetCellValue("Sheet1", "B1", 456)

	ctx, store := setupTestContext()
	defer store.Close()
	table := resource.GetTable(ctx)

	buf := &bytes.Buffer{}
	writeCloser := &mockWriteCloser{buf: buf}
	streamID := streamhandler.Insert(table, writeCloser)

	d := NewDispatcher(4)
	d.Start(ctx)
	defer d.Stop(ctx)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var resp excelapi.ExcelWriteStreamResponse
	done := make(chan struct{})

	err := handlers[excelapi.CmdExcelWriteStream].Handle(ctx, &excelapi.ExcelWriteStreamCmd{
		File:     f,
		StreamID: streamID,
	}, newTestEmitter(func(data any) {
		resp = data.(excelapi.ExcelWriteStreamResponse)
		close(done)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}

	if buf.Len() == 0 {
		t.Error("expected data to be written")
	}

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

func TestExcelWriteStreamHandlerNoTable(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	ctx := context.Background()

	d := NewDispatcher(4)
	d.Start(ctx)
	defer d.Stop(ctx)

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var resp excelapi.ExcelWriteStreamResponse
	done := make(chan struct{})

	err := handlers[excelapi.CmdExcelWriteStream].Handle(ctx, &excelapi.ExcelWriteStreamCmd{
		File:     f,
		StreamID: 1,
	}, newTestEmitter(func(data any) {
		resp = data.(excelapi.ExcelWriteStreamResponse)
		close(done)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	if resp.Error == nil {
		t.Error("expected error for missing table")
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher(4)

	count := 0
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		count++
	})

	if count != 2 {
		t.Errorf("expected 2 handlers registered, got %d", count)
	}
}
