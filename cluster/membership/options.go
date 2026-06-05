// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Option configures a membership Service.
type Option func(*options)

type options struct {
	transport           memberlist.Transport
	bus                 event.Bus
	coll                metrics.Collector
	mp                  otelmetric.MeterProvider
	tp                  trace.TracerProvider
	meta                cluster.NodeMeta
	logger              *zap.Logger
	nodeName            string
	bindAddr            string
	secretFile          string
	secretString        string
	advertiseIP         string
	joinAddrs           []string
	gossipInterval      time.Duration
	pushPullInterval    time.Duration
	deadNodeReclaimTime time.Duration
	bindPort            int
	veryVerbose         bool
}

func defaultOptions() *options {
	return &options{
		bindAddr: "0.0.0.0",
		bindPort: 7946,
		meta:     make(cluster.NodeMeta),
		logger:   zap.NewNop(),
	}
}

// WithNodeName sets the unique node identifier.
func WithNodeName(name string) Option {
	return func(o *options) { o.nodeName = name }
}

// WithJoinAddrs sets addresses of existing cluster nodes to join.
func WithJoinAddrs(addrs ...string) Option {
	return func(o *options) { o.joinAddrs = addrs }
}

// WithTransport sets a custom memberlist transport.
// Use this for testing with MockNetwork.
func WithTransport(t memberlist.Transport) Option {
	return func(o *options) { o.transport = t }
}

// WithLogger sets the logger.
func WithLogger(l *zap.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithEventBus sets the event bus for publishing cluster events.
func WithEventBus(bus event.Bus) Option {
	return func(o *options) { o.bus = bus }
}

// WithTelemetry wires the metrics collector and OTel providers used for
// gossip/membership instrumentation. Any of the arguments may be nil; nil
// disables the corresponding emission path.
func WithTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) Option {
	return func(o *options) {
		o.coll = coll
		o.mp = mp
		o.tp = tp
	}
}

// WithGossipInterval tunes memberlist's UDP gossip cadence. Non-positive values
// fall back to the production default at Start.
func WithGossipInterval(d time.Duration) Option {
	return func(o *options) { o.gossipInterval = d }
}

// WithPushPullInterval tunes memberlist's TCP full-state anti-entropy cadence.
// Non-positive values fall back to the production default at Start.
func WithPushPullInterval(d time.Duration) Option {
	return func(o *options) { o.pushPullInterval = d }
}

// WithDeadNodeReclaimTime tunes how long a dead same-name member must remain
// dead before a different address can reclaim that node name. Non-positive
// values fall back to the production default at Start.
func WithDeadNodeReclaimTime(d time.Duration) Option {
	return func(o *options) { o.deadNodeReclaimTime = d }
}
