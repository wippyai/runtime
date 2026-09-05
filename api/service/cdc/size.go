// SPDX-License-Identifier: MPL-2.0

package cdc

import "reflect"

const (
	// DefaultMaxStreamBytes bounds a subscriber's retained event backlog when
	// MaxBytes is omitted. It is deliberately finite for every driver.
	DefaultMaxStreamBytes int64 = 1 << 20
	// DefaultMaxStreamItems is the historical Lua CDC stream capacity. It is
	// also used by direct Go callers so process admission is always bounded.
	DefaultMaxStreamItems = 64
	changeStructuralBytes = 128
	valueStructuralBytes  = 24
	maxEstimateDepth      = 256
	maxEstimateNodes      = 1 << 20
)

// ValidateStreamOptions validates common stream resource limits. Buffer keeps
// its historical clamping behavior; MaxBytes is the only option with a
// rejected negative value.
func (o StreamOptions) Validate() error {
	if o.MaxBytes < 0 {
		return ErrInvalidMaxBytes
	}
	return nil
}

// EffectiveMaxBytes returns the finite subscriber backlog limit selected by
// the options. Zero means the safe common default.
func (o StreamOptions) EffectiveMaxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return DefaultMaxStreamBytes
}

// EffectiveMaxStreamItems returns the finite item limit selected by options.
func (o StreamOptions) EffectiveMaxStreamItems() int {
	if o.Buffer > 0 {
		return o.Buffer
	}
	return DefaultMaxStreamItems
}

// EstimateChangeBytes returns a conservative logical retained-size estimate
// for a Change and all nested values in its before/after images. It counts
// strings and byte blobs by length, includes container structure, saturates
// at MaxInt64, and terminates on cyclic pointers/maps/slices.
//
// The estimate is intentionally driver-neutral: both SQLite and PostgreSQL
// use this exact function for their sole subscriber backlog.
func EstimateChangeBytes(change Change) int64 {
	e := sizeEstimator{seen: make(map[sizeVisit]struct{})}
	return e.add(reflect.ValueOf(change), changeStructuralBytes)
}

type sizeVisit struct {
	typ  reflect.Type
	kind reflect.Kind
	ptr  uintptr
	len  int
	cap  int
}

type sizeEstimator struct {
	seen  map[sizeVisit]struct{}
	nodes int
}

func (e *sizeEstimator) add(value reflect.Value, total int64) int64 {
	return e.addDepth(value, total, 0)
}

func (e *sizeEstimator) addDepth(value reflect.Value, total int64, depth int) int64 {
	if total >= maxInt64Value {
		return maxInt64Value
	}
	if !value.IsValid() {
		return total
	}
	if depth > maxEstimateDepth || e.nodes >= maxEstimateNodes {
		return maxInt64Value
	}
	e.nodes++

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return total
		}
		return e.addDepth(value.Elem(), total, depth+1)
	case reflect.Pointer:
		if value.IsNil() {
			return total
		}
		if e.visited(value, 0, 0) {
			return total
		}
		return e.addDepth(value.Elem(), satAdd(total, valueStructuralBytes), depth+1)
	case reflect.Map:
		if value.IsNil() {
			return total
		}
		if e.visited(value, 0, 0) {
			return total
		}
		total = satAdd(total, satMul(valueStructuralBytes, int64(value.Len())))
		iter := value.MapRange()
		for iter.Next() {
			total = e.addDepth(iter.Key(), total, depth+1)
			total = e.addDepth(iter.Value(), total, depth+1)
		}
		return total
	case reflect.Slice:
		if value.IsNil() {
			return total
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return satAdd(total, int64(value.Len()))
		}
		if e.visited(value, value.Len(), value.Cap()) {
			return total
		}
		total = satAdd(total, satMul(valueStructuralBytes, int64(value.Len())))
		for i := 0; i < value.Len(); i++ {
			total = e.addDepth(value.Index(i), total, depth+1)
		}
		return total
	case reflect.Array:
		total = satAdd(total, satMul(valueStructuralBytes, int64(value.Len())))
		for i := 0; i < value.Len(); i++ {
			total = e.addDepth(value.Index(i), total, depth+1)
		}
		return total
	case reflect.Struct:
		total = satAdd(total, typeSize(value.Type()))
		for i := 0; i < value.NumField(); i++ {
			total = e.addDepth(value.Field(i), total, depth+1)
		}
		return total
	case reflect.String:
		return satAdd(total, int64(value.Len()))
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return satAdd(total, typeSize(value.Type()))
	default:
		return satAdd(total, typeSize(value.Type()))
	}
}

func (e *sizeEstimator) visited(value reflect.Value, length, capacity int) bool {
	ptr := value.Pointer()
	if ptr == 0 {
		return false
	}
	key := sizeVisit{typ: value.Type(), kind: value.Kind(), ptr: ptr, len: length, cap: capacity}
	if _, exists := e.seen[key]; exists {
		return true
	}
	e.seen[key] = struct{}{}
	return false
}

const maxInt64Value = int64(^uint64(0) >> 1)

func typeSize(typ reflect.Type) int64 {
	size := typ.Size()
	if uint64(size) > uint64(maxInt64Value) {
		return maxInt64Value
	}
	return int64(size)
}

func satAdd(a, b int64) int64 {
	if a >= maxInt64Value || b >= maxInt64Value-a {
		return maxInt64Value
	}
	return a + b
}

func satMul(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > maxInt64Value/b {
		return maxInt64Value
	}
	return a * b
}
