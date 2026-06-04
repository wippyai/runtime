// SPDX-License-Identifier: MPL-2.0

// Package kv adapts the low-level coordination kv engine (string keys, byte
// values) to the user-facing api/store.Store (registry.ID keys, payload values),
// scoping every store to a key namespace.
package kv

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// physicalKey maps a logical store key into the engine's flat keyspace under the
// store's namespace: "<namespace>:<ns:name>".
func physicalKey(namespace string, key registry.ID) string {
	return namespace + ":" + key.String()
}

// physicalPrefix is the engine-level scan prefix for this namespace plus an
// optional logical prefix.
func physicalPrefix(namespace, prefix string) string {
	return namespace + ":" + prefix
}

// logicalKey strips the namespace prefix and parses the remainder back into a
// registry.ID. Returns false for a key outside the namespace (defense in depth).
func logicalKey(namespace, phys string) (registry.ID, bool) {
	prefix := namespace + ":"
	if !strings.HasPrefix(phys, prefix) {
		return registry.ID{}, false
	}
	return registry.ParseID(strings.TrimPrefix(phys, prefix)), true
}

// encodeValue transcodes a payload to JSON bytes for the byte-oriented engine,
// mirroring the sql store's normalization.
func encodeValue(dtt payload.Transcoder, p payload.Payload) ([]byte, error) {
	v, err := dtt.Transcode(p, payload.JSON)
	if err != nil {
		return nil, err
	}
	switch d := v.Data().(type) {
	case []byte:
		return d, nil
	case string:
		return []byte(d), nil
	default:
		return nil, fmt.Errorf("store.kv: unexpected json payload data type %T", v.Data())
	}
}

// decodeValue reconstructs a payload from stored JSON bytes.
func decodeValue(b []byte) payload.Payload {
	return payload.NewPayload(b, payload.JSON)
}
