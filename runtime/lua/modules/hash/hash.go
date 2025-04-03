package hash

import (
	"crypto/md5"  //nolint:gosec
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"hash/fnv"

	lua "github.com/yuin/gopher-lua"
)

// Module represents a hash Lua module.
type Module struct{}

// NewHashModule creates and returns a new instance of the hash Module.
func NewHashModule() *Module {
	return &Module{}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "hash"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	mod := l.CreateTable(0, 6)

	// Register functions directly using RawSetString for better performance
	mod.RawSetString("md5", l.NewFunction(m.md5))
	mod.RawSetString("sha1", l.NewFunction(m.sha1))
	mod.RawSetString("sha256", l.NewFunction(m.sha256))
	mod.RawSetString("sha512", l.NewFunction(m.sha512))
	mod.RawSetString("fnv32", l.NewFunction(m.fnv32))
	mod.RawSetString("fnv64", l.NewFunction(m.fnv64))

	l.Push(mod)
	return 1
}

// computeHash computes hash and returns result based on raw flag
func computeHash(h hash.Hash, data string, raw bool) (lua.LValue, error) {
	_, err := h.Write([]byte(data))
	if err != nil {
		return lua.LNil, err
	}

	result := h.Sum(nil)
	if raw {
		return lua.LString(string(result)), nil
	}
	return lua.LString(hex.EncodeToString(result)), nil
}

func (m *Module) md5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTBool {
		raw = l.ToBool(2)
	}

	str := l.ToString(1)
	result, err := computeHash(md5.New(), str, raw) //nolint:gosec
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(result)
	return 1
}

func (m *Module) sha1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTBool {
		raw = l.ToBool(2)
	}

	str := l.ToString(1)
	result, err := computeHash(sha1.New(), str, raw) //nolint:gosec
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(result)
	return 1
}

func (m *Module) sha256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTBool {
		raw = l.ToBool(2)
	}

	str := l.ToString(1)
	result, err := computeHash(sha256.New(), str, raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(result)
	return 1
}

func (m *Module) sha512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTBool {
		raw = l.ToBool(2)
	}

	str := l.ToString(1)
	result, err := computeHash(sha512.New(), str, raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(result)
	return 1
}

func (m *Module) fnv32(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	h := fnv.New32()
	if _, err := h.Write([]byte(str)); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LNumber(h.Sum32()))
	return 1
}

func (m *Module) fnv64(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	h := fnv.New64()
	if _, err := h.Write([]byte(str)); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LNumber(h.Sum64()))
	return 1
}
