package dispatchers

import "github.com/wippyai/runtime/api/boot"

const (
	// Dispatcher parent dependency
	DispatcherName boot.ComponentName = "dispatcher"

	// System dispatchers
	ClockDispatcherName    boot.ComponentName = "dispatcher.clock"
	FuncDispatcherName     boot.ComponentName = "dispatcher.func"
	SecurityDispatcherName boot.ComponentName = "dispatcher.security"
	QueueDispatcherName    boot.ComponentName = "dispatcher.queue"

	// Service dispatchers
	HTTPDispatcherName   boot.ComponentName = "dispatcher.http"
	WSDispatcherName     boot.ComponentName = "dispatcher.ws"
	ExecDispatcherName   boot.ComponentName = "dispatcher.exec"
	StreamDispatcherName boot.ComponentName = "dispatcher.stream"
	ExcelDispatcherName  boot.ComponentName = "dispatcher.excel"
)
