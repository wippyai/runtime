// SPDX-License-Identifier: MPL-2.0

package membership

import (
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
	transport    memberlist.Transport
	bus          event.Bus
	meta         cluster.NodeMeta
	logger       *zap.Logger
	coll         metrics.Collector
	mp           otelmetric.MeterProvider
	tp           trace.TracerProvider
	nodeName     string
	bindAddr     string
	secretFile   string
	secretString string
	advertiseIP  string
	joinAddrs    []string
	bindPort     int
	veryVerbose  bool
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
