package pubsub

import (
	"context"
	"errors"
	contextApi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

// System constants for node management
const (
	// System identifies the node management system in the event context
	System       events.System = "node"
	RegisterHost events.Kind   = "node.register_host"
	DeleteHost   events.Kind   = "node.remove_host"
	AcceptHost   events.Kind   = "node.accept_host"
	RejectHost   events.Kind   = "node.reject_host"
)

var (
	ErrAlreadyAttached   = errors.New("receiver already attached")
	ErrHostNotFound      = errors.New("host not found")
	ErrHostAlreadyExists = errors.New("host already exists")
	ErrUpstreamNotFound  = errors.New("upstream not found")
)

type (
	Topic = string

	Message struct {
		Topic    Topic
		Payloads payload.Payloads
	}

	Batch = []*Message

	Host interface {
		Upstream
		Attach(PID, chan *Batch) (error, context.CancelFunc)
	}

	Node interface {
		Host
		ID() NodeID
	}

	Upstream interface {
		Send(context.Context, PID, *Batch) error
	}

	Downstream interface {
		Send(*Batch) error
	}
)

func NewBatch(topic Topic, payloads ...payload.Payload) *Batch {
	return &Batch{
		{Topic: topic, Payloads: payloads},
	}
}

func GetNode(ctx context.Context) Node {
	return ctx.Value(contextApi.NodeCtx).(Node)
}
