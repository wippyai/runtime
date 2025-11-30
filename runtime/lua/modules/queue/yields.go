package queue

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
	lua "github.com/yuin/gopher-lua"
)

// PublishYield wraps QueuePublishCmd for Lua.
type PublishYield struct {
	*queueapi.QueuePublishCmd
}

var publishYieldPool = sync.Pool{New: func() any { return &PublishYield{} }}

func AcquirePublishYield() *PublishYield {
	y := publishYieldPool.Get().(*PublishYield)
	y.QueuePublishCmd = queueapi.AcquireQueuePublishCmd()
	return y
}

func ReleasePublishYield(y *PublishYield) {
	if y.QueuePublishCmd != nil {
		y.QueuePublishCmd.Release()
		y.QueuePublishCmd = nil
	}
	publishYieldPool.Put(y)
}

func (y *PublishYield) String() string                { return "<queue_publish_yield>" }
func (y *PublishYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *PublishYield) CmdID() dispatcher.CommandID   { return queueapi.CmdQueuePublish }
func (y *PublishYield) ToCommand() dispatcher.Command { return y.QueuePublishCmd }
func (y *PublishYield) Release()                      { ReleasePublishYield(y) }
