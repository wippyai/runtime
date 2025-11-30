// Package excel provides excel command handlers for the dispatcher system.
package excel

import (
	"context"
	"io"

	"github.com/wippyai/runtime/api/dispatcher"
	excelapi "github.com/wippyai/runtime/api/dispatcher/excel"
	streamhandler "github.com/wippyai/runtime/service/dispatcher/stream"
	"github.com/xuri/excelize/v2"
)

// streamRegistryReader wraps StreamRegistry to implement io.Reader.
type streamRegistryReader struct {
	registry *streamhandler.StreamRegistry
	streamID uint64
	eof      bool
}

func (r *streamRegistryReader) Read(p []byte) (n int, err error) {
	if r.eof {
		return 0, io.EOF
	}

	data, err := r.registry.Read(r.streamID, int64(len(p)))
	if err == io.EOF {
		r.eof = true
		if len(data) > 0 {
			copy(p, data)
			return len(data), nil
		}
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}

	copy(p, data)
	return len(data), nil
}

// ExcelOpenStreamHandler opens an Excel file from a stream.
type ExcelOpenStreamHandler struct{}

func NewExcelOpenStreamHandler() *ExcelOpenStreamHandler {
	return &ExcelOpenStreamHandler{}
}

func (h *ExcelOpenStreamHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	openCmd := cmd.(*excelapi.ExcelOpenStreamCmd)

	registry := streamhandler.GetStreamRegistry(ctx)
	if registry == nil {
		emit(excelapi.ExcelOpenStreamResponse{Error: streamhandler.ErrStreamNotFound})
		return nil
	}

	reader := &streamRegistryReader{
		registry: registry,
		streamID: openCmd.StreamID,
	}

	file, err := excelize.OpenReader(reader)
	if err != nil {
		emit(excelapi.ExcelOpenStreamResponse{Error: err})
		return nil
	}

	emit(excelapi.ExcelOpenStreamResponse{File: file})
	return nil
}

// ExcelWriteStreamHandler writes an Excel file to a stream.
type ExcelWriteStreamHandler struct{}

func NewExcelWriteStreamHandler() *ExcelWriteStreamHandler {
	return &ExcelWriteStreamHandler{}
}

func (h *ExcelWriteStreamHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	writeCmd := cmd.(*excelapi.ExcelWriteStreamCmd)

	registry := streamhandler.GetStreamRegistry(ctx)
	if registry == nil {
		emit(excelapi.ExcelWriteStreamResponse{Error: streamhandler.ErrStreamNotFound})
		return nil
	}

	writer := &streamRegistryWriter{
		registry: registry,
		streamID: writeCmd.StreamID,
	}

	err := writeCmd.File.Write(writer)
	emit(excelapi.ExcelWriteStreamResponse{Error: err})
	return nil
}

// streamRegistryWriter wraps StreamRegistry to implement io.Writer.
type streamRegistryWriter struct {
	registry *streamhandler.StreamRegistry
	streamID uint64
}

func (w *streamRegistryWriter) Write(p []byte) (n int, err error) {
	return w.registry.Write(w.streamID, p)
}

// Service bundles all excel handlers.
type Service struct {
	OpenStream  *ExcelOpenStreamHandler
	WriteStream *ExcelWriteStreamHandler
}

// NewService creates a new excel service with all handlers initialized.
func NewService() *Service {
	return &Service{
		OpenStream:  NewExcelOpenStreamHandler(),
		WriteStream: NewExcelWriteStreamHandler(),
	}
}

// RegisterAll registers all excel handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(excelapi.CmdExcelOpenStream, s.OpenStream)
	register(excelapi.CmdExcelWriteStream, s.WriteStream)
}
