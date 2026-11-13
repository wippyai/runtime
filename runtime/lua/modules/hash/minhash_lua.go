// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const minHasherTypeName = "hash.MinHasher"

var minHasherMetatable *lua.LTable

func init() {
	minHasherMetatable = value.RegisterTypeMethods(nil, minHasherTypeName,
		map[string]lua.LGoFunc{"__tostring": minHasherToString},
		map[string]lua.LGoFunc{
			"update": minHasherUpdate,
			"sum":    minHasherSum,
			"reset":  minHasherReset,
		})
}

func minhashTable() *lua.LTable {
	t := lua.CreateTable(0, 5)
	t.RawSetString("signature", lua.LGoFunc(minhashSignature))
	t.RawSetString("sketch", lua.LGoFunc(minhashSketch))
	t.RawSetString("bands", lua.LGoFunc(minhashBands))
	t.RawSetString("similarity", lua.LGoFunc(minhashSimilarity))
	t.RawSetString("new", lua.LGoFunc(minhashNew))
	t.Immutable = true
	return t
}

var (
	minhashSketchOptions    = []string{"k", "shingle"}
	minhashBandOptions      = []string{"bands", "rows", "domain"}
	minhashSignatureOptions = []string{"k", "shingle", "bands", "rows", "domain"}
)

// minhashOptions reads the optional options table at index n into c, refusing
// fields outside allowed.
func minhashOptions(l *lua.LState, n int, allowed []string, c *MinHashConfig) error {
	v := l.Get(n)
	if v == lua.LNil {
		return nil
	}
	table, ok := v.(*lua.LTable)
	if !ok {
		return errors.New("options must be a table")
	}
	var err error
	table.ForEach(func(key, val lua.LValue) {
		if err != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			err = errors.New("option names must be strings")
			return
		}
		known := false
		for _, field := range allowed {
			known = known || string(name) == field
		}
		if !known {
			err = fmt.Errorf("unknown option: %s", string(name))
			return
		}
		switch name {
		case "k":
			err = integerOption("k", val, &c.K)
		case "shingle":
			err = integerOption("shingle", val, &c.Shingle)
		case "bands":
			err = integerOption("bands", val, &c.Bands)
		case "rows":
			err = integerOption("rows", val, &c.Rows)
		case "domain":
			s, ok := val.(lua.LString)
			if !ok {
				err = errors.New("domain must be a string")
				return
			}
			c.Domain = string(s)
		}
	})
	return err
}

// integerOption reads an integer option in 0..2^31-1; a float counts when it is integral.
func integerOption(name string, v lua.LValue, target *int) error {
	var n int64
	switch v := v.(type) {
	case lua.LInteger:
		n = int64(v)
	case lua.LNumber:
		f := float64(v)
		if math.Trunc(f) != f || f < 0 || f > math.MaxInt32 {
			return fmt.Errorf("%s must be an integer in 0..%d", name, math.MaxInt32)
		}
		n = int64(f)
	default:
		return fmt.Errorf("%s must be an integer", name)
	}
	if n < 0 || n > math.MaxInt32 {
		return fmt.Errorf("%s must be an integer in 0..%d", name, math.MaxInt32)
	}
	*target = int(n)
	return nil
}

// writeInput feeds the input at index n: a string through the default
// tokenizer, or an array of non-empty token strings as given.
func writeInput(l *lua.LState, n int, m *MinHasher) error {
	switch v := l.Get(n).(type) {
	case lua.LString:
		m.Write(string(v))
		return nil
	case *lua.LTable:
		size := v.Len()
		entries := 0
		v.ForEach(func(_, _ lua.LValue) { entries++ })
		if entries != size {
			return errors.New("tokens must be an array of strings")
		}
		for i := 1; i <= size; i++ {
			token, ok := v.RawGetInt(i).(lua.LString)
			if !ok || token == "" {
				return errors.New("tokens must be an array of non-empty strings")
			}
			m.WriteToken(string(token))
		}
		return nil
	default:
		return errors.New("input must be a string or an array of token strings")
	}
}

func keysTable(keys [][8]byte) *lua.LTable {
	out := lua.CreateTable(len(keys), 0)
	for _, key := range keys {
		out.Append(lua.LString(hex.EncodeToString(key[:])))
	}
	return out
}

func signatureTable(r MinHashResult, c MinHashConfig) (*lua.LTable, error) {
	out := lua.CreateTable(0, 4)
	out.RawSetString("tokens", lua.LInteger(r.Tokens))
	out.RawSetString("normalized", lua.LString(hex.EncodeToString(r.Normalized[:])))
	if r.Sketch != nil {
		keys, err := MinHashBands(r.Sketch, c)
		if err != nil {
			return nil, err
		}
		out.RawSetString("sketch", lua.LString(EncodeMinHashSketch(r.Sketch)))
		out.RawSetString("bands", keysTable(keys))
	}
	return out, nil
}

// minhashSignature: hash.minhash.signature(input, options?) -> Signature, error
func minhashSignature(l *lua.LState) int {
	c := DefaultMinHashConfig()
	if err := minhashOptions(l, 2, minhashSignatureOptions, &c); err != nil {
		return invalidError(l, err.Error())
	}
	if err := c.Validate(); err != nil {
		return invalidError(l, err.Error())
	}
	m, err := NewMinHasher(c, true)
	if err != nil {
		return invalidError(l, err.Error())
	}
	if err := writeInput(l, 1, m); err != nil {
		return invalidError(l, err.Error())
	}
	out, err := signatureTable(m.Sum(), c)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(out)
	l.Push(lua.LNil)
	return 2
}

// minhashSketch: hash.minhash.sketch(input, options?) -> string, error
func minhashSketch(l *lua.LState) int {
	c := DefaultMinHashConfig()
	if err := minhashOptions(l, 2, minhashSketchOptions, &c); err != nil {
		return invalidError(l, err.Error())
	}
	m, err := NewMinHasher(c, false)
	if err != nil {
		return invalidError(l, err.Error())
	}
	if err := writeInput(l, 1, m); err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(lua.LString(EncodeMinHashSketch(m.Sum().Sketch)))
	l.Push(lua.LNil)
	return 2
}

// minhashBands: hash.minhash.bands(sketch, options?) -> {string}, error
func minhashBands(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "sketch must be a string")
	}
	sketch, err := DecodeMinHashSketch([]byte(l.ToString(1)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	c := DefaultMinHashConfig()
	if err := minhashOptions(l, 2, minhashBandOptions, &c); err != nil {
		return invalidError(l, err.Error())
	}
	keys, err := MinHashBands(sketch, c)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(keysTable(keys))
	l.Push(lua.LNil)
	return 2
}

// minhashSimilarity: hash.minhash.similarity(a, b) -> number, error
func minhashSimilarity(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		return invalidError(l, "sketches must be strings")
	}
	a, err := DecodeMinHashSketch([]byte(l.ToString(1)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	b, err := DecodeMinHashSketch([]byte(l.ToString(2)))
	if err != nil {
		return invalidError(l, err.Error())
	}
	j, err := MinHashSimilarity(a, b)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(lua.LNumber(j))
	l.Push(lua.LNil)
	return 2
}

type minHasher struct {
	m      *MinHasher
	config MinHashConfig
}

// minhashNew: hash.minhash.new(options?) -> MinHasher, error
func minhashNew(l *lua.LState) int {
	c := DefaultMinHashConfig()
	if err := minhashOptions(l, 1, minhashSignatureOptions, &c); err != nil {
		return invalidError(l, err.Error())
	}
	if err := c.Validate(); err != nil {
		return invalidError(l, err.Error())
	}
	m, err := NewMinHasher(c, true)
	if err != nil {
		return invalidError(l, err.Error())
	}
	value.PushUserData(l, &minHasher{m: m, config: c}, minHasherMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkMinHasher(l *lua.LState, n int) *minHasher {
	ud := l.CheckUserData(n)
	h, ok := ud.Value.(*minHasher)
	if !ok {
		l.ArgError(n, "minhasher expected")
		return nil
	}
	return h
}

func minHasherToString(l *lua.LState) int {
	h := checkMinHasher(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("hash.MinHasher(k=%d, shingle=%d)", h.config.K, h.config.Shingle)))
	return 1
}

// minHasherUpdate appends text: minhasher:update(chunk) -> boolean, error
func minHasherUpdate(l *lua.LState) int {
	h := checkMinHasher(l, 1)
	if h == nil {
		return 0
	}
	chunk, ok := l.Get(2).(lua.LString)
	if !ok {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "chunk must be a string").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	h.m.Write(string(chunk))
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// minHasherSum answers the signature of everything appended so far and keeps
// the state: minhasher:sum() -> Signature, error
func minHasherSum(l *lua.LState) int {
	h := checkMinHasher(l, 1)
	if h == nil {
		return 0
	}
	out, err := signatureTable(h.m.Sum(), h.config)
	if err != nil {
		return invalidError(l, err.Error())
	}
	l.Push(out)
	l.Push(lua.LNil)
	return 2
}

// minHasherReset discards everything appended so far: minhasher:reset()
func minHasherReset(l *lua.LState) int {
	h := checkMinHasher(l, 1)
	if h == nil {
		return 0
	}
	h.m.Reset()
	return 0
}
