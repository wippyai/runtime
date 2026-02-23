// SPDX-License-Identifier: MPL-2.0

package stdio

import (
	"context"
	"io"

	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// StdinNamespace exposes WASI preview2 CLI stdin API.
	StdinNamespace = "wasi:cli/stdin@0.2.3"
	// StdoutNamespace exposes WASI preview2 CLI stdout API.
	StdoutNamespace = "wasi:cli/stdout@0.2.3"
	// StderrNamespace exposes WASI preview2 CLI stderr API.
	StderrNamespace = "wasi:cli/stderr@0.2.3"

	// TerminalStdinNamespace exposes terminal stdin presence API.
	TerminalStdinNamespace = "wasi:cli/terminal-stdin@0.2.3"
	// TerminalStdoutNamespace exposes terminal stdout presence API.
	TerminalStdoutNamespace = "wasi:cli/terminal-stdout@0.2.3"
	// TerminalStderrNamespace exposes terminal stderr presence API.
	TerminalStderrNamespace = "wasi:cli/terminal-stderr@0.2.3"
)

const (
	terminalStdinHandle  uint32 = 0x7FFE0001
	terminalStdoutHandle uint32 = 0x7FFE0002
	terminalStderrHandle uint32 = 0x7FFE0003
)

type nonClosingReader struct {
	r io.Reader
}

func (r nonClosingReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

// terminalOutputStreamResource writes directly to terminal output streams from frame context.
type terminalOutputStreamResource struct {
	writer io.Writer
	closed bool
}

func (s *terminalOutputStreamResource) Type() preview2.ResourceType {
	return preview2.ResourceOutputStream
}

func (s *terminalOutputStreamResource) Drop() {
	s.closed = true
}

func (s *terminalOutputStreamResource) Write(data []byte) error {
	if s.closed {
		return &preview2.StreamError{Closed: true}
	}
	if s.writer == nil {
		return &preview2.StreamError{LastOpFailed: true}
	}
	if _, err := s.writer.Write(data); err != nil {
		return &preview2.StreamError{LastOpFailed: true}
	}
	return nil
}

func (s *terminalOutputStreamResource) CheckWrite() (uint64, error) {
	if s.closed {
		return 0, &preview2.StreamError{Closed: true}
	}
	if s.writer == nil {
		return 0, &preview2.StreamError{LastOpFailed: true}
	}
	return preview2.DefaultBufferSize, nil
}

// Host provides stdin handle for WASI CLI.
type Host struct {
	resources *preview2.ResourceTable
}

// NewHost creates a stdio host.
func NewHost(resources *preview2.ResourceTable) *Host {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &Host{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *Host) Namespace() string {
	return StdinNamespace
}

// GetStdin returns an input stream handle backed by terminal stdin context, if available.
func (h *Host) GetStdin(ctx context.Context) uint32 {
	var src any
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil && tc.Stdin != nil {
		// Wrap to avoid closing process-wide stdin on resource drop.
		src = nonClosingReader{r: tc.Stdin}
	}
	return h.resources.Add(preview2.NewInputStreamResource(src))
}

// StdoutHost provides stdout handle for WASI CLI.
type StdoutHost struct {
	resources *preview2.ResourceTable
}

// NewStdoutHost creates a stdout host.
func NewStdoutHost(resources *preview2.ResourceTable) *StdoutHost {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &StdoutHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *StdoutHost) Namespace() string {
	return StdoutNamespace
}

// GetStdout returns an output stream handle backed by terminal stdout context, if available.
func (h *StdoutHost) GetStdout(ctx context.Context) uint32 {
	var out io.Writer
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil {
		out = tc.Stdout
	}
	return h.resources.Add(&terminalOutputStreamResource{writer: out})
}

// StderrHost provides stderr handle for WASI CLI.
type StderrHost struct {
	resources *preview2.ResourceTable
}

// NewStderrHost creates a stderr host.
func NewStderrHost(resources *preview2.ResourceTable) *StderrHost {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &StderrHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *StderrHost) Namespace() string {
	return StderrNamespace
}

// GetStderr returns an output stream handle backed by terminal stderr context, if available.
func (h *StderrHost) GetStderr(ctx context.Context) uint32 {
	var out io.Writer
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil {
		out = tc.Stderr
	}
	return h.resources.Add(&terminalOutputStreamResource{writer: out})
}

// TerminalStdinHost exposes presence of terminal stdin in current frame context.
type TerminalStdinHost struct{}

// NewTerminalStdinHost creates a terminal stdin host.
func NewTerminalStdinHost() *TerminalStdinHost {
	return &TerminalStdinHost{}
}

// Namespace implements wasm-runtime Host.
func (h *TerminalStdinHost) Namespace() string {
	return TerminalStdinNamespace
}

// GetTerminalStdin returns a terminal handle when terminal stdin exists in current context.
func (h *TerminalStdinHost) GetTerminalStdin(ctx context.Context) *uint32 {
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil && tc.Stdin != nil {
		handle := terminalStdinHandle
		return &handle
	}
	return nil
}

// TerminalStdoutHost exposes presence of terminal stdout in current frame context.
type TerminalStdoutHost struct{}

// NewTerminalStdoutHost creates a terminal stdout host.
func NewTerminalStdoutHost() *TerminalStdoutHost {
	return &TerminalStdoutHost{}
}

// Namespace implements wasm-runtime Host.
func (h *TerminalStdoutHost) Namespace() string {
	return TerminalStdoutNamespace
}

// GetTerminalStdout returns a terminal handle when terminal stdout exists in current context.
func (h *TerminalStdoutHost) GetTerminalStdout(ctx context.Context) *uint32 {
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil && tc.Stdout != nil {
		handle := terminalStdoutHandle
		return &handle
	}
	return nil
}

// TerminalStderrHost exposes presence of terminal stderr in current frame context.
type TerminalStderrHost struct{}

// NewTerminalStderrHost creates a terminal stderr host.
func NewTerminalStderrHost() *TerminalStderrHost {
	return &TerminalStderrHost{}
}

// Namespace implements wasm-runtime Host.
func (h *TerminalStderrHost) Namespace() string {
	return TerminalStderrNamespace
}

// GetTerminalStderr returns a terminal handle when terminal stderr exists in current context.
func (h *TerminalStderrHost) GetTerminalStderr(ctx context.Context) *uint32 {
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil && tc.Stderr != nil {
		handle := terminalStderrHandle
		return &handle
	}
	return nil
}
