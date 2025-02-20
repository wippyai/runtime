package process

import (
	"context"
	context2 "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"time"
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

// todo: add pid and etc

func MergeContext(base, foreign context.Context) context.Context {
	origComplete := GetOnComplete(foreign)
	if origComplete != nil {
		base = WithAddedOnComplete(base, origComplete)
	}

	origOnStart := GetOnStart(foreign)
	if origOnStart != nil {
		base = WithAddedOnStart(base, origOnStart)
	}

	// todo: good chance that order here is broken

	return base
}

type Context struct {
	PID       pubsub.PID
	Start     time.Time
	TrapLinks bool
}

func WithContext(ctx context.Context, process Context) context.Context {
	return context.WithValue(ctx, context2.ProcessCtx, process)
}

func GetProcessContext(ctx context.Context) Context {
	return ctx.Value(context2.ProcessCtx).(Context)
}
