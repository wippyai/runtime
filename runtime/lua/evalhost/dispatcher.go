package evalhost

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/eval"
)

// CompileHandler handles CmdCompile commands.
type CompileHandler struct {
	host *Host
}

// NewCompileHandler creates a compile handler.
func NewCompileHandler(host *Host) *CompileHandler {
	return &CompileHandler{host: host}
}

// Handle implements dispatcher.Handler.
func (h *CompileHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	compileCmd := cmd.(eval.CompileCmd)

	program, err := h.host.Compile(ctx, compileCmd)
	if err != nil {
		emit(nil)
		return err
	}

	emit(program)
	return nil
}

// RunHandler handles CmdRun commands.
// This handler compiles and executes Lua code inline.
type RunHandler struct {
	host *Host
}

// NewRunHandler creates a run handler.
func NewRunHandler(host *Host) *RunHandler {
	return &RunHandler{host: host}
}

// Handle implements dispatcher.Handler.
// TODO: Implement inline execution with scheduler
func (h *RunHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	// RunCmd execution is complex - needs scheduler integration
	// For now, return error
	emit(nil)
	return nil
}

// Service bundles all eval handlers.
type Service struct {
	host    *Host
	Compile *CompileHandler
	Run     *RunHandler
}

// NewService creates a new eval service.
func NewService(host *Host) *Service {
	return &Service{
		host:    host,
		Compile: NewCompileHandler(host),
		Run:     NewRunHandler(host),
	}
}

// RegisterAll registers all eval handlers.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(eval.CmdCompile, s.Compile)
	register(eval.CmdRun, s.Run)
}
