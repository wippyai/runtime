// SPDX-License-Identifier: MPL-2.0

// Package inventory contains the inert, KV-backed participant inventory used
// by future naming protocols. It deliberately has no boot, gossip, or Raft
// wiring: callers decide when a transition is appropriate and supply the KV
// revision they observed. A committed transition records state only; callers
// must not infer admission authority without a separate authoritative
// readiness decision.
package inventory

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const (
	// Key is the one retained record used by the default inventory.
	Key = "_sys:naming:participants:v1"

	formatVersion byte = 1

	defaultMaxSlots        = 4096
	defaultMaxDomainBytes  = 128
	defaultMaxNodeBytes    = 128
	defaultMaxNonceBytes   = 64
	defaultMaxEncodedBytes = 1024 * 1024
	fixedHeaderBytes       = 4 + 1 + 2 + 1 + 8 + 2
	fixedSlotBytes         = 2 + 8 + 2 + 1 + 8

	// magic is intentionally not a generic serialization marker. It makes a
	// key containing a different protocol's bytes fail closed during migration.
	magic = "NINV"
)

var (
	ErrNotInitialized    = errors.New("participant inventory is not initialized")
	ErrRevisionConflict  = errors.New("participant inventory revision conflict")
	ErrInvalidTransition = errors.New("invalid participant inventory transition")
	ErrStaleIncarnation  = errors.New("stale participant incarnation")
	ErrCapacity          = errors.New("participant inventory capacity exceeded")
	ErrCorrupt           = errors.New("corrupt participant inventory")
	// ErrRevisionUnavailable means the KV transaction committed, but this
	// backend could not expose the post-commit revision yet. Callers must issue
	// a later ReadObserved before attempting another transition.
	ErrRevisionUnavailable = errors.New("participant inventory committed revision unavailable")
	// ErrAlreadyObserved means the exact active incarnation (or retirement) is
	// already represented at the caller's expected revision. No KV mutation was
	// issued and the returned snapshot/revision remain usable.
	ErrAlreadyObserved = errors.New("participant inventory already observed")
	// ErrRetireRequired prevents an active incarnation from being replaced by a
	// newer boot without an explicit fencing transition.
	ErrRetireRequired = errors.New("participant incarnation must be retired before replacement")
)

// Incarnation identifies one process lifetime on a node. Sequence is
// monotonic for a node; BootNonce separates incarnations when a sequence is
// accidentally reused by a restarted process.
type Incarnation struct {
	BootNonce []byte
	Sequence  uint64
}

// Participant is the exact identity admitted to the inventory.
type Participant struct {
	Node        string
	Incarnation Incarnation
}

// Slot is retained even after retirement. HighWater is the greatest sequence
// retired for Node and prevents an old incarnation from being reintroduced.
// Slots are sorted by Node in every Snapshot.
type Slot struct {
	Participant
	Retired   bool
	HighWater uint64
}

// Snapshot is the decoded retained record. The returned slices and nonce
// bytes are copies and may be changed by the caller without changing the
// inventory.
type Snapshot struct {
	Domain     string
	Slots      []Slot
	Generation uint64
}

// Options bounds decoding and controls the physical key. All limits are
// enforced before allocating based on encoded lengths.
type Options struct {
	Key             string
	MaxSlots        int
	MaxDomainBytes  int
	MaxNodeBytes    int
	MaxNonceBytes   int
	MaxEncodedBytes int
}

func (o Options) withDefaults() Options {
	if o.Key == "" {
		o.Key = Key
	}
	if o.MaxSlots <= 0 {
		o.MaxSlots = defaultMaxSlots
	}
	if o.MaxDomainBytes <= 0 {
		o.MaxDomainBytes = defaultMaxDomainBytes
	}
	if o.MaxNodeBytes <= 0 {
		o.MaxNodeBytes = defaultMaxNodeBytes
	}
	if o.MaxNonceBytes <= 0 {
		o.MaxNonceBytes = defaultMaxNonceBytes
	}
	if o.MaxEncodedBytes <= 0 {
		o.MaxEncodedBytes = defaultMaxEncodedBytes
	}
	return o
}

// Store applies caller-authorized inventory transitions to one KV record.
// It never retries a failed compare-and-swap and never deletes the record.
type Store struct {
	engine     kvapi.Engine
	leaderRead leaderReadEngine
	opts       Options
}

// leaderReadEngine is an optional read-through surface supplied by the Raft
// KV backend. It is used only to observe a just-committed write; it does not
// turn ReadObserved into an authoritative or linearizable API.
type leaderReadEngine interface {
	GetViaLeader(key string) (kvapi.Entry, error)
}

// New creates an inert inventory store. The domain is stored by Initialize;
// it is not inferred from the key or from the KV backend.
func New(engine kvapi.Engine, opts Options) (*Store, error) {
	if engine == nil {
		return nil, errors.New("participant inventory: nil engine")
	}
	o := opts.withDefaults()
	if o.MaxSlots > 65535 || o.MaxDomainBytes > 65535 || o.MaxNodeBytes > 65535 || o.MaxNonceBytes > 65535 {
		return nil, fmt.Errorf("participant inventory: limits exceed wire bounds")
	}
	if o.MaxEncodedBytes < fixedHeaderBytes {
		return nil, fmt.Errorf("participant inventory: encoded limit too small")
	}
	s := &Store{engine: engine, opts: o}
	if reader, ok := engine.(leaderReadEngine); ok {
		s.leaderRead = reader
	}
	return s, nil
}

// Initialize creates the record only when the key is absent. An initialized
// empty inventory is distinct from a missing record and remains retained.
func (s *Store) Initialize(domain string) (Snapshot, kvapi.Version, error) {
	if err := s.validateDomain(domain); err != nil {
		return Snapshot{}, 0, err
	}
	next := Snapshot{Domain: domain, Generation: 1}
	data, err := s.encode(next)
	if err != nil {
		return Snapshot{}, 0, err
	}
	ok, err := s.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: s.opts.Key, Value: data},
	})
	if err != nil {
		return Snapshot{}, 0, err
	}
	if !ok {
		return Snapshot{}, 0, ErrRevisionConflict
	}
	return s.readCommitted(next)
}

// ReadObserved decodes the local KV replica. It is intentionally not a
// linearizable read, even when the backend offers GetViaLeader or a barrier.
// Readiness code must establish its own leader/barrier contract.
func (s *Store) ReadObserved() (Snapshot, kvapi.Version, error) {
	entry, err := s.engine.Get(s.opts.Key)
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return Snapshot{}, 0, ErrNotInitialized
	}
	if err != nil {
		return Snapshot{}, 0, err
	}
	snap, err := s.decode(entry.Value)
	if err != nil {
		return Snapshot{}, entry.Version, err
	}
	return snap, entry.Version, nil
}

// Register admits or advances one node incarnation in the retained record.
// expected is the exact KV revision returned by ReadObserved or a prior
// committed operation. A stale expected revision returns ErrRevisionConflict
// without retrying. A successful record transition does not itself grant
// admission authority; that policy belongs to the caller.
func (s *Store) Register(expected kvapi.Version, p Participant) (Snapshot, kvapi.Version, error) {
	return s.transition(expected, func(cur *Snapshot) error {
		return cur.register(s, p)
	})
}

// Retire marks an exact active incarnation retired and retains its high-water
// sequence. Retiring a different incarnation is rejected as stale. The
// resulting record still does not by itself establish readiness or admission.
func (s *Store) Retire(expected kvapi.Version, p Participant) (Snapshot, kvapi.Version, error) {
	return s.transition(expected, func(cur *Snapshot) error {
		return cur.retire(s, p)
	})
}

func (s *Store) transition(expected kvapi.Version, mutate func(*Snapshot) error) (Snapshot, kvapi.Version, error) {
	if expected == 0 {
		return Snapshot{}, 0, ErrRevisionConflict
	}
	cur, currentVersion, err := s.ReadObserved()
	if err != nil {
		return Snapshot{}, 0, err
	}
	// Validate the caller's revision before interpreting the requested
	// transition against a newer snapshot. This keeps revision conflicts
	// deterministic and avoids reporting a semantic error for an already
	// stale operation.
	if currentVersion != expected {
		return Snapshot{}, 0, ErrRevisionConflict
	}
	next := cloneSnapshot(cur)
	if err := mutate(&next); err != nil {
		if errors.Is(err, ErrAlreadyObserved) {
			return cur, currentVersion, ErrAlreadyObserved
		}
		return Snapshot{}, 0, err
	}
	if next.Generation == ^uint64(0) {
		return Snapshot{}, 0, ErrCapacity
	}
	next.Generation++
	data, err := s.encode(next)
	if err != nil {
		return Snapshot{}, 0, err
	}
	ok, err := s.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: s.opts.Key, Expect: expected},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: s.opts.Key, Value: data},
	})
	if err != nil {
		return Snapshot{}, 0, err
	}
	if !ok {
		return Snapshot{}, 0, ErrRevisionConflict
	}
	// Txn exposes no resulting version. Read once to return a usable exact
	// revision; if another writer wins immediately, this is still a valid
	// observed revision and the caller will receive a conflict on stale use.
	return s.readCommitted(next)
}

func (s *Store) readCommitted(committed Snapshot) (Snapshot, kvapi.Version, error) {
	var entry kvapi.Entry
	var err error
	if s.leaderRead != nil {
		entry, err = s.leaderRead.GetViaLeader(s.opts.Key)
	} else {
		entry, err = s.engine.Get(s.opts.Key)
	}
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return committed, 0, ErrRevisionUnavailable
	}
	if err != nil {
		return committed, 0, fmt.Errorf("%w: %w", ErrRevisionUnavailable, err)
	}
	snap, err := s.decode(entry.Value)
	if err != nil {
		return committed, 0, fmt.Errorf("%w: %w", ErrRevisionUnavailable, err)
	}
	// A concurrent later transition may already be visible. It is safe to
	// return that newer snapshot, but an older local read cannot be reported as
	// the result of the committed transaction.
	if snap.Domain != committed.Domain || snap.Generation < committed.Generation {
		return committed, 0, ErrRevisionUnavailable
	}
	return snap, entry.Version, nil
}

func (s *Store) validateDomain(domain string) error {
	if len(domain) == 0 || len(domain) > s.opts.MaxDomainBytes {
		return ErrInvalidTransition
	}
	return nil
}

func (s *Store) validateParticipant(p Participant) error {
	if len(p.Node) == 0 || len(p.Node) > s.opts.MaxNodeBytes || p.Incarnation.Sequence == 0 {
		return ErrInvalidTransition
	}
	if len(p.Incarnation.BootNonce) == 0 || len(p.Incarnation.BootNonce) > s.opts.MaxNonceBytes {
		return ErrInvalidTransition
	}
	return nil
}

func (s *Store) encode(snap Snapshot) ([]byte, error) {
	if err := s.validateDomain(snap.Domain); err != nil {
		return nil, err
	}
	if snap.Generation == 0 {
		return nil, ErrCorrupt
	}
	if len(snap.Slots) > s.opts.MaxSlots || len(snap.Slots) > 65535 {
		return nil, ErrCapacity
	}
	// Validate and size the complete record before allocating it. Limits set
	// below the slot ceiling must also bound transient encode memory.
	size := fixedHeaderBytes - 1 + len(snap.Domain)
	if size > s.opts.MaxEncodedBytes {
		return nil, ErrCapacity
	}
	for i, slot := range snap.Slots {
		if err := s.validateParticipant(slot.Participant); err != nil {
			return nil, fmt.Errorf("slot %d: %w", i, err)
		}
		if i > 0 && snap.Slots[i-1].Node >= slot.Node {
			return nil, ErrCorrupt
		}
		if (slot.Retired && slot.HighWater != slot.Incarnation.Sequence) ||
			(!slot.Retired && slot.HighWater != slot.Incarnation.Sequence-1) {
			return nil, ErrCorrupt
		}
		add := fixedSlotBytes + len(slot.Node) + len(slot.Incarnation.BootNonce)
		if add > s.opts.MaxEncodedBytes-size {
			return nil, ErrCapacity
		}
		size += add
	}
	buf := make([]byte, 0, size)
	buf = append(buf, magic...)
	buf = append(buf, formatVersion)
	put16 := func(v int) { var b [2]byte; binary.BigEndian.PutUint16(b[:], uint16(v)); buf = append(buf, b[:]...) }
	put64 := func(v uint64) { var b [8]byte; binary.BigEndian.PutUint64(b[:], v); buf = append(buf, b[:]...) }
	put16(len(snap.Domain))
	buf = append(buf, snap.Domain...)
	put64(snap.Generation)
	put16(len(snap.Slots))
	for _, slot := range snap.Slots {
		put16(len(slot.Node))
		buf = append(buf, slot.Node...)
		put64(slot.Incarnation.Sequence)
		put16(len(slot.Incarnation.BootNonce))
		buf = append(buf, slot.Incarnation.BootNonce...)
		if slot.Retired {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		put64(slot.HighWater)
	}
	if len(buf) > s.opts.MaxEncodedBytes {
		return nil, ErrCapacity
	}
	return buf, nil
}

func (s *Store) decode(data []byte) (Snapshot, error) {
	if len(data) > s.opts.MaxEncodedBytes || len(data) < fixedHeaderBytes {
		return Snapshot{}, ErrCorrupt
	}
	off := 0
	need := func(n int) bool { return n >= 0 && off <= len(data)-n }
	if !need(5) || string(data[:4]) != magic || data[4] != formatVersion {
		return Snapshot{}, ErrCorrupt
	}
	off = 5
	read16 := func() (uint16, bool) {
		if !need(2) {
			return 0, false
		}
		v := binary.BigEndian.Uint16(data[off:])
		off += 2
		return v, true
	}
	read64 := func() (uint64, bool) {
		if !need(8) {
			return 0, false
		}
		v := binary.BigEndian.Uint64(data[off:])
		off += 8
		return v, true
	}
	dl, ok := read16()
	if !ok || int(dl) > s.opts.MaxDomainBytes || !need(int(dl)) {
		return Snapshot{}, ErrCorrupt
	}
	domain := string(data[off : off+int(dl)])
	off += int(dl)
	if err := s.validateDomain(domain); err != nil {
		return Snapshot{}, ErrCorrupt
	}
	generation, ok := read64()
	if !ok || generation == 0 {
		return Snapshot{}, ErrCorrupt
	}
	count, ok := read16()
	// Even a one-byte node and nonce need these bytes per slot. Reject an
	// inflated count before allocating from attacker-controlled input.
	if !ok || int(count) > s.opts.MaxSlots || int(count) > (len(data)-off)/(fixedSlotBytes+2) {
		return Snapshot{}, ErrCorrupt
	}
	slots := make([]Slot, 0, int(count))
	for i := 0; i < int(count); i++ {
		nl, ok := read16()
		if !ok || int(nl) == 0 || int(nl) > s.opts.MaxNodeBytes || !need(int(nl)) {
			return Snapshot{}, ErrCorrupt
		}
		node := string(data[off : off+int(nl)])
		off += int(nl)
		seq, ok := read64()
		if !ok || seq == 0 {
			return Snapshot{}, ErrCorrupt
		}
		bl, ok := read16()
		if !ok || int(bl) == 0 || int(bl) > s.opts.MaxNonceBytes || !need(int(bl)) {
			return Snapshot{}, ErrCorrupt
		}
		nonce := append([]byte(nil), data[off:off+int(bl)]...)
		off += int(bl)
		if !need(1) {
			return Snapshot{}, ErrCorrupt
		}
		retired := data[off] == 1
		if data[off] > 1 {
			return Snapshot{}, ErrCorrupt
		}
		off++
		high, ok := read64()
		if !ok || (retired && high != seq) || (!retired && high != seq-1) {
			return Snapshot{}, ErrCorrupt
		}
		if i > 0 && slots[i-1].Node >= node {
			return Snapshot{}, ErrCorrupt
		}
		slots = append(slots, Slot{Participant: Participant{Node: node, Incarnation: Incarnation{Sequence: seq, BootNonce: nonce}}, Retired: retired, HighWater: high})
	}
	if off != len(data) {
		return Snapshot{}, ErrCorrupt
	}
	return Snapshot{Domain: domain, Generation: generation, Slots: slots}, nil
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := Snapshot{Domain: in.Domain, Generation: in.Generation, Slots: make([]Slot, len(in.Slots))}
	for i, slot := range in.Slots {
		out.Slots[i] = slot
		out.Slots[i].Incarnation.BootNonce = append([]byte(nil), slot.Incarnation.BootNonce...)
	}
	return out
}

func (s *Snapshot) register(store *Store, p Participant) error {
	if err := store.validateParticipant(p); err != nil {
		return err
	}
	i := sort.Search(len(s.Slots), func(i int) bool { return s.Slots[i].Node >= p.Node })
	if i == len(s.Slots) || s.Slots[i].Node != p.Node {
		if p.Incarnation.Sequence != 1 {
			return ErrStaleIncarnation
		}
		if len(s.Slots) >= store.opts.MaxSlots {
			return ErrCapacity
		}
		s.Slots = append(s.Slots, Slot{})
		copy(s.Slots[i+1:], s.Slots[i:])
		s.Slots[i] = Slot{Participant: cloneParticipant(p)}
		return nil
	}
	slot := &s.Slots[i]
	if slot.Retired {
		if slot.HighWater == ^uint64(0) || p.Incarnation.Sequence != slot.HighWater+1 {
			return ErrStaleIncarnation
		}
		slot.Participant = cloneParticipant(p)
		slot.Retired = false
		return nil
	}
	cmp := compareIncarnation(p.Incarnation, slot.Incarnation)
	if cmp < 0 {
		return ErrStaleIncarnation
	}
	if cmp == 0 {
		return ErrAlreadyObserved
	}
	if p.Incarnation.Sequence <= slot.Incarnation.Sequence {
		return ErrStaleIncarnation
	}
	return ErrRetireRequired
}

func (s *Snapshot) retire(store *Store, p Participant) error {
	if err := store.validateParticipant(p); err != nil {
		return err
	}
	i := sort.Search(len(s.Slots), func(i int) bool { return s.Slots[i].Node >= p.Node })
	if i == len(s.Slots) || s.Slots[i].Node != p.Node {
		return ErrStaleIncarnation
	}
	slot := &s.Slots[i]
	if compareIncarnation(p.Incarnation, slot.Incarnation) != 0 {
		return ErrStaleIncarnation
	}
	if slot.Retired {
		return ErrAlreadyObserved
	}
	slot.Retired = true
	if slot.HighWater < p.Incarnation.Sequence {
		slot.HighWater = p.Incarnation.Sequence
	}
	return nil
}

func cloneParticipant(p Participant) Participant {
	return Participant{Node: p.Node, Incarnation: Incarnation{Sequence: p.Incarnation.Sequence, BootNonce: append([]byte(nil), p.Incarnation.BootNonce...)}}
}

func compareIncarnation(a, b Incarnation) int {
	if a.Sequence < b.Sequence {
		return -1
	}
	if a.Sequence > b.Sequence {
		return 1
	}
	return bytes.Compare(a.BootNonce, b.BootNonce)
}
