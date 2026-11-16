// SPDX-License-Identifier: MPL-2.0

package security

import (
	"hash/maphash"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

const (
	segmentShards       = 64
	segmentsPerShard    = 32
	maxInternedSegments = segmentShards * segmentsPerShard
	maxInternedPolicies = 1024
)

type policyEntry struct {
	policy     security.Policy
	descriptor security.PolicyDescriptor
	id         registry.ID
	identity   uint64
}

func describePolicy(p security.Policy) policyEntry {
	entry := policyEntry{id: p.ID(), policy: p}
	if d, ok := p.(security.PolicyDescriptor); ok {
		if identity := d.PolicyIdentity(p); identity != 0 {
			entry.descriptor = d
			entry.identity = identity
		}
	}
	return entry
}

type segment struct {
	byID    map[registry.ID]int
	plans   atomic.Pointer[actionPlans]
	handle  [1]*segment
	single  scope
	ordered []policyEntry
	mu      sync.Mutex
	opaque  bool
}

func buildSegment(entries []policyEntry) *segment {
	s := &segment{byID: make(map[registry.ID]int, len(entries)), ordered: entries}
	for i, entry := range entries {
		s.byID[entry.id] = i
		s.opaque = s.opaque || entry.identity == 0
	}
	s.handle[0] = s
	s.single.segments = s.handle[:]
	return s
}

type segmentSlot struct {
	value *segment
	key   uint64
}
type segmentTable struct {
	slots [segmentsPerShard]segmentSlot
	next  int
}
type segmentShard struct {
	table atomic.Pointer[segmentTable]
	mu    sync.Mutex
}
type segmentInterner struct {
	shards [segmentShards]segmentShard
	seed   maphash.Seed
}

func newSegmentInterner() *segmentInterner { return &segmentInterner{seed: maphash.MakeSeed()} }

var sharedSegments = newSegmentInterner()

func (c *segmentInterner) idHash(id registry.ID) uint64 {
	return maphash.String(c.seed, id.NS) ^ (maphash.String(c.seed, id.Name) * 0x9e3779b97f4a7c15)
}
func identityHash(idHash, identity uint64) uint64 {
	h := idHash ^ (identity + 0x9e3779b97f4a7c15)
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	return h ^ (h >> 31)
}

type policyIdentity struct {
	id    registry.ID
	token uint64
}

func sameIdentities(s *segment, identities []policyIdentity) bool {
	if len(s.ordered) != len(identities) {
		return false
	}
	for _, identity := range identities {
		i, ok := s.byID[identity.id]
		if !ok || s.ordered[i].identity != identity.token {
			return false
		}
	}
	return true
}
func findSegment(table *segmentTable, key uint64, identities []policyIdentity) *segment {
	if table != nil {
		for _, slot := range table.slots {
			if slot.value != nil && slot.key == key && sameIdentities(slot.value, identities) {
				return slot.value
			}
		}
	}
	return nil
}

// Readers use immutable shard snapshots. Only a miss locks and publishes a new
// fixed-size table; eviction leaves every retained scope and plan intact.
func (c *segmentInterner) intern(key uint64, identities []policyIdentity, policies []security.Policy) *segment {
	shard := &c.shards[key%segmentShards]
	if s := findSegment(shard.table.Load(), key, identities); s != nil {
		return s
	}
	shard.mu.Lock()
	defer shard.mu.Unlock()
	previous := shard.table.Load()
	if s := findSegment(previous, key, identities); s != nil {
		return s
	}
	entries := make([]policyEntry, len(identities))
	for i, identity := range identities {
		entries[i] = policyEntry{id: identity.id, policy: policies[i], identity: identity.token, descriptor: policies[i].(security.PolicyDescriptor)}
	}
	s := buildSegment(entries)
	next := new(segmentTable)
	if previous != nil {
		*next = *previous
	}
	next.slots[next.next] = segmentSlot{key: key, value: s}
	next.next = (next.next + 1) % segmentsPerShard
	shard.table.Store(next)
	return s
}

func (c *segmentInterner) fromEntry(entry policyEntry) *segment {
	if entry.identity == 0 {
		return buildSegment([]policyEntry{entry})
	}
	identities := [1]policyIdentity{{id: entry.id, token: entry.identity}}
	policies := [1]security.Policy{entry.policy}
	return c.intern(identityHash(c.idHash(entry.id), entry.identity), identities[:], policies[:])
}

// Each scratch class has its own frame: small declarations never reserve the
// group-sized buffers. Resident lookups allocate nothing, including duplicates.
func (c *segmentInterner) fromList(policies []security.Policy) *segment {
	switch {
	case len(policies) == 1:
		return c.fromEntry(describePolicy(policies[0]))
	case len(policies) <= 8:
		return c.fromList8(policies)
	case len(policies) <= 32:
		return c.fromList32(policies)
	case len(policies) <= 128:
		return c.fromList128(policies)
	case len(policies) <= 512:
		return c.fromList512(policies)
	default:
		return c.fromList1024(policies)
	}
}

func (c *segmentInterner) fromList8(policies []security.Policy) *segment {
	var identities [8]policyIdentity
	var objects [8]security.Policy
	var slots [16]uint16
	return c.normalizeList(policies, identities[:], objects[:], slots[:])
}

func (c *segmentInterner) fromList32(policies []security.Policy) *segment {
	var identities [32]policyIdentity
	var objects [32]security.Policy
	var slots [64]uint16
	return c.normalizeList(policies, identities[:], objects[:], slots[:])
}

func (c *segmentInterner) fromList128(policies []security.Policy) *segment {
	var identities [128]policyIdentity
	var objects [128]security.Policy
	var slots [256]uint16
	return c.normalizeList(policies, identities[:], objects[:], slots[:])
}

func (c *segmentInterner) fromList512(policies []security.Policy) *segment {
	var identities [512]policyIdentity
	var objects [512]security.Policy
	var slots [1024]uint16
	return c.normalizeList(policies, identities[:], objects[:], slots[:])
}

func (c *segmentInterner) fromList1024(policies []security.Policy) *segment {
	var identities [1024]policyIdentity
	var objects [1024]security.Policy
	var slots [2048]uint16
	return c.normalizeList(policies, identities[:], objects[:], slots[:])
}

func (c *segmentInterner) normalizeList(policies []security.Policy, identities []policyIdentity, objects []security.Policy, slots []uint16) *segment {
	var overflow map[registry.ID]policyEntry
	count, opaque := 0, 0
	var key uint64
	for _, p := range policies {
		entry := describePolicy(p)
		if overflow != nil {
			overflow[entry.id] = entry
			continue
		}
		idHash := c.idHash(entry.id)
		slot := idHash % uint64(len(slots))
		for slots[slot] != 0 && identities[slots[slot]-1].id != entry.id {
			slot = (slot + 1) % uint64(len(slots))
		}
		index := int(slots[slot]) - 1
		if index < 0 {
			if count == len(identities) {
				overflow = make(map[registry.ID]policyEntry, count+1)
				for i := 0; i < count; i++ {
					e := entryFromIdentity(identities[i], objects[i])
					overflow[e.id] = e
				}
				overflow[entry.id] = entry
				continue
			}
			index = count
			count++
			slots[slot] = uint16(index + 1)
		} else {
			key -= identityHash(idHash, identities[index].token)
			if identities[index].token == 0 {
				opaque--
			}
		}
		identities[index] = policyIdentity{id: entry.id, token: entry.identity}
		objects[index] = p
		key += identityHash(idHash, entry.identity)
		if entry.identity == 0 {
			opaque++
		}
	}
	if overflow != nil {
		entries := make([]policyEntry, 0, len(overflow))
		for _, e := range overflow {
			entries = append(entries, e)
		}
		return buildSegment(entries)
	}
	if opaque == 0 {
		return c.intern(key, identities[:count], objects[:count])
	}
	entries := make([]policyEntry, count)
	for i := range entries {
		entries[i] = entryFromIdentity(identities[i], objects[i])
	}
	return buildSegment(entries)
}
func entryFromIdentity(identity policyIdentity, p security.Policy) policyEntry {
	e := policyEntry{id: identity.id, identity: identity.token, policy: p}
	if e.identity != 0 {
		e.descriptor = p.(security.PolicyDescriptor)
	}
	return e
}
func makeScope(policies []security.Policy) *scope { return &sharedSegments.fromList(policies).single }
