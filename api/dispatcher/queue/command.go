// Package queueapi provides queue command types for the dispatcher system.
package queueapi

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	queuemsg "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

func init() {
	dispatcher.MustRegisterCommands("queue", CmdQueuePublish)
}

// Command IDs for queue operations.
// Range 150-159 is reserved for messaging/queue commands.
const (
	CmdQueuePublish dispatcher.CommandID = 150 // Publish message to queue
)

// QueuePublishCmd publishes a message to a queue.
type QueuePublishCmd struct {
	Manager queuemsg.Manager
	QueueID registry.ID
	Message *queuemsg.Message
}

var queuePublishCmdPool = sync.Pool{New: func() any { return &QueuePublishCmd{} }}

func AcquireQueuePublishCmd() *QueuePublishCmd         { return queuePublishCmdPool.Get().(*QueuePublishCmd) }
func (c *QueuePublishCmd) CmdID() dispatcher.CommandID { return CmdQueuePublish }
func (c *QueuePublishCmd) Release() {
	c.Manager = nil
	c.QueueID = registry.ID{}
	c.Message = nil
	queuePublishCmdPool.Put(c)
}

// QueuePublishResponse contains the result of a publish operation.
type QueuePublishResponse struct {
	Error error
}
