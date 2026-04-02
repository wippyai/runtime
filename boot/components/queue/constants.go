// SPDX-License-Identifier: MPL-2.0

package queue

import "github.com/wippyai/runtime/api/boot"

const (
	ManagerName      boot.Name = "queues"
	QueuesName       boot.Name = "queue-declarations"
	ConsumersName    boot.Name = "queue-consumers"
	MemoryDriverName boot.Name = "queue-memory"
	AMQPDriverName   boot.Name = "queue-amqp"
	SQSDriverName    boot.Name = "queue-sqs"
	RedisDriverName  boot.Name = "queue-redis"
)
