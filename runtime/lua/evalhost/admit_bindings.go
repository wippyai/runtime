// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"fmt"
	"math"

	apihost "github.com/wippyai/runtime/api/host"
)

// Binding data limits. A binding is a tree of plain data: every table
// counts toward the node limit, so validation and copying stay linear in
// the admitted value.
const (
	MaxBindingDepth = 32
	MaxBindingNodes = 4096
)

// copyBindings validates bindings and deep-copies their values, so a caller
// cannot change an admitted program afterwards.
func copyBindings(bindings []apihost.EvalBinding) ([]apihost.EvalBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	out := make([]apihost.EvalBinding, len(bindings))
	nodes := 0
	for i, b := range bindings {
		v, err := copyBindingValue(b.Value, b.Name, 0, &nodes)
		if err != nil {
			return nil, err
		}
		out[i] = apihost.EvalBinding{Name: b.Name, Value: v}
	}
	return out, nil
}

// copyBindingValue copies nil, booleans, numbers, strings, []any and
// map[string]any trees.
func copyBindingValue(v any, path string, depth int, nodes *int) (any, error) {
	switch x := v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint8, uint16, uint32:
		return x, nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, bindingError(path, "integer out of range")
		}
		return x, nil
	case uint64:
		if x > math.MaxInt64 {
			return nil, bindingError(path, "integer out of range")
		}
		return x, nil
	case float32:
		return x, nil
	case float64:
		return x, nil
	case []any:
		if err := enterBindingNode(path, depth, nodes); err != nil {
			return nil, err
		}
		out := make([]any, len(x))
		for i, item := range x {
			c, err := copyBindingValue(item, fmt.Sprintf("%s[%d]", path, i+1), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		if err := enterBindingNode(path, depth, nodes); err != nil {
			return nil, err
		}
		out := make(map[string]any, len(x))
		for k, item := range x {
			c, err := copyBindingValue(item, path+"."+k, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	default:
		return nil, bindingError(path, fmt.Sprintf("unsupported %T value", v))
	}
}

func enterBindingNode(path string, depth int, nodes *int) error {
	if depth >= MaxBindingDepth {
		return bindingError(path, fmt.Sprintf("nests deeper than %d", MaxBindingDepth))
	}
	*nodes++
	if *nodes > MaxBindingNodes {
		return bindingError(path, fmt.Sprintf("bindings exceed %d tables", MaxBindingNodes))
	}
	return nil
}

func bindingError(path, reason string) error {
	return fmt.Errorf("%w: %s %s", apihost.ErrEvalBindingInvalid, path, reason)
}
