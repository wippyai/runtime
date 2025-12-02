package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/process"
)

const ProcessManagerName = "system.process_manager"

func ProcessManager() boot.Component {
	return boot.New(boot.P{
		Name: ProcessManagerName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available")
			}

			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, fmt.Errorf("relay node not available")
			}

			manager := process.NewManager(node, logger)
			api.WithManager(ctx, manager)

			logger.Info("process manager started")
			return ctx, nil
		},
	})
}
