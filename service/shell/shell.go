package shell

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/shell"
)

type Shell struct {
	id  registry.ID
	cfg *shell.HostConfig
}

func NewShell(id registry.ID, cfg *shell.HostConfig) *Shell {
	return &Shell{
		id:  id,
		cfg: cfg,
	}
}

func (s *Shell) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)
	status <- "started"

	return status, nil
}

func (s *Shell) Stop(ctx context.Context) error {
	return nil
}

func (s *Shell) Send(ctx context.Context, pid process.PID, msg payload.Payloads) error {
	return nil
}

func (s *Shell) Terminate(ctx context.Context, pid process.PID) error {
	return nil
}

func (s *Shell) updateConfig(cfg *shell.HostConfig) {
	s.cfg = cfg
}
