// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"encoding/binary"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestParticipantWireRequestRoundTripAndExactBound(t *testing.T) {
	want := &participantWireRequest{Version: participantWireVersion, Correlation: 7, Incarnation: "inc-1"}
	body, err := encodeParticipantRequest(want, 1024)
	require.NoError(t, err)
	require.NotEmpty(t, body)

	got, err := decodeParticipantRequest(body, len(body))
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = decodeParticipantRequest(body, len(body)-1)
	require.ErrorIs(t, err, errParticipantWireTooLarge)

	tooSmall := len(body) - 1
	if tooSmall <= 0 {
		t.Fatal("encoded request unexpectedly empty")
	}
	encoded, err := encodeParticipantRequest(want, tooSmall)
	require.ErrorIs(t, err, errParticipantWireTooLarge)
	require.Nil(t, encoded, "bounded encoder must not return partial bytes")
}

func TestParticipantWireRequestRejectsMalformedEnvelope(t *testing.T) {
	valid := &participantWireRequest{Version: participantWireVersion, Correlation: 9, Incarnation: "inc"}
	body, err := encodeParticipantRequest(valid, 1024)
	require.NoError(t, err)

	cases := map[string][]byte{
		"trailing": append(append([]byte(nil), body...), 0),
		"wrong version": participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(2))},
			participantWireField{"c", participantWireRaw(uint64(1))},
			participantWireField{"i", participantWireRaw("inc")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		),
		"zero correlation": participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))},
			participantWireField{"c", participantWireRaw(uint64(0))},
			participantWireField{"i", participantWireRaw("inc")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		),
		"wrong type": participantWireMap(
			participantWireField{"v", participantWireRaw("1")},
			participantWireField{"c", participantWireRaw(uint64(1))},
			participantWireField{"i", participantWireRaw("inc")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		),
		"unknown field": participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))},
			participantWireField{"c", participantWireRaw(uint64(1))},
			participantWireField{"x", participantWireRaw("inc")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		),
		"duplicate field": participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))},
			participantWireField{"c", participantWireRaw(uint64(1))},
			participantWireField{"i", participantWireRaw("inc")},
			participantWireField{"i", participantWireRaw("again")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		),
		"truncated": body[:len(body)-1],
	}
	for name, malformed := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeParticipantRequest(malformed, 1024)
			require.Error(t, err)
		})
	}
}

func TestParticipantWireResponseRoundTripBinaryAndLeaseExcluded(t *testing.T) {
	want := &participantWireResponse{Lifetime: "fixture-authority",
		Version:     participantWireVersion,
		Correlation: 11,
		Revision:    42,
		Incarnation: "inc-1",
		Entries: []kvapi.Entry{{
			Key:     "_sys:registry:active:name",
			LeaseID: "must-not-cross-wire",
			Value:   []byte{0x00, 0xff, 0x01, 0xfe},
			Version: 8,
			Epoch:   99,
		}},
	}
	_, err := encodeParticipantResponse(want, 4096, 4)
	require.Error(t, err, "leased records must not be silently stripped")
	want.Entries[0].LeaseID = ""
	body, err := encodeParticipantResponse(want, 4096, 4)
	require.NoError(t, err)
	got, err := decodeParticipantResponse(body, len(body), 1)
	require.NoError(t, err)
	require.Equal(t, want.Version, got.Version)
	require.Equal(t, want.Correlation, got.Correlation)
	require.Equal(t, want.Revision, got.Revision)
	require.Equal(t, want.Incarnation, got.Incarnation)
	require.Len(t, got.Entries, 1)
	require.Equal(t, want.Entries[0].Key, got.Entries[0].Key)
	require.Equal(t, want.Entries[0].Value, got.Entries[0].Value)
	require.Equal(t, want.Entries[0].Version, got.Entries[0].Version)
	require.Equal(t, want.Entries[0].Epoch, got.Entries[0].Epoch)
	require.Empty(t, got.Entries[0].LeaseID)

	_, err = decodeParticipantResponse(body, len(body), 0)
	require.Error(t, err)
}

func TestParticipantWireResponseRejectsOversizedDeclaredArrayBeforeAllocation(t *testing.T) {
	// array32 declares far more entries than permitted but contains no entry
	// payloads. The decoder must reject from the header before making a slice.
	entries := []byte{0xdd, 0x7f, 0xff, 0xff, 0xff}
	body := participantWireMap(
		participantWireField{"v", participantWireRaw(uint8(1))},
		participantWireField{"c", participantWireRaw(uint64(1))},
		participantWireField{"r", participantWireRaw(uint64(1))},
		participantWireField{"e", entries},
		participantWireField{"i", participantWireRaw("inc-1")},
		participantWireField{"g", participantWireRaw("fixture-authority")}, participantWireField{"u", participantWireRaw(uint8(0))},
	)
	_, err := decodeParticipantResponse(body, len(body), 4)
	require.ErrorIs(t, err, errParticipantWireTooLarge)
}

func TestParticipantWireResponseBoundsAndMalformedEntries(t *testing.T) {
	resp := &participantWireResponse{Lifetime: "fixture-authority",
		Version:     participantWireVersion,
		Correlation: 1,
		Revision:    1,
		Incarnation: "inc-1",
		Entries:     []kvapi.Entry{{Key: "a", Value: []byte("a"), Version: 1, Epoch: 1}, {Key: "b", Value: []byte("b"), Version: 2, Epoch: 2}},
	}
	body, err := encodeParticipantResponse(resp, 4096, 2)
	require.NoError(t, err)
	_, err = decodeParticipantResponse(body, len(body), 1)
	require.ErrorIs(t, err, errParticipantWireTooLarge)
	_, err = decodeParticipantResponse(append(append([]byte(nil), body...), 0), 4096, 2)
	require.Error(t, err)

	badEntry := participantWireMap(
		participantWireField{"k", participantWireRaw("a")},
		participantWireField{"v", participantWireRaw([]byte("x"))},
		participantWireField{"r", participantWireRaw(uint64(1))},
		participantWireField{"r", participantWireRaw(uint64(2))},
	)
	badResponse := participantWireMap(
		participantWireField{"v", participantWireRaw(uint8(1))},
		participantWireField{"c", participantWireRaw(uint64(1))},
		participantWireField{"r", participantWireRaw(uint64(1))},
		participantWireField{"e", participantWireArray(badEntry)},
		participantWireField{"i", participantWireRaw("inc-1")},
		participantWireField{"g", participantWireRaw("fixture-authority")}, participantWireField{"u", participantWireRaw(uint8(0))},
	)
	_, err = decodeParticipantResponse(badResponse, len(badResponse), 1)
	require.Error(t, err)
}

func TestParticipantWireRejectsInvalidEnvelopeValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"null incarnation", participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))}, participantWireField{"c", participantWireRaw(uint64(1))}, participantWireField{"i", []byte{0xc0}},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		)},
		{"empty incarnation", participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))}, participantWireField{"c", participantWireRaw(uint64(1))}, participantWireField{"i", participantWireRaw("")},
			participantWireField{"g", participantWireRaw("")}, participantWireField{"r", participantWireRaw(uint64(0))},
		)},
		{"null entries", participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))}, participantWireField{"c", participantWireRaw(uint64(1))}, participantWireField{"r", participantWireRaw(uint64(1))}, participantWireField{"e", []byte{0xc0}}, participantWireField{"i", participantWireRaw("inc-1")},
			participantWireField{"g", participantWireRaw("fixture-authority")}, participantWireField{"u", participantWireRaw(uint8(0))},
		)},
		{"missing incarnation", participantWireMap(
			participantWireField{"v", participantWireRaw(uint8(1))}, participantWireField{"c", participantWireRaw(uint64(1))}, participantWireField{"r", participantWireRaw(uint64(1))}, participantWireField{"e", participantWireArray()},
			participantWireField{"g", participantWireRaw("fixture-authority")}, participantWireField{"u", participantWireRaw(uint8(0))},
		)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "null incarnation" || tc.name == "empty incarnation" {
				_, err := decodeParticipantRequest(tc.body, len(tc.body))
				require.Error(t, err)
				return
			}
			_, err := decodeParticipantResponse(tc.body, len(tc.body), 1)
			require.Error(t, err)
		})
	}
}

type participantWireField struct {
	key string
	raw []byte
}

func participantWireRaw(value any) []byte {
	var out []byte
	requireNoErrorParticipantWire(codec.NewEncoderBytes(&out, &codec.MsgpackHandle{}).Encode(value))
	return out
}

func requireNoErrorParticipantWire(err error) {
	if err != nil {
		panic(err)
	}
}

func participantWireMap(fields ...participantWireField) []byte {
	var out []byte
	switch {
	case len(fields) < 16:
		out = append(out, 0x80|byte(len(fields)))
	default:
		out = append(out, 0xde, byte(len(fields)>>8), byte(len(fields)))
	}
	for _, field := range fields {
		out = append(out, participantWireRaw(field.key)...)
		out = append(out, field.raw...)
	}
	return out
}

func participantWireArray(items ...[]byte) []byte {
	var out []byte
	if len(items) < 16 {
		out = append(out, 0x90|byte(len(items)))
	} else {
		out = append(out, 0xdc, byte(len(items)>>8), byte(len(items)))
	}
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func TestParticipantWireHeaderArray16(t *testing.T) {
	entries := make([]kvapi.Entry, 16)
	for i := range entries {
		entries[i] = kvapi.Entry{Key: string(rune('a' + i)), Value: []byte{byte(i)}, Version: uint64(i + 1), Epoch: uint64(i + 1)}
	}
	body, err := encodeParticipantResponse(&participantWireResponse{Lifetime: "fixture-authority", Version: 1, Correlation: 1, Revision: 1, Incarnation: "inc-1", Entries: entries}, 65536, 16)
	require.NoError(t, err)
	got, err := decodeParticipantResponse(body, len(body), 16)
	require.NoError(t, err)
	require.Len(t, got.Entries, 16)
}

func TestParticipantWireArrayHeaderHelperUsesBigEndian(t *testing.T) {
	count := uint32(16)
	raw := []byte{0xdd, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(raw[1:], count)
	_, tooMany := rawParticipantArray(raw, 8)
	require.True(t, tooMany)
}

func TestParticipantScalarFramesAndOwnedBytes(t *testing.T) {
	for _, descriptor := range []byte{0xa3, 0xd9, 0xda, 0xdb, 0xc4, 0xc5, 0xc6} {
		raw := []byte{descriptor}
		switch descriptor {
		case 0xd9, 0xc4:
			raw = append(raw, 3)
		case 0xda, 0xc5:
			raw = append(raw, 0, 3)
		case 0xdb, 0xc6:
			raw = append(raw, 0, 0, 0, 3)
		}
		raw = append(raw, 'a', 0, 'b')
		var value []byte
		require.NoError(t, decodeParticipantBytes(raw, &value))
		require.Equal(t, []byte{'a', 0, 'b'}, value)
		value[0] = 'X'
		require.Equal(t, byte('a'), raw[len(raw)-3], "decoded bytes must own their storage")
		var text string
		err := decodeParticipantText(raw, &text)
		if descriptor >= 0xc4 && descriptor <= 0xc6 {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, "a\x00b", text)
		}
		require.Error(t, decodeParticipantBytes(raw[:len(raw)-1], &value))
		require.Error(t, decodeParticipantBytes(append(append([]byte(nil), raw...), 0), &value))
	}
	for _, raw := range [][]byte{nil, {0xdb, 255, 255, 255, 255}, {0xc6, 255, 255, 255, 255}, {0xd9}, {0xda, 0}, {0xc3}, {0xc0, 0}} {
		var value []byte
		require.Error(t, decodeParticipantBytes(raw, &value))
	}
}

func TestParticipantUintMatchesReferenceDecoder(t *testing.T) {
	for descriptor := 0; descriptor < 256; descriptor++ {
		for _, length := range []int{1, 2, 3, 5, 9, 10} {
			for _, fill := range []byte{0, 0x7f, 0x80, 0xff} {
				raw := make([]byte, length)
				raw[0] = byte(descriptor)
				for i := 1; i < len(raw); i++ {
					raw[i] = fill
				}
				for _, small := range []bool{false, true} {
					var want64, got64 uint64
					var want8, got8 uint8
					var want, got any = &want64, &got64
					if small {
						want, got = &want8, &got8
					}
					decoder := codec.NewDecoderBytes(raw, &codec.MsgpackHandle{})
					referenceErr := decoder.Decode(want)
					valid := referenceErr == nil && decoder.NumBytesRead() == len(raw) && raw[0] != 0xc0
					err := decodeParticipantUint(raw, got)
					if valid != (err == nil) {
						t.Fatalf("acceptance mismatch raw=%x small=%v reference=%v actual=%v", raw, small, referenceErr, err)
					}
					if valid && (want64 != got64 || want8 != got8) {
						t.Fatalf("value mismatch raw=%x small=%v", raw, small)
					}
				}
			}
		}
	}
}

func TestParticipantEntryAcceptsAllFieldOrders(t *testing.T) {
	fields := []participantWireField{
		{"k", participantWireRaw("name")}, {"v", participantWireRaw([]byte{0, 1, 255})},
		{"r", participantWireRaw(uint64(12))}, {"e", participantWireRaw(uint64(19))},
	}
	var permute func(int)
	permute = func(index int) {
		if index == len(fields) {
			raw := participantWireMap(fields...)
			for _, body := range [][]byte{raw, append([]byte{0xde, 0, 4}, raw[1:]...)} {
				entry, err := decodeParticipantEntry(body)
				require.NoError(t, err)
				require.Equal(t, kvapi.Entry{Key: "name", Value: []byte{0, 1, 255}, Version: 12, Epoch: 19}, entry)
			}
			return
		}
		for i := index; i < len(fields); i++ {
			fields[index], fields[i] = fields[i], fields[index]
			permute(index + 1)
			fields[index], fields[i] = fields[i], fields[index]
		}
	}
	permute(0)
}
