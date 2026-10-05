// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
	systempayload "github.com/wippyai/runtime/system/payload"
)

type printableMapKey string

func (k printableMapKey) String() string { return "key:" + string(k) }

type luaPayloadValue struct {
	payload.Payload
	lua.LValue
}

func TestFromGolang_ContainerCompatibility(t *testing.T) {
	type namedMap map[string]any
	type namedSlice []any
	type namedBytes []byte
	type record struct {
		Empty   map[string]any `json:"empty"`
		Hidden  string         `json:"hidden,omitempty"`
		private string
		List    []string `json:"list"`
		Nil     []string `json:"nil"`
	}
	tests := []struct {
		name  string
		input any
		json  string
	}{
		{"nil", nil, "null"},
		{"nil map", map[string]any(nil), "{}"},
		{"empty map", map[string]any{}, "{}"},
		{"nil slice", []any(nil), "null"},
		{"empty slice", []any{}, "[]"},
		{"typed map", map[string]int{"value": 42}, `{"value":42}`},
		{"integer keys", map[int]string{42: "value"}, `{"42":"value"}`},
		{"stringer keys", map[printableMapKey]string{"name": "value"}, `{"key:name":"value"}`},
		{"named map", namedMap{"value": true}, `{"value":true}`},
		{"named slice", namedSlice{"value", false}, `["value",false]`},
		{"array", [2]int{1, 2}, `[1,2]`},
		{"empty array", [0]int{}, `[]`},
		{"empty struct", struct{}{}, `{}`},
		{"bytes", []byte("hello"), `"hello"`},
		{"nil bytes", []byte(nil), `""`},
		{"named bytes remain a list", namedBytes{1, 2}, `[1,2]`},
		{"struct fields and tags", record{List: []string{}, private: "not visible"}, `{"empty":{},"hidden,omitempty":"","list":[]}`},
		{"nested shapes", map[string]any{"object": map[string]any{}, "list": []any{}, "nil": nil}, `{"list":[],"object":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := (&FromGolang{}).Transcode(payload.NewPayload(tt.input, payload.Golang))
			require.NoError(t, err)
			encoded, err := jsonlua.Encode(out.Data().(lua.LValue))
			require.NoError(t, err)
			require.JSONEq(t, tt.json, string(encoded))
		})
	}
}

func TestFromGolang_SpecialPointersAndErrorIdentity(t *testing.T) {
	at := time.Unix(12345, 0)
	wait := 1250 * time.Millisecond
	from := pid.PID{Node: "node", Host: "host", UniqID: "id"}
	details := attrs.NewBag()
	details.Set("field", "payload")
	cause := apierror.New(apierror.Unavailable, "original cause").WithRetryable(apierror.True).WithDetails(details)
	input := []any{&at, &wait, &from, cause, (*time.Time)(nil), (*time.Duration)(nil), (*pid.PID)(nil)}
	out, err := (&FromGolang{}).Transcode(payload.NewPayload(input, payload.Golang))
	require.NoError(t, err)
	table := out.Data().(*lua.LTable)
	require.Equal(t, lua.LNumber(at.Unix()), table.RawGetInt(1))
	require.Equal(t, lua.LInteger(wait), table.RawGetInt(2))
	require.Equal(t, lua.LString(from.String()), table.RawGetInt(3))
	require.ErrorIs(t, table.RawGetInt(4).(*lua.Error), cause)
	var structured apierror.Error
	require.ErrorAs(t, table.RawGetInt(4).(*lua.Error), &structured)
	require.Equal(t, apierror.Unavailable, structured.Kind())
	require.Equal(t, apierror.True, structured.Retryable())
	require.Equal(t, "payload", structured.Details().GetString("field", ""))
	for i := 5; i <= 7; i++ {
		require.Equal(t, lua.LNil, table.RawGetInt(i))
	}
}

func TestFromGolang_RecipientIsolation(t *testing.T) {
	source := map[string]any{"nested": map[string]any{"value": "original"}, "list": []any{"original"}}
	pl := payload.NewPayload(source, payload.Golang)
	first, err := (&FromGolang{}).Transcode(pl)
	require.NoError(t, err)
	second, err := (&FromGolang{}).Transcode(pl)
	require.NoError(t, err)
	root := first.Data().(*lua.LTable)
	root.RawGetString("nested").(*lua.LTable).RawSetString("value", lua.LString("changed"))
	root.RawGetString("list").(*lua.LTable).RawSetInt(1, lua.LString("changed"))
	other := second.Data().(*lua.LTable)
	require.Equal(t, lua.LString("original"), other.RawGetString("nested").(*lua.LTable).RawGetString("value"))
	require.Equal(t, lua.LString("original"), other.RawGetString("list").(*lua.LTable).RawGetInt(1))
	require.Equal(t, "original", source["nested"].(map[string]any)["value"])
	require.Equal(t, "original", source["list"].([]any)[0])
}

type failingNestedTranscoder struct {
	payload.Transcoder
	err error
}

func (t failingNestedTranscoder) Transcode(payload.Payload, payload.Format) (payload.Payload, error) {
	return nil, t.err
}

func TestFromGolang_NestedTranscodeErrorPassesThrough(t *testing.T) {
	cause := errors.New("nested format failed")
	tc := &payload.TranscodeContext{Parent: failingNestedTranscoder{err: cause}}
	input := map[string]any{"list": []any{payload.NewPayload("data", "custom")}}
	_, err := (&FromGolang{}).TranscodeWith(tc, payload.NewPayload(input, payload.Golang))
	require.Same(t, cause, err)
}

func TestFromGolang_ConversionErrors(t *testing.T) {
	tests := []struct {
		input   any
		message string
	}{
		{map[string]any{"value": ^uint64(0)}, "error converting map value for key value: unsupported value: uint64 overflows lua integer"},
		{[]any{make(chan int)}, "error converting slice/array element 0: unsupported type: chan int"},
		{struct {
			Value uint64 `json:"value"`
		}{^uint64(0)}, "error converting map value for key value: unsupported value: uint64 overflows lua integer"},
	}
	for _, tt := range tests {
		_, err := (&FromGolang{}).Transcode(payload.NewPayload(tt.input, payload.Golang))
		require.EqualError(t, err, tt.message)
	}
}

func TestFromGolang_NoIntermediateContainerAllocations(t *testing.T) {
	input := map[string]any{"list": []any{map[string]any{"name": "entry", "count": 42}}}
	pl := payload.NewPayload(input, payload.Golang)
	direct := testing.AllocsPerRun(100, func() {
		_, err := GoToLua(input)
		if err != nil {
			panic(err)
		}
	})
	transcoded := testing.AllocsPerRun(100, func() {
		_, err := (&FromGolang{}).Transcode(pl)
		if err != nil {
			panic(err)
		}
	})
	// The returned payload may allocate once; the container walk should not
	// allocate a second Go map/slice tree before constructing the Lua tables.
	require.LessOrEqual(t, transcoded, direct+1)
}

func BenchmarkFromGolang(b *testing.B) {
	entries := make([]any, 64)
	for i := range entries {
		entries[i] = map[string]any{"id": fmt.Sprintf("app:entry%d", i), "version": i, "meta": map[string]any{"kind": "function.lua", "tags": []any{"app", "service"}}}
	}
	type event struct {
		At      time.Time `json:"at"`
		From    pid.PID   `json:"from"`
		Entries []any     `json:"entries"`
	}
	cases := []struct {
		input any
		name  string
	}{
		{"value", "scalar"},
		{map[string]any{"id": "app:entry", "version": 1, "enabled": true}, "small_map"},
		{map[string]any{"entries": entries}, "registry_batch"},
		{event{At: time.Unix(12345, 0), From: pid.PID{Host: "host", UniqID: "id"}, Entries: entries}, "event"},
	}
	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			pl := payload.NewPayload(tt.input, payload.Golang)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := (&FromGolang{}).Transcode(pl); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFromGolang_NestedPayloadsAcrossContainers(t *testing.T) {
	tc := systempayload.NewTranscoder()
	Register(tc)
	input := []any{struct {
		Result payload.Payload `json:"result"`
	}{payload.NewPayload([]byte(`{"value":42}`), payload.JSON)}}
	out, err := tc.Transcode(payload.NewPayload(input, payload.Golang), payload.Lua)
	require.NoError(t, err)
	encoded, err := jsonlua.Encode(out.Data().(lua.LValue))
	require.NoError(t, err)
	require.JSONEq(t, `[{"result":{"value":42}}]`, string(encoded))
}

func TestFromGolang_PayloadTakesPrecedenceOverLuaValue(t *testing.T) {
	tc := systempayload.NewTranscoder()
	Register(tc)
	input := &luaPayloadValue{
		Payload: payload.NewPayload([]byte(`{"value":42}`), payload.JSON),
		LValue:  lua.LString("not the payload data"),
	}
	direct, err := GoToLua(input)
	require.NoError(t, err)
	require.Same(t, input, direct, "direct conversion keeps its Lua-value precedence")
	out, err := tc.Transcode(payload.NewPayload(map[string]any{"result": input}, payload.Golang), payload.Lua)
	require.NoError(t, err)
	encoded, err := jsonlua.Encode(out.Data().(lua.LValue))
	require.NoError(t, err)
	require.JSONEq(t, `{"result":{"value":42}}`, string(encoded))
}

func FuzzFromGolang_ContainerParity(f *testing.F) {
	for _, seed := range []string{`null`, `{}`, `[]`, `{"list":[{"value":42},null,{},[]],"flag":true}`, `{"text":"hello","number":1.25}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var input any
		if err := json.Unmarshal([]byte(text), &input); err != nil {
			t.Skip()
		}
		direct, err := GoToLua(input)
		require.NoError(t, err)
		out, err := (&FromGolang{}).Transcode(payload.NewPayload(input, payload.Golang))
		require.NoError(t, err)
		want, wantErr := jsonlua.Encode(direct)
		got, gotErr := jsonlua.Encode(out.Data().(lua.LValue))
		if wantErr != nil {
			require.EqualError(t, gotErr, wantErr.Error())
			return
		}
		require.NoError(t, gotErr)
		require.Equal(t, string(want), string(got))
	})
}
