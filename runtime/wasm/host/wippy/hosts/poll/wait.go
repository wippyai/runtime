// SPDX-License-Identifier: MPL-2.0

package poll

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// waitSources owns host resource references only, never guest memory.
type waitSources struct{ sources []preview2.Pollable }

func (*waitSources) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketPollWait)
}
func (w *waitSources) ToCommand() dispatcher.Command { return &socketapi.PollWaitCmd{Wait: w.wait} }
func (*waitSources) Execute(context.Context) (uint64, error) {
	return 0, errors.New("poll requires scheduler dispatch")
}

func (w *waitSources) ready() []uint32 {
	var ready []uint32
	for i, p := range w.sources {
		if p.Ready() {
			ready = append(ready, uint32(i))
		}
	}
	return ready
}

func (w *waitSources) wait(ctx context.Context) ([]uint32, error) {
	// One dispatcher goroutine per suspended poll, no goroutine per source.
	cases := make([]reflect.SelectCase, 1, len(w.sources)+2)
	cases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ready := w.ready(); len(ready) > 0 {
			return ready, nil
		}
		cases = cases[:1]
		var earliest time.Time
		for _, p := range w.sources {
			if timer, ok := p.(interface{ Deadline() time.Time }); ok {
				deadline := timer.Deadline()
				if earliest.IsZero() || deadline.Before(earliest) {
					earliest = deadline
				}
			} else if notifier, ok := p.(interface{ Notify() <-chan struct{} }); ok {
				signal := notifier.Notify()
				if signal == nil {
					return nil, errors.New("pollable returned a nil readiness signal")
				}
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(signal)})
			} else {
				return nil, errors.New("pollable has no readiness notification")
			}
		}
		var timer *time.Timer
		if !earliest.IsZero() {
			timer = time.NewTimer(max(time.Until(earliest), 0))
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timer.C)})
		}
		// Notify must return an already-closed signal when currently ready, closing
		// the race between the readiness snapshot and registration above.
		reflect.Select(cases)
		if timer != nil {
			timer.Stop()
		}
	}
}
