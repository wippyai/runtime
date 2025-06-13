package pubsub

import (
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"sync"
)

// Router is a minimal implementation of the Receiver interface.
// It routes a Package to one of the configured upstream Receivers based on
// `pkg.Target.Node`. When no matching upstream exists the optional
// `internode` fallback is tried instead. If that is nil an error is
// returned.
//
// The implementation is *deliberately* simple: we do not attempt retries,
// round‑robin, health‑checks, or dynamic updates – callers are expected to
// rebuild the Router when their topology changes.
//
// Example:
//
//  up := map[NodeID]Receiver{"node‑a": aConn, "node‑b": bConn}
//  r := NewRouter(up, internodeSvc)
//  _ = r.Send(pkg)
//
// Note: Once the Router is created its upstream set is immutable.
//
// The zero value is not usable – always create via NewRouter.

type Router struct {
	upstreams sync.Map        // NodeID -> Receiver (immutable after New)
	internode pubsub.Receiver // optional fallback
}

// NewRouter initialises a Router with the provided upstream mapping and
// optional internode fallback. The upstream map *will be copied* – the caller
// is free to mutate their original map afterwards.
func NewRouter(upstreams map[pubsub.NodeID]pubsub.Receiver, upstream pubsub.Receiver) *Router {
	r := &Router{internode: upstream}
	for id, rc := range upstreams {
		r.upstreams.Store(id, rc)
	}
	return r
}

// Send implements the pubsub.Receiver interface.
// If a Receiver for pkg.Target.Node exists, it is used. Otherwise we fallback
// to the internode Receiver when present.
func (r *Router) Send(pkg *pubsub.Package) error {
	if pkg == nil {
		return fmt.Errorf("nil package")
	}

	if rc, ok := r.upstreams.Load(pkg.Target.Node); ok {
		return rc.(pubsub.Receiver).Send(pkg)
	}

	if r.internode != nil {
		return r.internode.Send(pkg)
	}

	return fmt.Errorf("router: no upstream for node %s", pkg.Target.Node)
}
