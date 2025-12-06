package membership

import (
	"github.com/hashicorp/memberlist"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

// Option configures a membership Service.
type Option func(*options)

type options struct {
	nodeName     string
	bindAddr     string
	bindPort     int
	joinAddrs    []string
	secretFile   string
	secretString string
	advertiseIP  string
	veryVerbose  bool
	meta         cluster.NodeMeta
	transport    memberlist.Transport
	logger       *zap.Logger
	bus          event.Bus
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

// WithBindAddr sets the address to bind for gossip.
func WithBindAddr(addr string) Option {
	return func(o *options) { o.bindAddr = addr }
}

// WithBindPort sets the port to bind for gossip.
func WithBindPort(port int) Option {
	return func(o *options) { o.bindPort = port }
}

// WithJoinAddrs sets addresses of existing cluster nodes to join.
func WithJoinAddrs(addrs ...string) Option {
	return func(o *options) { o.joinAddrs = addrs }
}

// WithSecretFile sets path to encryption key file.
func WithSecretFile(path string) Option {
	return func(o *options) { o.secretFile = path }
}

// WithSecretKey sets encryption key directly.
func WithSecretKey(key string) Option {
	return func(o *options) { o.secretString = key }
}

// WithAdvertiseIP sets the IP to advertise to other nodes.
func WithAdvertiseIP(ip string) Option {
	return func(o *options) { o.advertiseIP = ip }
}

// WithVerbose enables verbose memberlist logging.
func WithVerbose(v bool) Option {
	return func(o *options) { o.veryVerbose = v }
}

// WithMeta sets node metadata for service discovery.
func WithMeta(meta cluster.NodeMeta) Option {
	return func(o *options) { o.meta = meta }
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
