// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/relay"
)

// MonitorConfig bounds outstanding native monitor operations and their waits.
// A deadline reports uncertainty; it never revokes a remote relationship.
type MonitorConfig struct {
	MaxRecordsPerCaller int
	MaxPending          int
	RequestTimeout      time.Duration
}

const DefaultMonitorMaxPending = 256
const DefaultMonitorMaxRecordsPerCaller = 4096
const DefaultMonitorRequestTimeout = 5 * time.Second

// StartRemoteMonitoring installs the native reply endpoint. Boot owns the
// returned stop function. A topology instance has one endpoint lifetime; it
// cannot be reopened with stale pending observations after shutdown. Stop joins
// calls, releases the route, and reports unfinished terminal delivery obligations.
func (t *Topology) StartRemoteMonitoring(ctx context.Context, registrar relay.OwnedHostRegistrar, cfg MonitorConfig) (func() error, error) {
	if cfg.MaxRecordsPerCaller == 0 {
		cfg.MaxRecordsPerCaller = DefaultMonitorMaxRecordsPerCaller
	}
	if cfg.MaxRecordsPerCaller < 0 {
		return nil, errors.New("invalid per-caller monitor record bound")
	}
	if registrar == nil {
		return nil, errors.New("remote monitoring requires owned host registration")
	}
	sender, ok := t.router.(relay.ContextSender)
	if !ok {
		return nil, errors.New("remote monitoring requires cancellable relay")
	}
	exchange, err := newMonitorExchange(ctx, t.localNodeID, sender, cfg.MaxPending, cfg.RequestTimeout)
	if err != nil {
		return nil, err
	}
	t.monitorMu.Lock()
	defer t.monitorMu.Unlock()
	if t.monitorEndpoint != nil {
		exchange.stop()
		return nil, errors.New("remote monitor endpoint already started")
	}
	exchange.onCompletion = t.receiveMonitorCompletion
	release, err := registrar.RegisterOwnedHost(monitorControlHostID, exchange)
	if err != nil {
		exchange.stop()
		return nil, err
	}
	t.monitorMaxRecords = cfg.MaxRecordsPerCaller
	t.monitorEndpoint = exchange
	var once sync.Once
	var stopErr error
	return func() error {
		once.Do(func() {
			// Seal and join while routing still identifies this owner. Late in-flight
			// replies are rejected by the stopped exchange; release only our route.
			stopErr = exchange.stop()
			release()
		})
		return stopErr
	}, nil
}
