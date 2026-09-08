// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
)

func legacyRaftEncode(v any) ([]byte, error) {
	var buf bytes.Buffer
	err := codec.NewEncoder(&buf, &codec.MsgpackHandle{}).Encode(v)
	return buf.Bytes(), err
}
func legacyRaftDecode(data []byte, v any) error {
	return codec.NewDecoder(bytes.NewReader(data), &codec.MsgpackHandle{}).Decode(v)
}

func raftCodecSamples() []any {
	header := hraft.RPCHeader{ProtocolVersion: 3, ID: []byte("node"), Addr: []byte("node-address")}
	return []any{
		&raftFrame{},
		&raftFrame{ID: 42, Type: raftRPCAppendEntries, Request: true, Payload: []byte{0, 1, 255}, Snapshot: []byte("snapshot"), Error: "message", EOF: true},
		&hraft.AppendEntriesRequest{RPCHeader: header, Term: 2, PrevLogEntry: 7, PrevLogTerm: 1, LeaderCommitIndex: 7, Entries: []*hraft.Log{{Index: 8, Term: 2, Type: hraft.LogCommand, Data: []byte("command"), Extensions: []byte("extension")}}},
		&hraft.AppendEntriesResponse{RPCHeader: header, Term: 2, LastLog: 8, Success: true},
		&hraft.RequestVoteRequest{RPCHeader: header, Term: 2, LastLogIndex: 8, LastLogTerm: 2},
		&hraft.RequestVoteResponse{RPCHeader: header, Term: 2, Granted: true},
		&hraft.RequestPreVoteRequest{RPCHeader: header, Term: 3, LastLogIndex: 8, LastLogTerm: 2},
		&hraft.RequestPreVoteResponse{RPCHeader: header, Term: 3, Granted: true},
		&hraft.InstallSnapshotRequest{RPCHeader: header, Term: 2, LastLogIndex: 8, LastLogTerm: 2, Configuration: []byte("configuration"), Peers: []byte("peers"), Size: 123},
		&hraft.InstallSnapshotResponse{RPCHeader: header, Term: 2, Success: true},
		&hraft.TimeoutNowRequest{RPCHeader: header},
		&hraft.TimeoutNowResponse{RPCHeader: header},
	}
}

func TestRaftCodecWireCompatibilityAndOwnership(t *testing.T) {
	t.Parallel()
	for i, sample := range raftCodecSamples() {
		t.Run(fmt.Sprintf("%d-%T", i, sample), func(t *testing.T) {
			t.Parallel()
			for range 20 {
				old, err := legacyRaftEncode(sample)
				require.NoError(t, err)
				wire, err := encodeMsgpack(sample)
				require.NoError(t, err)
				require.Equal(t, old, wire, "existing peers must receive the same MessagePack fields and representation")

				typ := reflect.TypeOf(sample).Elem()
				fromOld := reflect.New(typ).Interface()
				fromNew := reflect.New(typ).Interface()
				require.NoError(t, decodeMsgpack(old, fromOld))
				require.NoError(t, legacyRaftDecode(wire, fromNew))
				require.Equal(t, sample, fromOld)
				require.Equal(t, sample, fromNew)
				clear(old)
				clear(wire)
				require.Equal(t, sample, fromOld, "decoded data must not borrow the received frame")
				require.Equal(t, sample, fromNew)

				retained, err := encodeMsgpack(sample)
				require.NoError(t, err)
				expected := bytes.Clone(retained)
				for range 3 {
					_, err = encodeMsgpack(&raftFrame{Payload: bytes.Repeat([]byte{7}, 4096)})
					require.NoError(t, err)
				}
				require.Equal(t, expected, retained, "another encode must not overwrite an earlier frame")
			}
		})
	}
}

func BenchmarkRaftMessageCodec(b *testing.B) {
	frame := &raftFrame{ID: 42, Type: raftRPCAppendEntries, Request: true, Payload: bytes.Repeat([]byte{1}, 1024)}
	wire, err := encodeMsgpack(frame)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := encodeMsgpack(frame); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var result raftFrame
			if err := decodeMsgpack(wire, &result); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func FuzzRaftFrameCodecCompatibility(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xc1})
	for _, frame := range []*raftFrame{{}, {ID: 42, Type: raftRPCAppendEntries, Request: true, Payload: []byte("payload"), Error: "error", EOF: true}} {
		wire, err := legacyRaftEncode(frame)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(wire)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var old, current raftFrame
		oldErr := legacyRaftDecode(data, &old)
		currentErr := decodeMsgpack(data, &current)
		require.Equal(t, oldErr == nil, currentErr == nil, "decoder acceptance changed")
		if oldErr == nil {
			require.Equal(t, old, current)
			clear(data)
			require.Equal(t, old, current, "decoded payload must survive reuse of receive buffer")
		}
	})
}
