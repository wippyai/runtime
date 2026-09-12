// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hashicorp/go-msgpack/v2/codec"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const participantWireVersion uint8 = 1

var (
	errParticipantWireTooLarge = errors.New("participant wire payload exceeds limit")
	errParticipantWireInvalid  = errors.New("invalid participant wire payload")
)

// participantWireRequest is the bounded request envelope used by the
// participant authority feed. The node identity is deliberately absent: the
// transport supplies the authenticated peer identity outside this codec.
type participantWireRequest struct {
	Version       uint8  `codec:"v"`
	Correlation   uint64 `codec:"c"`
	Incarnation   string `codec:"i"`
	Lifetime      string `codec:"g"`
	KnownRevision uint64 `codec:"r"`
}

// participantWireResponse is the service-level response. LeaseID is omitted
// from the wire representation because registry records are never leased.
type participantWireResponse struct {
	Version     uint8
	Correlation uint64
	Revision    uint64
	Incarnation string
	Entries     []kvapi.Entry
	Lifetime    string
	Unchanged   bool
}

type participantWireResponseEnvelope struct {
	Version     uint8                  `codec:"v"`
	Correlation uint64                 `codec:"c"`
	Revision    uint64                 `codec:"r"`
	Entries     []participantWireEntry `codec:"e"`
	Incarnation string                 `codec:"i"`
	Lifetime    string                 `codec:"g"`
	Unchanged   uint8                  `codec:"u"`
}

type participantWireEntry struct {
	Key     string `codec:"k"`
	Value   []byte `codec:"v"`
	Version uint64 `codec:"r"`
	Epoch   uint64 `codec:"e"`
}

// participantWireWriter bounds both the returned length and the backing
// capacity. The encoder writes into this sink, so a failed encode is never
// returned as a partial payload.
type participantWireWriter struct {
	data     []byte
	limit    int
	exceeded bool
}

func (w *participantWireWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-len(w.data) {
		w.exceeded = true
		return 0, errParticipantWireTooLarge
	}
	needed := len(w.data) + len(p)
	if needed > cap(w.data) {
		capacity := cap(w.data) * 2
		if capacity < 64 {
			capacity = 64
		}
		if capacity > w.limit {
			capacity = w.limit
		}
		if capacity < needed {
			capacity = needed
		}
		next := make([]byte, len(w.data), capacity)
		copy(next, w.data)
		w.data = next
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

// encodeParticipantRequest encodes req as a v1 MessagePack map, enforcing
// maxBytes before returning. It returns no bytes when validation or encoding
// fails.
func encodeParticipantRequest(req *participantWireRequest, maxBytes int) ([]byte, error) {
	if req == nil || maxBytes <= 0 || req.Version != participantWireVersion || req.Correlation == 0 || req.Incarnation == "" || (req.KnownRevision == 0) != (req.Lifetime == "") {
		return nil, errParticipantWireInvalid
	}
	return encodeParticipantValue(req, maxBytes)
}

// encodeParticipantResponse encodes resp as a v1 MessagePack map, enforcing
// both maxBytes and maxEntries before returning. Lease IDs are intentionally
// dropped from every wire entry.
func encodeParticipantResponse(resp *participantWireResponse, maxBytes, maxEntries int) ([]byte, error) {
	if resp == nil || maxBytes <= 0 || maxEntries <= 0 || resp.Version != participantWireVersion || resp.Correlation == 0 || resp.Revision == 0 || resp.Incarnation == "" || len(resp.Entries) > maxEntries || resp.Lifetime == "" || (resp.Unchanged && len(resp.Entries) != 0) {
		return nil, errParticipantWireInvalid
	}
	payload := participantWireResponseEnvelope{
		Version:     resp.Version,
		Correlation: resp.Correlation,
		Revision:    resp.Revision,
		Entries:     make([]participantWireEntry, len(resp.Entries)),
		Incarnation: resp.Incarnation,
		Lifetime:    resp.Lifetime,
	}
	if resp.Unchanged {
		payload.Unchanged = 1
	}
	for i, entry := range resp.Entries {
		if entry.LeaseID != "" {
			return nil, errParticipantWireInvalid
		}
		payload.Entries[i] = participantWireEntry{
			Key:     entry.Key,
			Value:   entry.Value,
			Version: entry.Version,
			Epoch:   entry.Epoch,
		}
	}
	return encodeParticipantValue(&payload, maxBytes)
}

func encodeParticipantValue(value any, maxBytes int) ([]byte, error) {
	w := &participantWireWriter{limit: maxBytes}
	if err := codec.NewEncoder(w, &registryMsgpackHandle).Encode(value); err != nil {
		if w.exceeded {
			return nil, errParticipantWireTooLarge
		}
		return nil, err
	}
	return w.data, nil
}

// decodeParticipantRequest decodes and validates a complete v1 request. The
// map is parsed as bounded raw fields first, which detects duplicate and
// unknown envelope keys without allocating from an attacker-controlled map.
func decodeParticipantRequest(body []byte, maxBytes int) (*participantWireRequest, error) {
	if len(body) == 0 || maxBytes <= 0 {
		return nil, errParticipantWireInvalid
	}
	if len(body) > maxBytes {
		return nil, errParticipantWireTooLarge
	}
	fields, err := participantWireMapFields(body, 5)
	if err != nil {
		return nil, err
	}
	if err := participantWireExactKeys(fields, "v", "c", "i", "g", "r"); err != nil {
		return nil, err
	}
	var out participantWireRequest
	if err := decodeParticipantUint(fields["v"], &out.Version); err != nil {
		return nil, err
	}
	if err := decodeParticipantUint(fields["c"], &out.Correlation); err != nil {
		return nil, err
	}
	if err := decodeParticipantText(fields["i"], &out.Incarnation); err != nil {
		return nil, err
	}
	if err := decodeParticipantText(fields["g"], &out.Lifetime); err != nil {
		return nil, err
	}
	if err := decodeParticipantUint(fields["r"], &out.KnownRevision); err != nil {
		return nil, err
	}
	if out.Version != participantWireVersion || out.Correlation == 0 || out.Incarnation == "" || (out.KnownRevision == 0) != (out.Lifetime == "") {
		return nil, errParticipantWireInvalid
	}
	return &out, nil
}

// decodeParticipantResponse decodes and validates a complete v1 response.
// Entry-array cardinality is checked from its raw MessagePack header before
// the returned slice is allocated.
func decodeParticipantResponse(body []byte, maxBytes, maxEntries int) (*participantWireResponse, error) {
	if len(body) == 0 || maxBytes <= 0 || maxEntries <= 0 {
		return nil, errParticipantWireInvalid
	}
	if len(body) > maxBytes {
		return nil, errParticipantWireTooLarge
	}
	fields, err := participantWireMapFieldsLimit(body, 7, maxEntries)
	if err != nil {
		return nil, err
	}
	if err := participantWireExactKeys(fields, "v", "c", "r", "e", "i", "g", "u"); err != nil {
		return nil, err
	}
	var out participantWireResponse
	if err := decodeParticipantUint(fields["v"], &out.Version); err != nil {
		return nil, err
	}
	if err := decodeParticipantUint(fields["c"], &out.Correlation); err != nil {
		return nil, err
	}
	if err := decodeParticipantUint(fields["r"], &out.Revision); err != nil {
		return nil, err
	}
	if err := decodeParticipantText(fields["i"], &out.Incarnation); err != nil {
		return nil, err
	}
	if err := decodeParticipantText(fields["g"], &out.Lifetime); err != nil {
		return nil, err
	}
	var unchanged uint8
	if err := decodeParticipantUint(fields["u"], &unchanged); err != nil {
		return nil, err
	}
	out.Unchanged = unchanged == 1
	if out.Version != participantWireVersion || out.Correlation == 0 || out.Revision == 0 || out.Incarnation == "" || out.Lifetime == "" || unchanged > 1 {
		return nil, errParticipantWireInvalid
	}
	rawEntries, tooMany := rawParticipantArray(fields["e"], maxEntries)
	if tooMany {
		return nil, errParticipantWireTooLarge
	}
	if rawEntries == nil {
		return nil, fmt.Errorf("%w: entries must be an array", errParticipantWireInvalid)
	}
	if len(rawEntries) > maxEntries {
		return nil, errParticipantWireTooLarge
	}
	if out.Unchanged && len(rawEntries) != 0 {
		return nil, errParticipantWireInvalid
	}
	out.Entries = make([]kvapi.Entry, len(rawEntries))
	for i, raw := range rawEntries {
		entry, err := decodeParticipantEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: entry %d: %v", errParticipantWireInvalid, i, err)
		}
		out.Entries[i] = entry
	}
	return &out, nil
}

func participantWireMapFields(body []byte, expected uint64) (map[string]codec.Raw, error) {
	return participantWireMapFieldsLimit(body, expected, 0)
}

func participantWireMapFieldsLimit(body []byte, expected uint64, arrayLimit int) (map[string]codec.Raw, error) {
	count, offset, err := participantWireHeader(body, 0x80)
	if err != nil {
		return nil, err
	}
	if count != expected {
		return nil, fmt.Errorf("%w: expected %d envelope fields", errParticipantWireInvalid, expected)
	}
	fields := make(map[string]codec.Raw, expected)
	position := offset
	for i := uint64(0); i < count; i++ {
		rawKey, next, err := participantWireRawAt(body, position)
		if err != nil {
			return nil, err
		}
		position = next
		if len(rawKey) == 0 || !participantWireTextDescriptor(rawKey[0]) {
			return nil, fmt.Errorf("%w: map key must be text", errParticipantWireInvalid)
		}
		var key string
		if err := decodeParticipantText(rawKey, &key); err != nil {
			return nil, err
		}
		if arrayLimit > 0 && key == "e" {
			count, _, headerErr := participantWireHeader(body[position:], 0x90)
			if headerErr == nil && count > uint64(arrayLimit) {
				return nil, errParticipantWireTooLarge
			}
		}
		rawValue, next, err := participantWireRawAt(body, position)
		if err != nil {
			return nil, err
		}
		position = next
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate envelope field %q", errParticipantWireInvalid, key)
		}
		fields[key] = rawValue
	}
	if position != len(body) {
		return nil, fmt.Errorf("%w: trailing bytes", errParticipantWireInvalid)
	}
	return fields, nil
}

func participantWireExactKeys(fields map[string]codec.Raw, keys ...string) error {
	if len(fields) != len(keys) {
		return fmt.Errorf("%w: incomplete envelope", errParticipantWireInvalid)
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%w: missing envelope field %q", errParticipantWireInvalid, key)
		}
	}
	return nil
}

func decodeParticipantEntry(raw codec.Raw) (kvapi.Entry, error) {
	count, position, err := participantWireHeader(raw, 0x80)
	if err != nil || count != 4 {
		return kvapi.Entry{}, errParticipantWireInvalid
	}
	// The entry vocabulary is fixed. Track duplicates in a bitset rather than
	// allocating a map per cell of a potentially large snapshot.
	var fields [4]codec.Raw
	seen := uint8(0)
	for i := 0; i < 4; i++ {
		keyRaw, next, err := participantWireRawAt(raw, position)
		if err != nil {
			return kvapi.Entry{}, err
		}
		var key string
		if err = decodeParticipantText(keyRaw, &key); err != nil {
			return kvapi.Entry{}, err
		}
		index := 0
		switch key {
		case "k":
			index = 0
		case "v":
			index = 1
		case "r":
			index = 2
		case "e":
			index = 3
		default:
			return kvapi.Entry{}, errParticipantWireInvalid
		}
		bit := uint8(1) << index
		if seen&bit != 0 {
			return kvapi.Entry{}, errParticipantWireInvalid
		}
		seen |= bit
		value, end, err := participantWireRawAt(raw, next)
		if err != nil {
			return kvapi.Entry{}, err
		}
		fields[index] = value
		position = end
	}
	if position != len(raw) {
		return kvapi.Entry{}, errParticipantWireInvalid
	}
	var entry kvapi.Entry
	if err := decodeParticipantText(fields[0], &entry.Key); err != nil {
		return kvapi.Entry{}, err
	}
	if err := decodeParticipantBytes(fields[1], &entry.Value); err != nil {
		return kvapi.Entry{}, err
	}
	if err := decodeParticipantUint(fields[2], &entry.Version); err != nil {
		return kvapi.Entry{}, err
	}
	if err := decodeParticipantUint(fields[3], &entry.Epoch); err != nil {
		return kvapi.Entry{}, err
	}
	return entry, nil
}

func decodeParticipantUint(raw codec.Raw, out any) error {
	if len(raw) == 0 {
		return errParticipantWireInvalid
	}
	var value uint64
	descriptor := raw[0]
	if descriptor <= 0x7f {
		if len(raw) != 1 {
			return errParticipantWireInvalid
		}
		value = uint64(descriptor)
	} else {
		width := 0
		switch descriptor {
		case 0xcc, 0xd0:
			width = 1
		case 0xcd, 0xd1:
			width = 2
		case 0xce, 0xd2:
			width = 4
		case 0xcf, 0xd3:
			width = 8
		default:
			return errParticipantWireInvalid
		}
		if len(raw) != width+1 {
			return errParticipantWireInvalid
		}
		// Preserve acceptance of nonnegative signed encodings; reject negatives.
		if descriptor >= 0xd0 && raw[1]&0x80 != 0 {
			return errParticipantWireInvalid
		}
		switch width {
		case 1:
			value = uint64(raw[1])
		case 2:
			value = uint64(binary.BigEndian.Uint16(raw[1:]))
		case 4:
			value = uint64(binary.BigEndian.Uint32(raw[1:]))
		case 8:
			value = binary.BigEndian.Uint64(raw[1:])
		}
	}
	switch target := out.(type) {
	case *uint8:
		if value > 255 {
			return errParticipantWireInvalid
		}
		*target = uint8(value)
	case *uint64:
		*target = value
	default:
		return errParticipantWireInvalid
	}
	return nil
}

func decodeParticipantText(raw codec.Raw, out *string) error {
	value, err := participantWireStringBytes(raw, false)
	if err != nil {
		return err
	}
	*out = string(value)
	return nil
}

func decodeParticipantBytes(raw codec.Raw, out *[]byte) error {
	if len(raw) == 1 && raw[0] == 0xc0 {
		*out = nil
		return nil
	}
	value, err := participantWireStringBytes(raw, true)
	if err != nil {
		return err
	}
	// Own bytes independently of pooled incoming payload storage.
	*out = make([]byte, len(value))
	copy(*out, value)
	return nil
}

// Decode only string/bin framing. Length must match the entire raw field;
// compare before converting wire lengths to int, including on 32-bit hosts.
func participantWireStringBytes(raw codec.Raw, allowBin bool) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errParticipantWireInvalid
	}
	var size uint64
	offset := 1
	descriptor := raw[0]
	switch {
	case descriptor >= 0xa0 && descriptor <= 0xbf:
		size = uint64(descriptor & 0x1f)
	case descriptor == 0xd9 || (allowBin && descriptor == 0xc4):
		if len(raw) < 2 {
			return nil, errParticipantWireInvalid
		}
		size = uint64(raw[1])
		offset = 2
	case descriptor == 0xda || (allowBin && descriptor == 0xc5):
		if len(raw) < 3 {
			return nil, errParticipantWireInvalid
		}
		size = uint64(binary.BigEndian.Uint16(raw[1:3]))
		offset = 3
	case descriptor == 0xdb || (allowBin && descriptor == 0xc6):
		if len(raw) < 5 {
			return nil, errParticipantWireInvalid
		}
		size = uint64(binary.BigEndian.Uint32(raw[1:5]))
		offset = 5
	default:
		return nil, errParticipantWireInvalid
	}
	if size != uint64(len(raw)-offset) {
		return nil, errParticipantWireInvalid
	}
	return raw[offset:], nil
}

func participantWireTextDescriptor(b byte) bool {
	return (b >= 0xa0 && b <= 0xbf) || b == 0xd9 || b == 0xda || b == 0xdb
}

// participantWireHeader returns the element count and byte offset after a
// MessagePack map (kind=0x80) or array (kind=0x90) header.
func participantWireHeader(body []byte, kind byte) (uint64, int, error) {
	if len(body) == 0 {
		return 0, 0, fmt.Errorf("%w: empty container", errParticipantWireInvalid)
	}
	b := body[0]
	if b >= kind && b < kind+0x10 {
		return uint64(b & 0x0f), 1, nil
	}
	map16, map32 := byte(0xde), byte(0xdf)
	if kind == 0x90 {
		map16, map32 = 0xdc, 0xdd
	}
	if b == map16 {
		if len(body) < 3 {
			return 0, 0, fmt.Errorf("%w: truncated container header", errParticipantWireInvalid)
		}
		return uint64(binary.BigEndian.Uint16(body[1:3])), 3, nil
	}
	if b == map32 {
		if len(body) < 5 {
			return 0, 0, fmt.Errorf("%w: truncated container header", errParticipantWireInvalid)
		}
		return uint64(binary.BigEndian.Uint32(body[1:5])), 5, nil
	}
	return 0, 0, fmt.Errorf("%w: unexpected container type", errParticipantWireInvalid)
}

func rawParticipantArray(raw codec.Raw, maxEntries int) ([]codec.Raw, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	count, offset, err := participantWireHeader(raw, 0x90)
	if err != nil {
		return nil, false
	}
	if count > uint64(maxEntries) {
		return nil, true
	}
	if count > uint64(len(raw)) {
		return nil, false
	}
	items := make([]codec.Raw, int(count))
	position := offset
	for i := range items {
		item, next, err := participantWireRawAt(raw, position)
		if err != nil {
			return nil, false
		}
		items[i], position = item, next
	}
	if position != len(raw) {
		return nil, false
	}
	return items, false
}

// participantWireRawAt returns one complete MessagePack value without asking
// the codec to materialize arrays or maps. This is what lets callers reject a
// hostile declared array size before allocating the decoded entry slice.
func participantWireRawAt(data []byte, offset int) (codec.Raw, int, error) {
	end, err := participantWireSkip(data, offset, 0)
	if err != nil {
		return nil, offset, err
	}
	return codec.Raw(data[offset:end]), end, nil
}

func participantWireSkip(data []byte, offset, depth int) (int, error) {
	if depth > 64 || offset >= len(data) {
		return offset, fmt.Errorf("%w: malformed value", errParticipantWireInvalid)
	}
	b := data[offset]
	offset++
	// Positive and negative fixints, nil, booleans, and fixed-width scalars.
	switch {
	case b <= 0x7f || b >= 0xe0 || b == 0xc0 || b == 0xc2 || b == 0xc3:
		return offset, nil
	case b >= 0xa0 && b <= 0xbf:
		return participantWireTake(data, offset, int(b&0x1f))
	case b >= 0x90 && b <= 0x9f:
		return participantWireSkipN(data, offset, int(b&0x0f), true, depth)
	case b >= 0x80 && b <= 0x8f:
		return participantWireSkipN(data, offset, int(b&0x0f)*2, false, depth)
	}
	switch b {
	case 0xca:
		return participantWireTake(data, offset, 4)
	case 0xcb:
		return participantWireTake(data, offset, 8)
	case 0xcc, 0xd0:
		return participantWireTake(data, offset, 1)
	case 0xcd, 0xd1:
		return participantWireTake(data, offset, 2)
	case 0xce, 0xd2:
		return participantWireTake(data, offset, 4)
	case 0xcf, 0xd3:
		return participantWireTake(data, offset, 8)
	case 0xd4:
		return participantWireTake(data, offset, 3)
	case 0xd5:
		return participantWireTake(data, offset, 5)
	case 0xd6:
		return participantWireTake(data, offset, 9)
	case 0xd7:
		return participantWireTake(data, offset, 17)
	case 0xd8:
		return participantWireTake(data, offset, 33)
	case 0xc4, 0xd9:
		return participantWireLen(data, offset, 1)
	case 0xc5, 0xda:
		return participantWireLen(data, offset, 2)
	case 0xc6, 0xdb:
		return participantWireLen(data, offset, 4)
	case 0xdc:
		if offset+2 > len(data) {
			return offset, fmt.Errorf("%w: truncated array", errParticipantWireInvalid)
		}
		return participantWireSkipN(data, offset+2, int(binary.BigEndian.Uint16(data[offset:offset+2])), true, depth)
	case 0xdd:
		if offset+4 > len(data) {
			return offset, fmt.Errorf("%w: truncated array", errParticipantWireInvalid)
		}
		count := binary.BigEndian.Uint32(data[offset : offset+4])
		if uint64(count) > uint64(len(data)) {
			// Avoid converting an attacker-controlled count to int below.
			return offset, fmt.Errorf("%w: malformed array", errParticipantWireInvalid)
		}
		return participantWireSkipN(data, offset+4, int(count), true, depth)
	case 0xde:
		if offset+2 > len(data) {
			return offset, fmt.Errorf("%w: truncated map", errParticipantWireInvalid)
		}
		return participantWireSkipN(data, offset+2, int(binary.BigEndian.Uint16(data[offset:offset+2]))*2, false, depth)
	case 0xdf:
		if offset+4 > len(data) {
			return offset, fmt.Errorf("%w: truncated map", errParticipantWireInvalid)
		}
		count := binary.BigEndian.Uint32(data[offset : offset+4])
		if uint64(count) > uint64(len(data)) {
			return offset, fmt.Errorf("%w: malformed map", errParticipantWireInvalid)
		}
		return participantWireSkipN(data, offset+4, int(count)*2, false, depth)
	default:
		return offset, fmt.Errorf("%w: unknown MessagePack descriptor 0x%x", errParticipantWireInvalid, b)
	}
}

func participantWireSkipN(data []byte, offset, count int, array bool, depth int) (int, error) {
	if count < 0 {
		return offset, fmt.Errorf("%w: invalid container count", errParticipantWireInvalid)
	}
	for i := 0; i < count; i++ {
		var err error
		offset, err = participantWireSkip(data, offset, depth+1)
		if err != nil {
			return offset, err
		}
	}
	return offset, nil
}

func participantWireTake(data []byte, offset, n int) (int, error) {
	if n < 0 || n > len(data)-offset {
		return offset, fmt.Errorf("%w: truncated value", errParticipantWireInvalid)
	}
	return offset + n, nil
}

func participantWireLen(data []byte, offset, width int) (int, error) {
	if offset+width > len(data) {
		return offset, fmt.Errorf("%w: truncated length", errParticipantWireInvalid)
	}
	var n uint64
	switch width {
	case 1:
		n = uint64(data[offset])
	case 2:
		n = uint64(binary.BigEndian.Uint16(data[offset : offset+2]))
	case 4:
		n = uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
	default:
		return offset, fmt.Errorf("%w: invalid length width", errParticipantWireInvalid)
	}
	start := offset + width
	if n > uint64(len(data)-start) {
		return offset, fmt.Errorf("%w: truncated value", errParticipantWireInvalid)
	}
	return start + int(n), nil
}
