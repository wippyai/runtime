// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/msgpack"
	"github.com/wippyai/runtime/system/payload/yaml"
)

// mockTranscoder for testing
type mockTranscoder struct{}

func (mt *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil // Pass through
}

func (mt *mockTranscoder) Unmarshal(_ payload.Payload, _ any) error {
	return nil
}

type mutatingTranscoder struct{}

func (mt *mutatingTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	m, ok := p.Data().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mutating transcoder got %T", p.Data())
	}
	m["normalized"] = true
	if reps, ok := m["replicas"].([]any); ok {
		for i, rep := range reps {
			if rm, ok := rep.(map[string]any); ok {
				rm["ordinal"] = i
			}
		}
	}
	return payload.New(m), nil
}

func (mt *mutatingTranscoder) Unmarshal(_ payload.Payload, _ any) error {
	return nil
}

// createRealTranscoder creates a fully configured transcoder
func createRealTranscoder() *transcoder.Transcoder {
	dtt := transcoder.NewTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	msgpack.Register(dtt)
	luapayload.Register(dtt)
	luapayload.RegisterMsgPack(dtt)
	return dtt
}

func TestMessageCodec_PackagePIDs_SourceTarget(t *testing.T) {
	codec := NewMessageCodec(&mockTranscoder{})

	// Create PIDs with actual values
	sourcePID := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "src123",
	}

	targetPID := pid.PID{
		Node:   "node2",
		Host:   "host2",
		UniqID: "tgt456",
	}

	// Create package with both Source and Target
	originalPkg := &relay.Package{
		Source: sourcePID,
		Target: targetPID,
		Messages: []*relay.Message{
			{
				Topic: "test.topic",
				Payloads: []payload.Payload{
					payload.NewString("test message"),
				},
			},
		},
	}

	t.Logf("Original Source: %s", originalPkg.Source.String())
	t.Logf("Original Target: %s", originalPkg.Target.String())

	// Encode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	t.Logf("Decoded Source: %s", decoded.Source.String())
	t.Logf("Decoded Target: %s", decoded.Target.String())

	// Verify Source PID
	if decoded.Source.Node != originalPkg.Source.Node {
		t.Errorf("Source Node mismatch. Expected %q, got %q", originalPkg.Source.Node, decoded.Source.Node)
	}
	if decoded.Source.Host != originalPkg.Source.Host {
		t.Errorf("Source Host mismatch. Expected %q, got %q", originalPkg.Source.Host, decoded.Source.Host)
	}
	if decoded.Source.UniqID != originalPkg.Source.UniqID {
		t.Errorf("Source UniqID mismatch. Expected %q, got %q", originalPkg.Source.UniqID, decoded.Source.UniqID)
	}

	// Verify Target PID
	if decoded.Target.Node != originalPkg.Target.Node {
		t.Errorf("Target Node mismatch. Expected %q, got %q", originalPkg.Target.Node, decoded.Target.Node)
	}
	if decoded.Target.Host != originalPkg.Target.Host {
		t.Errorf("Target Host mismatch. Expected %q, got %q", originalPkg.Target.Host, decoded.Target.Host)
	}
	if decoded.Target.UniqID != originalPkg.Target.UniqID {
		t.Errorf("Target UniqID mismatch. Expected %q, got %q", originalPkg.Target.UniqID, decoded.Target.UniqID)
	}

	// Verify message content
	if len(decoded.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(decoded.Messages))
	}
	if decoded.Messages[0].Topic != "test.topic" {
		t.Errorf("Topic mismatch. Expected 'test.topic', got %q", decoded.Messages[0].Topic)
	}
}

func TestMessageCodec_ConcurrentEncodeSharedMapPayload(t *testing.T) {
	const mutableFormat payload.Format = "test/mutable-map"
	codec := NewMessageCodec(&mutatingTranscoder{})
	shared := map[string]any{
		"service": "qwen-fleet",
		"replicas": []any{
			map[string]any{"node": "towers", "port": 8000},
			map[string]any{"node": "towers", "port": 8001},
			map[string]any{"node": "mac", "port": 8000},
		},
	}
	pkg := &relay.Package{
		Source: pid.PID{Node: "leader", Host: "pg", UniqID: "src"},
		Target: pid.PID{Node: "worker", Host: "pg", UniqID: "dst"},
		Messages: []*relay.Message{{
			Topic:    "app.broadcast.deploy",
			Payloads: []payload.Payload{payload.NewPayload(shared, mutableFormat)},
		}},
	}

	const workers = 32
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				encoded, err := codec.Encode(pkg)
				if err != nil {
					errs <- err
					return
				}
				decoded, err := codec.Decode(encoded)
				if err != nil {
					errs <- err
					return
				}
				data := decoded.Messages[0].Payloads[0].Data().(map[string]any)
				if data["service"] != "qwen-fleet" {
					errs <- fmt.Errorf("decoded service = %v", data["service"])
					relay.ReleasePackage(decoded)
					return
				}
				if data["normalized"] != true {
					errs <- fmt.Errorf("decoded normalized = %v", data["normalized"])
					relay.ReleasePackage(decoded)
					return
				}
				relay.ReleasePackage(decoded)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	encoded, err := codec.Encode(pkg)
	if err != nil {
		t.Fatalf("encode after fanout: %v", err)
	}
	shared["service"] = "mutated-after-encode"
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("decode after source mutation: %v", err)
	}
	defer relay.ReleasePackage(decoded)
	got := decoded.Messages[0].Payloads[0].Data().(map[string]any)
	if got["service"] != "qwen-fleet" {
		t.Fatalf("encoded payload aliased source map, got service=%v", got["service"])
	}
	if _, ok := shared["normalized"]; ok {
		t.Fatalf("transcoder mutated shared source map")
	}
}

func TestEncodeData_DoesNotCopyImmutableWireFormats(t *testing.T) {
	raw := []byte("wire")
	for _, p := range []payload.Payload{
		payload.NewPayload(raw, payload.JSON),
		payload.NewPayload(raw, payload.Bytes),
		payload.NewPayload(raw, payload.MsgPack),
		payload.NewString("text"),
	} {
		allocs := testing.AllocsPerRun(1000, func() {
			_ = encodeData(p)
		})
		if allocs != 0 {
			t.Fatalf("encodeData(%s) allocations = %v, want 0", p.Format(), allocs)
		}
	}
}

func TestEncodeData_SnapshotsGolangMap(t *testing.T) {
	src := map[string]any{"k": "v"}
	got := encodeData(payload.New(src)).(map[string]any)
	src["k"] = "mutated"
	if got["k"] != "v" {
		t.Fatalf("Golang map payload was not snapshotted: %v", got["k"])
	}
}

func TestMessageCodec_EmptyPIDs(t *testing.T) {
	codec := NewMessageCodec(&mockTranscoder{})

	// Package with empty PIDs (this is what we're seeing in logs)
	originalPkg := &relay.Package{
		Source: pid.PID{}, // Empty
		Target: pid.PID{}, // Empty
		Messages: []*relay.Message{
			{
				Topic: "test.topic",
				Payloads: []payload.Payload{
					payload.NewString("test message"),
				},
			},
		},
	}

	t.Logf("Original Source (empty): %s", originalPkg.Source.String())
	t.Logf("Original Target (empty): %s", originalPkg.Target.String())

	// This should be {|} for both (new PID format without NS:Name)
	if originalPkg.Source.String() != "{|}" {
		t.Errorf("Expected empty source to be {|}, got %s", originalPkg.Source.String())
	}
	if originalPkg.Target.String() != "{|}" {
		t.Errorf("Expected empty target to be {|}, got %s", originalPkg.Target.String())
	}

	// Encode/decode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	t.Logf("Decoded Source (should be empty): %s", decoded.Source.String())
	t.Logf("Decoded Target (should be empty): %s", decoded.Target.String())

	// Verify they remain empty after round-trip
	if decoded.Source.String() != "{|}" {
		t.Errorf("Expected decoded source to be {|}, got %s", decoded.Source.String())
	}
	if decoded.Target.String() != "{|}" {
		t.Errorf("Expected decoded target to be {|}, got %s", decoded.Target.String())
	}
}

// End-to-end tests with real transcoder

func TestMessageCodec_EndToEnd_LuaPayload(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	// Create Lua table
	tbl := lua.CreateTable(0, 3)
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)

	originalPkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic:    "lua.test",
				Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
			},
		},
	}

	// Encode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify message
	if len(decoded.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(decoded.Messages))
	}

	// Lua is transcoded to Golang format for serialization
	msg := decoded.Messages[0]
	if msg.Topic != "lua.test" {
		t.Errorf("Topic = %s, want lua.test", msg.Topic)
	}

	if len(msg.Payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(msg.Payloads))
	}

	// After decode, it's Golang format (map[string]any)
	p := msg.Payloads[0]
	if p.Format() != payload.Golang {
		t.Errorf("Format = %s, want %s", p.Format(), payload.Golang)
	}

	m, ok := p.Data().(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", p.Data())
	}

	// msgpack decodes strings as []byte
	switch name := m["name"].(type) {
	case string:
		if name != "test" {
			t.Errorf("name = %v, want test", name)
		}
	case []byte:
		if string(name) != "test" {
			t.Errorf("name = %v, want test", string(name))
		}
	default:
		t.Errorf("name type = %T, want string or []byte", m["name"])
	}
}

func TestMessageCodec_EndToEnd_MixedPayloads(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	// Create various payload types
	luaTbl := lua.CreateTable(0, 1)
	luaTbl.RawSetString("lua", lua.LString("value"))

	originalPkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic: "mixed.test",
				Payloads: []payload.Payload{
					payload.NewString("string payload"),
					payload.NewPayload([]byte(`{"json":"data"}`), payload.JSON),
					payload.NewPayload([]byte("raw bytes"), payload.Bytes),
					payload.NewPayload(map[string]any{"go": "value"}, payload.Golang),
					payload.NewPayload(luaTbl, payload.Lua),
				},
			},
		},
	}

	// Encode
	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(decoded.Messages))
	}

	payloads := decoded.Messages[0].Payloads
	if len(payloads) != 5 {
		t.Fatalf("Expected 5 payloads, got %d", len(payloads))
	}

	// Check formats are preserved (except Lua which becomes Golang)
	expectedFormats := []payload.Format{
		payload.String,
		payload.JSON,
		payload.Bytes,
		payload.Golang,
		payload.Golang, // Lua is transcoded to Golang
	}

	for i, expected := range expectedFormats {
		if payloads[i].Format() != expected {
			t.Errorf("Payload[%d] format = %s, want %s", i, payloads[i].Format(), expected)
		}
	}
}

func TestMessageCodec_EndToEnd_NestedLuaTables(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	// Create nested Lua tables
	nested := lua.CreateTable(0, 2)
	nested.RawSetString("x", lua.LNumber(1))
	nested.RawSetString("y", lua.LNumber(2))

	arr := lua.CreateTable(3, 0)
	arr.RawSetInt(1, lua.LString("a"))
	arr.RawSetInt(2, lua.LString("b"))
	arr.RawSetInt(3, lua.LString("c"))

	root := lua.CreateTable(0, 3)
	root.RawSetString("nested", nested)
	root.RawSetString("array", arr)
	root.RawSetString("value", lua.LNumber(42))

	originalPkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: []*relay.Message{
			{
				Topic:    "nested.test",
				Payloads: []payload.Payload{payload.NewPayload(root, payload.Lua)},
			},
		},
	}

	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	data := decoded.Messages[0].Payloads[0].Data()
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", data)
	}

	// Check nested map
	nestedMap, ok := m["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested type = %T, want map[string]any", m["nested"])
	}
	if nestedMap["x"] != float64(1) && nestedMap["x"] != int64(1) {
		t.Errorf("nested.x = %v, want 1", nestedMap["x"])
	}

	// Check array
	arr2, ok := m["array"].([]any)
	if !ok {
		t.Fatalf("array type = %T, want []any", m["array"])
	}
	if len(arr2) != 3 {
		t.Errorf("array len = %d, want 3", len(arr2))
	}
}

func TestMessageCodec_EndToEnd_MultipleMessages(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	originalPkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt"},
		Messages: []*relay.Message{
			{Topic: "topic1", Payloads: []payload.Payload{payload.NewString("msg1")}},
			{Topic: "topic2", Payloads: []payload.Payload{payload.NewString("msg2")}},
			{Topic: "topic3", Payloads: []payload.Payload{payload.NewString("msg3")}},
		},
	}

	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(decoded.Messages))
	}

	expectedTopics := []string{"topic1", "topic2", "topic3"}
	for i, expected := range expectedTopics {
		if decoded.Messages[i].Topic != expected {
			t.Errorf("Message[%d].Topic = %s, want %s", i, decoded.Messages[i].Topic, expected)
		}
	}
}

func TestMessageCodec_EndToEnd_LargePayload(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	// Create large Lua table with many entries
	tbl := lua.CreateTable(100, 0)
	for i := 1; i <= 100; i++ {
		inner := lua.CreateTable(0, 2)
		inner.RawSetString("index", lua.LNumber(float64(i)))
		inner.RawSetString("data", lua.LString("value"))
		tbl.RawSetInt(i, inner)
	}

	originalPkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: []*relay.Message{
			{
				Topic:    "large.test",
				Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
			},
		},
	}

	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	data := decoded.Messages[0].Payloads[0].Data()
	arr, ok := data.([]any)
	if !ok {
		t.Fatalf("Data type = %T, want []any", data)
	}

	if len(arr) != 100 {
		t.Errorf("Array len = %d, want 100", len(arr))
	}
}

// Benchmarks

func BenchmarkMessageCodec_Encode_LuaPayload(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)
	tbl.RawSetString("ratio", lua.LNumber(3.14))
	tbl.RawSetString("tags", lua.LString("a,b,c"))

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic:    "bench.test",
				Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_Decode_LuaPayload(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic:    "bench.test",
				Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
			},
		},
	}

	encoded, _ := codec.Encode(pkg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := codec.Decode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_RoundTrip_LuaPayload(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic:    "bench.test",
				Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = codec.Decode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_Encode_GolangPayload(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	data := map[string]any{
		"name":    "benchmark",
		"count":   42,
		"enabled": true,
		"ratio":   3.14,
		"tags":    []any{"a", "b", "c"},
	}

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Target: pid.PID{Node: "node2", Host: "host2", UniqID: "tgt456"},
		Messages: []*relay.Message{
			{
				Topic:    "bench.test",
				Payloads: []payload.Payload{payload.NewPayload(data, payload.Golang)},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_NestedLuaTable(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	nested := lua.CreateTable(0, 2)
	nested.RawSetString("x", lua.LNumber(1))
	nested.RawSetString("y", lua.LNumber(2))

	arr := lua.CreateTable(10, 0)
	for i := 1; i <= 10; i++ {
		arr.RawSetInt(i, lua.LNumber(float64(i)))
	}

	root := lua.CreateTable(0, 3)
	root.RawSetString("nested", nested)
	root.RawSetString("array", arr)
	root.RawSetString("value", lua.LNumber(42))

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: []*relay.Message{
			{
				Topic:    "bench.nested",
				Payloads: []payload.Payload{payload.NewPayload(root, payload.Lua)},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = codec.Decode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_MixedPayloads(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	luaTbl := lua.CreateTable(0, 1)
	luaTbl.RawSetString("lua", lua.LString("value"))

	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src123"},
		Messages: []*relay.Message{
			{
				Topic: "mixed.bench",
				Payloads: []payload.Payload{
					payload.NewString("string"),
					payload.NewPayload([]byte(`{"json":"data"}`), payload.JSON),
					payload.NewPayload([]byte("bytes"), payload.Bytes),
					payload.NewPayload(map[string]any{"go": "value"}, payload.Golang),
					payload.NewPayload(luaTbl, payload.Lua),
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = codec.Decode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageCodec_MultipleMessages(b *testing.B) {
	codec := NewMessageCodec(createRealTranscoder())

	pkg := &relay.Package{
		Source:   pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: make([]*relay.Message, 10),
	}

	for i := 0; i < 10; i++ {
		tbl := lua.CreateTable(0, 2)
		tbl.RawSetString("index", lua.LNumber(float64(i)))
		tbl.RawSetString("data", lua.LString("value"))

		pkg.Messages[i] = &relay.Message{
			Topic:    "multi.bench",
			Payloads: []payload.Payload{payload.NewPayload(tbl, payload.Lua)},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := codec.Encode(pkg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = codec.Decode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Comparison benchmark
func BenchmarkLuaToGoConversion(b *testing.B) {
	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)
	tbl.RawSetString("ratio", lua.LNumber(3.14))

	arr := lua.CreateTable(5, 0)
	for i := 1; i <= 5; i++ {
		arr.RawSetInt(i, lua.LString("item"))
	}
	tbl.RawSetString("items", arr)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = luaToGo(tbl)
	}
}

func luaToGo(lv lua.LValue) any {
	switch v := lv.(type) {
	case *lua.LTable:
		isArray := true
		maxKey := 0
		v.ForEach(func(k, _ lua.LValue) {
			if n, ok := k.(lua.LNumber); ok {
				if int(n) > maxKey {
					maxKey = int(n)
				}
			} else {
				isArray = false
			}
		})
		if isArray && maxKey > 0 {
			arr := make([]any, maxKey)
			v.ForEach(func(k, val lua.LValue) {
				if n, ok := k.(lua.LNumber); ok {
					arr[int(n)-1] = luaToGo(val)
				}
			})
			return arr
		}
		m := make(map[string]any)
		v.ForEach(func(k, val lua.LValue) {
			m[k.String()] = luaToGo(val)
		})
		return m
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LBool:
		return bool(v)
	default:
		return nil
	}
}

// Test MsgPack transcoder integration
func TestMessageCodec_MsgPackFormat(t *testing.T) {
	dtt := createRealTranscoder()
	codec := NewMessageCodec(dtt)

	// Create Lua table
	tbl := lua.CreateTable(0, 2)
	tbl.RawSetString("key", lua.LString("value"))
	tbl.RawSetString("num", lua.LNumber(123))

	luaPayload := payload.NewPayload(tbl, payload.Lua)

	// Transcode Lua -> MsgPack directly using the transcoder
	msgpackPayload, err := dtt.Transcode(luaPayload, payload.MsgPack)
	if err != nil {
		t.Fatalf("Transcode Lua->MsgPack failed: %v", err)
	}

	if msgpackPayload.Format() != payload.MsgPack {
		t.Errorf("Format = %s, want %s", msgpackPayload.Format(), payload.MsgPack)
	}

	// Transcode back MsgPack -> Golang
	golangPayload, err := dtt.Transcode(msgpackPayload, payload.Golang)
	if err != nil {
		t.Fatalf("Transcode MsgPack->Golang failed: %v", err)
	}

	if golangPayload.Format() != payload.Golang {
		t.Errorf("Format = %s, want %s", golangPayload.Format(), payload.Golang)
	}

	// Now use the codec with MsgPack payload
	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: []*relay.Message{
			{
				Topic:    "msgpack.test",
				Payloads: []payload.Payload{msgpackPayload},
			},
		},
	}

	encoded, err := codec.Encode(pkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// MsgPack format should be preserved
	decodedPayload := decoded.Messages[0].Payloads[0]
	if decodedPayload.Format() != payload.MsgPack {
		t.Errorf("Decoded format = %s, want %s", decodedPayload.Format(), payload.MsgPack)
	}
}

// Test that JSON bytes are preserved
func TestMessageCodec_JSONBytesPreserved(t *testing.T) {
	codec := NewMessageCodec(createRealTranscoder())

	jsonBytes := []byte(`{"key":"value","num":123}`)
	pkg := &relay.Package{
		Source: pid.PID{Node: "node1", Host: "host1", UniqID: "src"},
		Messages: []*relay.Message{
			{
				Topic:    "json.test",
				Payloads: []payload.Payload{payload.NewPayload(jsonBytes, payload.JSON)},
			},
		},
	}

	encoded, err := codec.Encode(pkg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	decodedPayload := decoded.Messages[0].Payloads[0]
	if decodedPayload.Format() != payload.JSON {
		t.Errorf("Format = %s, want %s", decodedPayload.Format(), payload.JSON)
	}

	// Data should be bytes
	data, ok := decodedPayload.Data().([]byte)
	if !ok {
		t.Fatalf("Data type = %T, want []byte", decodedPayload.Data())
	}

	if !reflect.DeepEqual(data, jsonBytes) {
		t.Errorf("Data = %s, want %s", string(data), string(jsonBytes))
	}
}
