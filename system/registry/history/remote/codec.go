// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

type encodedPayload struct {
	Data   any
	Format payload.Format
}

type encodedEntry struct {
	Meta     attrs.Bag
	Data     *encodedPayload
	ID       registry.ID
	Kind     string
	Registry registry.EntryMetadata
}

type encodedOperation struct {
	OriginalEntry *encodedEntry
	Kind          string
	Entry         encodedEntry
}

var msgpackHandle = func() *codec.MsgpackHandle {
	handle := &codec.MsgpackHandle{}
	handle.MapType = reflect.TypeOf(map[string]any(nil))
	handle.RawToString = true
	handle.Canonical = true
	return handle
}()

func encodeEntry(entry registry.Entry) encodedEntry {
	encoded := encodedEntry{ID: entry.ID, Kind: entry.Kind, Meta: entry.Meta, Registry: entry.Registry}
	if entry.Data != nil {
		encoded.Data = &encodedPayload{Data: entry.Data.Data(), Format: entry.Data.Format()}
	}
	return encoded
}

func decodeEntry(encoded encodedEntry) registry.Entry {
	entry := registry.Entry{ID: encoded.ID, Kind: encoded.Kind, Meta: encoded.Meta, Registry: encoded.Registry}
	if encoded.Data != nil {
		entry.Data = payload.NewPayload(encoded.Data.Data, encoded.Data.Format)
	}
	return entry
}

func encode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := codec.NewEncoder(&buffer, msgpackHandle).Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func encodeChangeSet(changes registry.ChangeSet) ([]byte, error) {
	operations := make([]encodedOperation, len(changes))
	for i, op := range changes {
		operations[i] = encodedOperation{Kind: op.Kind, Entry: encodeEntry(op.Entry)}
		if op.OriginalEntry != nil {
			original := encodeEntry(*op.OriginalEntry)
			operations[i].OriginalEntry = &original
		}
	}
	return encode(operations)
}

func decodeChangeSet(data []byte) (registry.ChangeSet, error) {
	var operations []encodedOperation
	if err := codec.NewDecoderBytes(data, msgpackHandle).Decode(&operations); err != nil {
		return nil, err
	}
	changes := make(registry.ChangeSet, len(operations))
	for i, op := range operations {
		changes[i] = registry.Operation{Kind: op.Kind, Entry: decodeEntry(op.Entry)}
		if op.OriginalEntry != nil {
			original := decodeEntry(*op.OriginalEntry)
			changes[i].OriginalEntry = &original
		}
	}
	return changes, nil
}

func encodeState(state registry.State) ([]byte, string, error) {
	entries := make([]encodedEntry, len(state))
	for i, entry := range state {
		entries[i] = encodeEntry(entry)
	}
	data, err := encode(entries)
	if err != nil {
		return nil, "", err
	}
	return data, digest(data), nil
}

func decodeState(data []byte) (registry.State, error) {
	var entries []encodedEntry
	if err := codec.NewDecoderBytes(data, msgpackHandle).Decode(&entries); err != nil {
		return nil, err
	}
	state := make(registry.State, len(entries))
	for i, entry := range entries {
		state[i] = decodeEntry(entry)
	}
	return state, nil
}

func encodeResolution(resolution *registry.DependencyResolution) ([]byte, error) {
	if resolution == nil {
		return nil, nil
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return nil, registry.ErrInvalidDependencyResolution
	}
	return json.Marshal(canonical)
}

func decodeResolution(data []byte) (*registry.DependencyResolution, error) {
	if len(data) == 0 {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	var resolution registry.DependencyResolution
	if err := json.Unmarshal(data, &resolution); err != nil {
		return nil, err
	}
	if !resolution.Valid() {
		return nil, errors.New("stored dependency resolution digest mismatch")
	}
	return resolution.Canonical(), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
