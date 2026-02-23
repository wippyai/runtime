// SPDX-License-Identifier: MPL-2.0

// Package hash provides stable hashing for registry entries.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// Hasher computes stable hashes of registry entries.
type Hasher struct {
	transcoder payload.Transcoder
}

// New creates a new Hasher with the given transcoder.
func New(transcoder payload.Transcoder) *Hasher {
	return &Hasher{
		transcoder: transcoder,
	}
}

// Hash computes a stable hash of multiple entries.
// Entries are sorted by ID before hashing to ensure stability.
func (h *Hasher) Hash(entries []registry.Entry) (string, error) {
	if len(entries) == 0 {
		return emptyHash(), nil
	}

	// Sort entries by ID for stable ordering
	sorted := make([]registry.Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID.String() < sorted[j].ID.String()
	})

	// Convert each entry to stable representation
	stableEntries := make([]stableEntry, len(sorted))
	for i, entry := range sorted {
		stable, err := h.toStable(entry)
		if err != nil {
			return "", NewEntryHashError(entry.ID.String(), err)
		}
		stableEntries[i] = stable
	}

	// Serialize to JSON with sorted keys
	data, err := json.Marshal(stableEntries)
	if err != nil {
		return "", NewMarshalError(err)
	}

	// Compute SHA-256 hash
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// stableEntry is a normalized representation of a registry entry.
type stableEntry struct {
	Data any            `json:"data,omitempty"`
	Meta map[string]any `json:"meta,omitempty"`
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
}

// toStable converts a registry entry to stable representation.
func (h *Hasher) toStable(entry registry.Entry) (stableEntry, error) {
	data, err := h.normalizePayload(entry.Data)
	if err != nil {
		return stableEntry{}, err
	}

	return stableEntry{
		ID:   entry.ID.String(),
		Kind: entry.Kind,
		Meta: h.normalizeMeta(entry.Meta),
		Data: data,
	}, nil
}

// normalizeMeta normalizes metadata values.
func (h *Hasher) normalizeMeta(meta attrs.Bag) map[string]any {
	if len(meta) == 0 {
		return nil
	}

	result := make(map[string]any, len(meta))
	for k, v := range meta {
		result[k] = normalize(v)
	}
	return result
}

// normalizePayload converts a payload to stable representation.
//
//nolint:unparam // error return reserved for future validation
func (h *Hasher) normalizePayload(p payload.Payload) (any, error) {
	if p == nil {
		return nil, nil //nolint:nilnil // nil input returns nil output
	}

	// For Golang format, use data directly
	if p.Format() == payload.Golang {
		return normalize(p.Data()), nil
	}

	// For other formats, unmarshal to Go structure
	var result any
	if err := h.transcoder.Unmarshal(p, &result); err != nil {
		// Fall back to Data() if unmarshal fails
		return normalize(p.Data()), nil
	}

	return normalize(result), nil
}

// normalize recursively normalizes Go values for stable serialization.
func normalize(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]any:
		return normalizeMap(val)
	case map[any]any:
		return normalizeGenericMap(val)
	case []any:
		return normalizeSlice(val)
	default:
		return v
	}
}

// normalizeMap converts map[string]any to sorted key-value pairs.
func normalizeMap(m map[string]any) any {
	if len(m) == 0 {
		return nil
	}

	// Extract and sort keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted key-value pairs
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		pairs[i] = kv{
			K: k,
			V: normalize(m[k]),
		}
	}

	return pairs
}

// normalizeGenericMap converts map[any]any to sorted key-value pairs.
func normalizeGenericMap(m map[any]any) any {
	if len(m) == 0 {
		return nil
	}

	// Convert to string keys and sort
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{
			K: fmt.Sprintf("%v", k),
			V: normalize(v),
		})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].K < pairs[j].K
	})

	return pairs
}

// normalizeSlice recursively normalizes slice elements.
func normalizeSlice(s []any) []any {
	if len(s) == 0 {
		return nil
	}

	result := make([]any, len(s))
	for i, v := range s {
		result[i] = normalize(v)
	}
	return result
}

// kv is a key-value pair for sorted map representation.
type kv struct {
	V any    `json:"v"`
	K string `json:"k"`
}

// emptyHash returns hash for empty entry set.
func emptyHash() string {
	sum := sha256.Sum256([]byte("[]"))
	return hex.EncodeToString(sum[:])
}
