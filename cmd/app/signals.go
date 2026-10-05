// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// signalScope cancels its context on os.Interrupt or SIGTERM until it is
// released. The runner releases it when it hands the process to the Wippy
// CLI: the run command owns those signals from then on (first signal stops
// gracefully, a second forces exit), so one signal never has two owners.
type signalScope struct {
	signals  <-chan os.Signal
	stop     func()
	cancel   context.CancelFunc
	released chan struct{}
	once     sync.Once
}

type signalScopeKey struct{}

// captureSignals returns a context that a termination signal cancels until
// the returned scope is released or closed.
func captureSignals(parent context.Context) (context.Context, *signalScope) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	return newSignalScope(parent, signals, func() { signal.Stop(signals) })
}

func newSignalScope(parent context.Context, signals <-chan os.Signal, stop func()) (context.Context, *signalScope) {
	ctx, cancel := context.WithCancel(parent)
	scope := &signalScope{signals: signals, stop: stop, cancel: cancel, released: make(chan struct{})}
	go func() {
		select {
		case <-signals:
			// A signal that is read after the release belongs to the CLI.
			select {
			case <-scope.released:
			default:
				cancel()
			}
		case <-scope.released:
		case <-ctx.Done():
		}
	}()
	return context.WithValue(ctx, signalScopeKey{}, scope), scope
}

// release stops capturing signals without canceling the context.
func (s *signalScope) release() {
	s.once.Do(func() {
		s.stop()
		close(s.released)
	})
}

// close releases the scope and cancels its context.
func (s *signalScope) close() {
	s.release()
	s.cancel()
}

// releaseSignals hands termination signals back from the scope that ctx
// carries, if any.
func releaseSignals(ctx context.Context) {
	if scope, ok := ctx.Value(signalScopeKey{}).(*signalScope); ok {
		scope.release()
	}
}
