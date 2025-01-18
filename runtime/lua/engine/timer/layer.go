package timer

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync"
	"time"
)

type contextKey struct{}

var timerContextKey = &contextKey{}

// TimerContext maintains the state of all active timers
type TimerContext struct {
	mu           sync.Mutex
	activeTimers map[string]*timerPromise
	internalChan chan timerResult
}

// timerPromise represents a pending timer operation
type timerPromise struct {
	channelName string
	duration    time.Duration
	deadline    time.Time
	fired       bool
}

// timerResult represents the completion of a timer
type timerResult struct {
	channelName string
	err         error
}

// NewTimerContext creates a new timer context
func NewTimerContext() *TimerContext {
	return &TimerContext{
		activeTimers: make(map[string]*timerPromise),
		internalChan: make(chan timerResult, 100),
	}
}

// Layer implements the engine.Layer interface for timer operations
type Layer struct {
	chRunner *channel.Runner
}

func NewTimerLayer(chRunner *channel.Runner) *Layer {
	return &Layer{
		chRunner: chRunner,
	}
}

// getDuration extracts a time.Duration from various Lua types
func getDuration(L *lua.LState, idx int) (time.Duration, error) {
	v := L.Get(idx)
	switch v.Type() {
	case lua.LTNumber:
		return time.Duration(float64(v.(lua.LNumber)) * float64(time.Second)), nil
	case lua.LTString:
		return time.ParseDuration(string(v.(lua.LString)))
	case lua.LTUserData:
		if ud, ok := v.(*lua.LUserData); ok {
			if d, ok := ud.Value.(*Duration); ok {
				return d.duration, nil
			}
		}
	}
	return 0, fmt.Errorf("invalid duration type: %s", v.Type().String())
}

// generateUniqueChannelName creates a unique channel name for a timer
func generateUniqueChannelName(base string) string {
	return fmt.Sprintf("timer_%s_%d", base, time.Now().UnixNano())
}

// startTimer begins a new timer operation
func (tc *TimerContext) startTimer(channelName string, duration time.Duration) {
	timer := &timerPromise{
		channelName: channelName,
		duration:    duration,
		deadline:    time.Now().Add(duration),
	}

	tc.mu.Lock()
	tc.activeTimers[channelName] = timer
	tc.mu.Unlock()

	go func() {
		time.Sleep(duration)
		tc.internalChan <- timerResult{
			channelName: channelName,
		}
	}()
}

// checkAndProcessTimers processes any completed timers
func (l *Layer) checkAndProcessTimers(ctx context.Context, tg *engine.TaskGroup) error {
	tc := ctx.Value(timerContextKey).(*TimerContext)

	// Check internal channel for completed timers
	select {
	case result := <-tc.internalChan:
		tc.mu.Lock()
		if timer, exists := tc.activeTimers[result.channelName]; exists {
			timer.fired = true
			// SendToOpen the completion signal
			err := l.chRunner.SendToOpen(ctx, tg, result.channelName, lua.LTrue)
			if err != nil {
				tc.mu.Unlock()
				return err
			}
			delete(tc.activeTimers, result.channelName)
		}
		tc.mu.Unlock()
	default:
		// No timers completed
	}
	return nil
}

// Step implements the engine.Layer interface
func (l *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	ctx := cvm.GetContext()
	tg := engine.GetTaskGroup(ctx)

	if err := l.checkAndProcessTimers(ctx, tg); err != nil {
		return nil, err
	}

	return cvm.Step(tasks...)
}

// WithTimerContext adds a timer context to a context.Context
func WithTimerContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, timerContextKey, NewTimerContext())
}
