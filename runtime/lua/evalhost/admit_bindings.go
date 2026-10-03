// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"fmt"
	"math"
	"reflect"
	"unsafe"

	apihost "github.com/wippyai/runtime/api/host"
)

// Binding data limits. A binding is a tree of plain data: every table
// counts toward the node limit and no table is reachable twice, so
// validation, hashing and copying stay linear in the admitted value.
const (
	MaxBindingDepth = 32
	MaxBindingNodes = 4096
)

// copyBindings validates bindings and deep-copies their values into the
// values a program receives: nil, booleans, strings, int64, float64, []any
// and map[string]any. A caller cannot change an admitted program afterwards.
func copyBindings(bindings []apihost.EvalBinding) ([]apihost.EvalBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	out := make([]apihost.EvalBinding, len(bindings))
	w := bindingWalker{seen: make(map[uintptr]struct{})}
	for i, b := range bindings {
		v, err := w.copy(b.Value, b.Name, 0)
		if err != nil {
			return nil, err
		}
		out[i] = apihost.EvalBinding{Name: b.Name, Value: v}
	}
	return out, nil
}

type bindingWalker struct {
	seen  map[uintptr]struct{}
	nodes int
}

func (w *bindingWalker) copy(v any, path string, depth int) (any, error) {
	switch x := v.(type) {
	case nil, bool, string, int64, float64:
		return x, nil
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, bindingError(path, "integer out of range")
		}
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return nil, bindingError(path, "integer out of range")
		}
		return int64(x), nil
	case float32:
		return float64(x), nil
	case []any:
		if err := w.enter(path, depth, unsafe.Pointer(unsafe.SliceData(x)), len(x)); err != nil {
			return nil, err
		}
		out := make([]any, len(x))
		for i, item := range x {
			c, err := w.copy(item, fmt.Sprintf("%s[%d]", path, i+1), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		if err := w.enter(path, depth, reflect.ValueOf(x).UnsafePointer(), len(x)); err != nil {
			return nil, err
		}
		out := make(map[string]any, len(x))
		for k, item := range x {
			c, err := w.copy(item, path+"."+k, depth+1)
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

// enter admits one table. A table with storage is identified by it and may
// appear once; an empty one has nothing to duplicate.
func (w *bindingWalker) enter(path string, depth int, storage unsafe.Pointer, size int) error {
	if depth >= MaxBindingDepth {
		return bindingError(path, fmt.Sprintf("nests deeper than %d", MaxBindingDepth))
	}
	w.nodes++
	if w.nodes > MaxBindingNodes {
		return bindingError(path, fmt.Sprintf("bindings exceed %d tables", MaxBindingNodes))
	}
	if storage == nil || size == 0 {
		return nil
	}
	if _, ok := w.seen[uintptr(storage)]; ok {
		return bindingError(path, "is a table that appears more than once")
	}
	w.seen[uintptr(storage)] = struct{}{}
	return nil
}

func bindingError(path, reason string) error {
	return fmt.Errorf("%w: %s %s", apihost.ErrEvalBindingInvalid, path, reason)
}
