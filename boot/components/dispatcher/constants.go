package dispatcher

import "github.com/wippyai/runtime/api/boot"

const (
	ClockName      boot.ComponentName = "dispatcher.clock"
	StreamName     boot.ComponentName = "dispatcher.stream"
	HTTPName       boot.ComponentName = "dispatcher.http"
	WSName         boot.ComponentName = "dispatcher.ws"
	FuncName       boot.ComponentName = "dispatcher.func"
	StoreName      boot.ComponentName = "dispatcher.store"
	QueueName      boot.ComponentName = "dispatcher.queue"
	ExecName       boot.ComponentName = "dispatcher.exec"
	ExcelName      boot.ComponentName = "dispatcher.excel"
	SecurityName   boot.ComponentName = "dispatcher.security"
	DispatcherDeps boot.ComponentName = "dispatcher" // local ref to avoid import cycle
)
