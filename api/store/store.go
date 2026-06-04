// SPDX-License-Identifier: MPL-2.0

// Package store provides generic storage abstractions.
package store

import (
	"context"
	"sort"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// Backend identifies the broad store implementation family exposed to Lua.
	Backend string

	// Consistency describes the read/write coordination semantics a store offers.
	Consistency string

	// Info reports stable store capabilities. Backend and consistency values are
	// intentionally coarse constants, not internal Go type names.
	Info struct {
		ID             registry.ID
		Backend        Backend
		Consistency    Consistency
		Durable        bool
		List           bool
		Versioned      bool
		ConditionalPut bool
		TTL            bool
	}

	// Entry represents a key-value entry with optional TTL.
	Entry struct {
		Value payload.Payload
		Key   registry.ID
		TTL   time.Duration
	}

	// Store defines the interface for a key-value store.
	Store interface {
		// Get retrieves a value by key.
		Get(ctx context.Context, key registry.ID) (payload.Payload, error)

		// Set stores or updates a value with the given key.
		Set(ctx context.Context, entry Entry) error

		// Delete removes a value by key.
		Delete(ctx context.Context, key registry.ID) error

		// Has checks if a key exists without retrieving the value.
		Has(ctx context.Context, key registry.ID) (bool, error)
	}

	// ScanOptions configures a prefix scan operation.
	ScanOptions struct {
		Prefix string
		After  string
		Limit  int
	}

	// ListOptions configures deterministic key listing.
	ListOptions struct {
		Prefix string
		After  string
		Limit  int
	}

	// Page is a deterministic page of versioned entries.
	Page struct {
		Cursor  string
		Items   []VersionedEntry
		HasMore bool
	}

	// Scanner extends Store with prefix scan capability.
	Scanner interface {
		Store
		// Scan iterates over entries matching the prefix.
		// The callback returns true to continue, false to stop.
		Scan(ctx context.Context, opts ScanOptions, fn func(Entry) bool) error
	}

	// Version represents an entry version for optimistic concurrency.
	Version uint64

	// VersionedEntry adds version tracking to Entry.
	VersionedEntry struct {
		Entry
		Version Version // Version for CAS operations (0 = not found).
	}

	// PutOptions controls the richer put operation exposed to Lua.
	PutOptions struct {
		TTL          time.Duration
		Version      Version
		OnlyIfAbsent bool
		HasVersion   bool
	}

	// Atomic extends Store with compare-and-swap capability.
	Atomic interface {
		Store
		// GetVersioned retrieves a value with its version.
		GetVersioned(ctx context.Context, key registry.ID) (VersionedEntry, error)

		// CompareAndSwap updates the entry only if version matches.
		// Returns true if swap succeeded, false if version mismatch.
		CompareAndSwap(ctx context.Context, key registry.ID, expected Version, entry Entry) (bool, error)

		// SetIfAbsent stores the entry only if the key does not exist.
		// Returns true if stored, false if key already exists.
		SetIfAbsent(ctx context.Context, entry Entry) (bool, error)
	}

	// InfoProvider reports stable capabilities for a concrete store.
	InfoProvider interface {
		StoreInfo(ctx context.Context) Info
	}

	// EntryReader returns a value plus metadata.
	EntryReader interface {
		Store
		Entry(ctx context.Context, key registry.ID) (VersionedEntry, error)
	}

	// Lister returns deterministic pages.
	Lister interface {
		Store
		List(ctx context.Context, opts ListOptions) (Page, error)
	}

	// Putter performs richer writes and returns the stored entry metadata.
	Putter interface {
		Store
		Put(ctx context.Context, key registry.ID, value payload.Payload, opts PutOptions) (VersionedEntry, error)
	}
)

const (
	BackendKVRaft  Backend = "kv.raft"
	BackendKVCRDT  Backend = "kv.crdt"
	BackendMemory  Backend = "memory"
	BackendSQL     Backend = "sql"
	BackendUnknown Backend = "unknown"
)

const (
	ConsistencyLinearizable Consistency = "linearizable"
	ConsistencyEventual     Consistency = "eventual"
	ConsistencyLocal        Consistency = "local"
	ConsistencyUnknown      Consistency = "unknown"
)

const (
	DefaultListLimit = 100
	MaxListLimit     = 1000
)

func NormalizeListOptions(opts ListOptions) ListOptions {
	if opts.Limit <= 0 {
		opts.Limit = DefaultListLimit
	}
	if opts.Limit > MaxListLimit {
		opts.Limit = MaxListLimit
	}
	return opts
}

func Inspect(ctx context.Context, id registry.ID, s Store) Info {
	if p, ok := s.(InfoProvider); ok {
		info := p.StoreInfo(ctx)
		info.ID = id
		return info
	}
	_, scanner := s.(Scanner)
	_, atomic := s.(Atomic)
	return Info{
		ID:             id,
		Backend:        BackendUnknown,
		Consistency:    ConsistencyUnknown,
		List:           scanner,
		Versioned:      atomic,
		ConditionalPut: atomic,
		TTL:            true,
	}
}

func ReadEntry(ctx context.Context, s Store, key registry.ID) (VersionedEntry, error) {
	if r, ok := s.(EntryReader); ok {
		return r.Entry(ctx, key)
	}
	if a, ok := s.(Atomic); ok {
		return a.GetVersioned(ctx, key)
	}
	value, err := s.Get(ctx, key)
	if err != nil {
		return VersionedEntry{}, err
	}
	return VersionedEntry{Entry: Entry{Key: key, Value: value}}, nil
}

func ListEntries(ctx context.Context, s Store, opts ListOptions) (Page, error) {
	opts = NormalizeListOptions(opts)
	if l, ok := s.(Lister); ok {
		return l.List(ctx, opts)
	}
	scanner, ok := s.(Scanner)
	if !ok {
		return Page{}, ErrUnsupported
	}
	var items []VersionedEntry
	if err := scanner.Scan(ctx, ScanOptions{Prefix: opts.Prefix}, func(e Entry) bool {
		items = append(items, VersionedEntry{Entry: e})
		return true
	}); err != nil {
		return Page{}, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key.String() < items[j].Key.String()
	})
	return PageFromSorted(items, opts), nil
}

func PutEntry(ctx context.Context, s Store, key registry.ID, value payload.Payload, opts PutOptions) (VersionedEntry, error) {
	if opts.OnlyIfAbsent && opts.HasVersion {
		return VersionedEntry{}, ErrInvalidOptions
	}
	if opts.HasVersion && opts.Version == 0 {
		return VersionedEntry{}, ErrInvalidOptions
	}
	if opts.TTL < 0 {
		return VersionedEntry{}, ErrInvalidOptions
	}
	if p, ok := s.(Putter); ok {
		return p.Put(ctx, key, value, opts)
	}
	entry := Entry{Key: key, Value: value, TTL: opts.TTL}
	if opts.OnlyIfAbsent || opts.HasVersion {
		atomic, ok := s.(Atomic)
		if !ok {
			return VersionedEntry{}, ErrUnsupported
		}
		if opts.OnlyIfAbsent {
			stored, err := atomic.SetIfAbsent(ctx, entry)
			if err != nil {
				return VersionedEntry{}, err
			}
			if !stored {
				return VersionedEntry{}, ErrKeyExists
			}
			return ReadEntry(ctx, s, key)
		}
		swapped, err := atomic.CompareAndSwap(ctx, key, opts.Version, entry)
		if err != nil {
			return VersionedEntry{}, err
		}
		if !swapped {
			return VersionedEntry{}, ErrVersionMismatch
		}
		return ReadEntry(ctx, s, key)
	}
	if err := s.Set(ctx, entry); err != nil {
		return VersionedEntry{}, err
	}
	return VersionedEntry{Entry: entry}, nil
}

func PageFromSorted(items []VersionedEntry, opts ListOptions) Page {
	opts = NormalizeListOptions(opts)
	start := 0
	if opts.After != "" {
		for start < len(items) && items[start].Key.String() <= opts.After {
			start++
		}
	}
	end := start + opts.Limit
	hasMore := false
	if end < len(items) {
		hasMore = true
	} else {
		end = len(items)
	}
	pageItems := append([]VersionedEntry(nil), items[start:end]...)
	cursor := ""
	if len(pageItems) > 0 {
		cursor = pageItems[len(pageItems)-1].Key.String()
	}
	return Page{Items: pageItems, Cursor: cursor, HasMore: hasMore}
}
