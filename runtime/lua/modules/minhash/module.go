// SPDX-License-Identifier: MPL-2.0

package minhash

import (
	"encoding/hex"
	"fmt"
	"math"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// Module is the minhash module definition.
var Module = &luaapi.ModuleDef{
	Name:        "minhash",
	Description: "MinHash sketches of text, LSH band keys and Jaccard estimates",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 4)
	mod.RawSetString("sketch", lua.LGoFunc(luaSketch))
	mod.RawSetString("signature", lua.LGoFunc(luaSignature))
	mod.RawSetString("bands", lua.LGoFunc(luaBands))
	mod.RawSetString("estimate", lua.LGoFunc(luaEstimate))
	mod.Immutable = true
	return mod, nil
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

const maxExactLuaNumber = 1 << 53

var (
	sketchFields    = []string{"shingle", "k", "segment", "modulus", "base", "probe", "stride"}
	bandFields      = []string{"bands", "rows", "domain"}
	signatureFields = append(append([]string{}, sketchFields...), bandFields...)
)

// options reads the optional options table at index n, refusing fields outside allowed.
func options(l *lua.LState, n int, allowed []string) (*lua.LTable, error) {
	v := l.Get(n)
	if v == lua.LNil {
		return nil, nil
	}
	table, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("options must be a table")
	}
	var err error
	table.ForEach(func(key, _ lua.LValue) {
		if err != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			err = fmt.Errorf("options keys must be strings")
			return
		}
		for _, field := range allowed {
			if string(name) == field {
				return
			}
		}
		err = fmt.Errorf("unknown option: %s", string(name))
	})
	if err != nil {
		return nil, err
	}
	return table, nil
}

// integerOption reads a non-negative integer field, keeping target when absent.
func integerOption(table *lua.LTable, name string, target *uint64) error {
	if table == nil {
		return nil
	}
	switch v := table.RawGetString(name).(type) {
	case *lua.LNilType:
		return nil
	case lua.LInteger:
		if v < 0 {
			return fmt.Errorf("%s must be a non-negative integer", name)
		}
		*target = uint64(v)
		return nil
	case lua.LNumber:
		f := float64(v)
		if math.IsNaN(f) || math.Trunc(f) != f || f < 0 || f > maxExactLuaNumber {
			return fmt.Errorf("%s must be a non-negative integer", name)
		}
		*target = uint64(f)
		return nil
	default:
		return fmt.Errorf("%s must be a non-negative integer", name)
	}
}

func intOption(table *lua.LTable, name string, target *int) error {
	value := uint64(*target)
	if err := integerOption(table, name, &value); err != nil {
		return err
	}
	if value > math.MaxInt32 {
		return fmt.Errorf("%s is too large", name)
	}
	*target = int(value)
	return nil
}

func sketchParams(table *lua.LTable) (Params, error) {
	p := DefaultParams()
	for _, err := range []error{
		intOption(table, "shingle", &p.Shingle),
		intOption(table, "k", &p.K),
		intOption(table, "segment", &p.Segment),
		integerOption(table, "modulus", &p.Modulus),
		integerOption(table, "base", &p.Base),
		integerOption(table, "probe", &p.Probe),
		integerOption(table, "stride", &p.Stride),
	} {
		if err != nil {
			return p, err
		}
	}
	return p, p.Validate()
}

func lshParams(table *lua.LTable, size int) (LSH, error) {
	lsh := DefaultLSH()
	for _, err := range []error{
		intOption(table, "bands", &lsh.Bands),
		intOption(table, "rows", &lsh.Rows),
	} {
		if err != nil {
			return lsh, err
		}
	}
	if table != nil {
		switch v := table.RawGetString("domain").(type) {
		case *lua.LNilType:
		case lua.LString:
			lsh.Domain = string(v)
		default:
			return lsh, fmt.Errorf("domain must be a string")
		}
	}
	if lsh.Bands < 1 || lsh.Rows < 1 {
		return lsh, fmt.Errorf("bands and rows must be positive")
	}
	if lsh.Bands*lsh.Rows != size {
		return lsh, fmt.Errorf("bands*rows must equal the sketch size %d", size)
	}
	return lsh, nil
}

func keysTable(keys [][8]byte) *lua.LTable {
	out := lua.CreateTable(len(keys), 0)
	for _, key := range keys {
		out.Append(lua.LString(hex.EncodeToString(key[:])))
	}
	return out
}

func luaSketch(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "text must be a string")
	}
	table, err := options(l, 2, sketchFields)
	if err != nil {
		return invalidError(l, err.Error())
	}
	p, err := sketchParams(table)
	if err != nil {
		return invalidError(l, err.Error())
	}
	_, sketch, err := Sketch(l.ToString(1), p)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(lua.LString(Encode(sketch)))
	l.Push(lua.LNil)
	return 2
}

func luaSignature(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "text must be a string")
	}
	table, err := options(l, 2, signatureFields)
	if err != nil {
		return invalidError(l, err.Error())
	}
	p, err := sketchParams(table)
	if err != nil {
		return invalidError(l, err.Error())
	}
	lsh, err := lshParams(table, p.K)
	if err != nil {
		return invalidError(l, err.Error())
	}
	result, err := Signature(l.ToString(1), p)
	if err != nil {
		return invalidError(l, err.Error())
	}
	out := lua.CreateTable(0, 4)
	out.RawSetString("words", lua.LInteger(result.Words))
	out.RawSetString("normalized_sha256", lua.LString(hex.EncodeToString(result.Normalized[:])))
	if result.Sketch != nil {
		keys, err := lsh.Keys(result.Sketch)
		if err != nil {
			return invalidError(l, err.Error())
		}
		out.RawSetString("sketch", lua.LString(Encode(result.Sketch)))
		out.RawSetString("bands", keysTable(keys))
	}
	l.Push(out)
	l.Push(lua.LNil)
	return 2
}

func luaBands(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "sketch must be a string")
	}
	sketch, err := Decode([]byte(l.ToString(1)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	table, err := options(l, 2, bandFields)
	if err != nil {
		return invalidError(l, err.Error())
	}
	lsh, err := lshParams(table, len(sketch))
	if err != nil {
		return invalidError(l, err.Error())
	}
	keys, err := lsh.Keys(sketch)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(keysTable(keys))
	l.Push(lua.LNil)
	return 2
}

func luaEstimate(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		return invalidError(l, "sketches must be strings")
	}
	a, err := Decode([]byte(l.ToString(1)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	b, err := Decode([]byte(l.ToString(2)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	estimate, err := Estimate(a, b)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(lua.LNumber(estimate))
	l.Push(lua.LNil)
	return 2
}
