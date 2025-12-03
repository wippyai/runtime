package dispatchers

import "github.com/wippyai/runtime/api/boot"

const (
	// Dispatcher parent dependency
	DispatcherName boot.Name = "dispatcher"

	// System dispatchers
	ClockDispatcherName    boot.Name = "dispatcher.clock"
	FuncDispatcherName     boot.Name = "dispatcher.func"
	SecurityDispatcherName boot.Name = "dispatcher.security"
	QueueDispatcherName    boot.Name = "dispatcher.queue"

	// Service dispatchers
	HTTPDispatcherName   boot.Name = "dispatcher.http"
	WSDispatcherName     boot.Name = "dispatcher.ws"
	ExecDispatcherName   boot.Name = "dispatcher.exec"
	StreamDispatcherName boot.Name = "dispatcher.stream"
	ExcelDispatcherName  boot.Name = "dispatcher.excel"
)
