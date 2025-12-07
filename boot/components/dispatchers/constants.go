package dispatchers

import "github.com/wippyai/runtime/api/boot"

const (
	// Dispatcher parent dependency
	DispatcherName boot.Name = "dispatcher"

	// System dispatchers
	ClockDispatcherName    boot.Name = "dispatcher.clock"
	ContractDispatcherName boot.Name = "dispatcher.contract"
	FuncDispatcherName     boot.Name = "dispatcher.func"
	SecurityDispatcherName boot.Name = "dispatcher.security"

	// Service dispatchers
	CloudStorageDispatcherName boot.Name = "dispatcher.cloudstorage"
	HTTPDispatcherName         boot.Name = "dispatcher.http"
	WSDispatcherName           boot.Name = "dispatcher.ws"
	ExecDispatcherName         boot.Name = "dispatcher.exec"
	StreamDispatcherName       boot.Name = "dispatcher.stream"
	SQLDispatcherName          boot.Name = "dispatcher.sql"
)
