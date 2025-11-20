package queue

import "github.com/wippyai/runtime/api/boot"

const (
	ManagerName      boot.ComponentName = "queues"
	QueuesName       boot.ComponentName = "queue-declarations"
	ConsumersName    boot.ComponentName = "queue-consumers"
	MemoryDriverName boot.ComponentName = "queue-memory"
)
