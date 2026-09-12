// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
)

func (t *Topology) monitorRoundTrip(ctx context.Context, exchange *monitorExchange, record *remoteWatch, lifetime *callerLifetime, control remoteMonitorControl) error {
	binding := record.binding
	if binding == nil {
		return exchange.roundTrip(ctx, control)
	}
	exchange.mu.Lock()
	if exchange.stopped {
		exchange.mu.Unlock()
		return context.Canceled
	}
	if exchange.nativePending >= exchange.maximum {
		exchange.mu.Unlock()
		return errRemoteMonitorCapacity
	}
	exchange.nativePending++
	exchange.calls.Add(1)
	exchange.mu.Unlock()
	defer func() { exchange.mu.Lock(); exchange.nativePending--; exchange.mu.Unlock(); exchange.calls.Done() }()
	return binding.WithReceiver(ctx, func(ctx context.Context, receiver relay.Receiver) error {
		provider, ok := receiver.(topapi.NativeMonitorProvider)
		if !ok {
			return errors.New("local peer does not support native monitoring")
		}
		request := topapi.NativeMonitor{Caller: control.caller, Target: control.target, Reference: control.reference, Previous: control.previous}
		if control.kind == topapi.MonitorRelease {
			return provider.ReleaseMonitor(ctx, request)
		}
		return provider.AdmitMonitor(ctx, request, func(ctx context.Context, result *runtime.Result) error {
			if result == nil {
				return errors.New("provider completion requires a terminal result")
			}
			// A retained callback must enter fresh registration admission. Capturing
			// the provider pointer or its original request context does not grant it
			// authority after retirement. The pin spans local destination admission.
			return binding.WithReceiver(ctx, func(ctx context.Context, _ relay.Receiver) error {
				return exchange.withCompletion(ctx, func(ctx context.Context) error {
					return t.deliverMonitorResult(ctx, control.caller, control.target, control.reference, result, "", func(state *processState, current *remoteWatch) bool {
						return state.remoteLifetime == lifetime && current == record && current.binding == binding
					})
				})
			})
		})
	})
}
