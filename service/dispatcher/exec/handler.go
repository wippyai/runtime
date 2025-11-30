// Package exec provides exec command handlers for the dispatcher system.
package exec

import (
	"context"
	"errors"
	osexec "os/exec"

	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/dispatcher/exec"
)

// ProcessWaitHandler handles process wait commands.
type ProcessWaitHandler struct{}

func NewProcessWaitHandler() *ProcessWaitHandler {
	return &ProcessWaitHandler{}
}

func (h *ProcessWaitHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	waitCmd := cmd.(*execapi.ProcessWaitCmd)

	err := waitCmd.Process.Wait()

	var exitCode int
	if err == nil {
		exitCode = 0
	} else {
		var exitErr *osexec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			err = nil
		}
	}

	emit(execapi.ProcessWaitResponse{ExitCode: exitCode, Error: err})
	return nil
}

// Service bundles all exec handlers.
type Service struct {
	Wait *ProcessWaitHandler
}

// NewService creates a new exec service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Wait: NewProcessWaitHandler(),
	}
}

// RegisterAll registers all exec handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(execapi.CmdProcessWait, s.Wait)
}
