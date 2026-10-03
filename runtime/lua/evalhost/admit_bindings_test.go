// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"testing"

	"github.com/stretchr/testify/require"
	apihost "github.com/wippyai/runtime/api/host"
	payloadconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
)

func TestCopyBindingsDeepCopiesPlainData(t *testing.T) {
	inner := map[string]any{"k": "v"}
	list := []any{int64(1), inner}
	out, err := copyBindings([]apihost.EvalBinding{{Name: "cfg", Value: list}})
	require.NoError(t, err)

	inner["k"] = "changed"
	list[0] = int64(99)
	copied := out[0].Value.([]any)
	require.Equal(t, int64(1), copied[0], "admitted values do not follow caller mutation")
	require.Equal(t, "v", copied[1].(map[string]any)["k"])
}

func TestCopyBindingsRejectsUnsupportedValues(t *testing.T) {
	cases := map[string]any{
		"go array":      [1]int{1},
		"struct":        struct{ A int }{1},
		"func":          func() {},
		"typed slice":   []string{"a"},
		"pointer":       new(int),
		"huge unsigned": ^uint64(0),
	}
	for name, v := range cases {
		_, err := copyBindings([]apihost.EvalBinding{{Name: "x", Value: v}})
		require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid, name)
	}
}

func TestCopyBindingsBounded(t *testing.T) {
	var deep any = map[string]any{}
	for i := 0; i < MaxBindingDepth+1; i++ {
		deep = []any{deep}
	}
	_, err := copyBindings([]apihost.EvalBinding{{Name: "x", Value: deep}})
	require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid)

	wide := make([]any, MaxBindingNodes+1)
	for i := range wide {
		wide[i] = map[string]any{}
	}
	_, err = copyBindings([]apihost.EvalBinding{{Name: "x", Value: wide}})
	require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid)
}

func TestCopyBindingsNormalizesNumbers(t *testing.T) {
	out, err := copyBindings([]apihost.EvalBinding{{Name: "x", Value: []any{
		int8(-1), int16(2), int32(3), int(4), uint8(5), uint16(6), uint32(7), uint(8), uint64(9),
		float32(1.5), float64(2.5),
	}}})
	require.NoError(t, err)
	require.Equal(t, []any{
		int64(-1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7), int64(8), int64(9),
		float64(1.5), float64(2.5),
	}, out[0].Value, "integers become int64 and floats float64, the values a program receives")
	_, err = payloadconv.GoToLua(out[0].Value)
	require.NoError(t, err, "every admitted binding converts to Lua")
}

func TestCopyBindingsRejectsSharedTables(t *testing.T) {
	shared := map[string]any{"k": "v"}
	_, err := copyBindings([]apihost.EvalBinding{{Name: "x", Value: []any{shared, shared}}})
	require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid, "a map reachable twice would be copied twice")

	list := []any{"a"}
	_, err = copyBindings([]apihost.EvalBinding{{Name: "x", Value: map[string]any{"a": list, "b": list}}})
	require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid, "a list reachable twice would be copied twice")

	_, err = copyBindings([]apihost.EvalBinding{{Name: "a", Value: shared}, {Name: "b", Value: shared}})
	require.ErrorIs(t, err, apihost.ErrEvalBindingInvalid, "sharing across bindings counts too")

	_, err = copyBindings([]apihost.EvalBinding{{Name: "x", Value: []any{map[string]any{}, map[string]any{}, []any{}, []any{}}}})
	require.NoError(t, err, "distinct empty tables are not shared")
}

func TestHashPolicyDistinguishesNumberKinds(t *testing.T) {
	hashOf := func(v any) [32]byte {
		bindings, err := copyBindings([]apihost.EvalBinding{{Name: "n", Value: v}})
		require.NoError(t, err)
		h, err := hashPolicy(apihost.EvalPolicy{Bindings: bindings}, [32]byte{})
		require.NoError(t, err)
		return h
	}
	require.NotEqual(t, hashOf(int64(1)), hashOf(float64(1)), "math.type differs, so the programs differ")
	require.Equal(t, hashOf(int8(1)), hashOf(int64(1)), "integer widths are one Lua integer")
	require.Equal(t, hashOf(map[string]any{"a": int64(1), "b": "x"}), hashOf(map[string]any{"b": "x", "a": int64(1)}))
	require.NotEqual(t, hashOf([]any{"a", "b"}), hashOf([]any{"ab"}))
}
