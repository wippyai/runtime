package process

import (
	"context"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
)

// OnComplete is the type for a completion callback.
type OnComplete func(pid pubsub.PID, result *runtime.Result)

// OnStart is the type for a start callback.
type OnStart func(pid pubsub.PID, proc Process)

type onCompleteKeyType struct{}
type onStartKeyType struct{}

var onCompleteKey = &onCompleteKeyType{} //nolint:gochecknoglobals
var onStartKey = &onStartKeyType{}       //nolint:gochecknoglobals

// WithAddedOnComplete attaches an OnComplete callback to the context.
// If there's already one present, it combines them so that both are called.
func WithAddedOnComplete(ctx context.Context, cb OnComplete) context.Context {
	if existing, ok := ctx.Value(onCompleteKey).(OnComplete); ok {
		combined := func(pid pubsub.PID, result *runtime.Result) {
			cb(pid, result)
			existing(pid, result)
		}
		return context.WithValue(ctx, onCompleteKey, OnComplete(combined))
	}

	return context.WithValue(ctx, onCompleteKey, cb)
}

// WithAddedOnStart attaches an OnStart callback to the context.
// If there's already one present, it combines them so that both are called.
func WithAddedOnStart(ctx context.Context, cb OnStart) context.Context {
	if existing, ok := ctx.Value(onStartKey).(OnStart); ok {
		combined := func(pid pubsub.PID, proc Process) {
			cb(pid, proc)
			existing(pid, proc)
		}
		return context.WithValue(ctx, onStartKey, OnStart(combined))
	}

	return context.WithValue(ctx, onStartKey, cb)
}

// GetOnComplete retrieves the OnComplete callback from the context.
func GetOnComplete(ctx context.Context) OnComplete {
	if cb, ok := ctx.Value(onCompleteKey).(OnComplete); ok {
		return cb
	}
	return nil
}

// GetOnStart retrieves the OnStart callback from the context.
func GetOnStart(ctx context.Context) OnStart {
	if cb, ok := ctx.Value(onStartKey).(OnStart); ok {
		return cb
	}
	return nil
}
