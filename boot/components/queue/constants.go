// SPDX-License-Identifier: MPL-2.0

package queue

import "github.com/wippyai/runtime/api/boot"

const (
	ManagerName      boot.Name = "queue"
	QueuesName       boot.Name = "queue.declarations"
	ConsumersName    boot.Name = "queue.consumers"
	MemoryDriverName boot.Name = "queue.memory"
	AMQPDriverName   boot.Name = "queue.amqp"
)
