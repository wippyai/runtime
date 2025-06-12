package consensus

import (
	"context"
	"github.com/ponyruntime/pony/api/event" // :contentReference[oaicite:2]{index=2}
)

/* ---------- event constants (optional) ---------- */

const System event.System = "consensus"

const (
	KindPut    event.Kind = "kv.put"    // payload = Change
	KindDelete event.Kind = "kv.delete" // payload = Change
)

/* ---------- user-visible types ---------- */

// Change is what subscribers receive in Bus events.
type Change struct {
	Key string // full key that changed
	Rev uint64 // new revision (0 on delete)
	Val []byte // nil on delete
}

// Store exposes the only three ops the runtime needs.
type Store interface {
	// Get returns the current value and its revision.
	Get(ctx context.Context, key string) (val []byte, rev uint64, err error)

	// Put stores val under key if rev matches expectRev.
	//   expectRev == 0  ➜  create-only
	//   expectRev == N  ➜  CAS
	// Returns the new revision or ErrCAS on mismatch.
	Put(ctx context.Context, key string, val []byte, expectRev uint64) (newRev uint64, err error)

	// Delete removes key if its revision == expectRev.
	//   expectRev == 0  ➜  unconditional delete
	Delete(ctx context.Context, key string, expectRev uint64) (ok bool, err error)
}

// Facade is what services depend on.
type Facade interface {
	Store() Store

	// Bridge wires Raft commits into an existing event.Bus.  If you don’t
	// care about change events, just never call it.
	Bridge(bus event.Bus)
}
