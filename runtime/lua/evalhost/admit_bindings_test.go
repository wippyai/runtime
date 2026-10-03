// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"testing"

	"github.com/stretchr/testify/require"
	apihost "github.com/wippyai/runtime/api/host"
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
