package hash

import (
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec // ok for now. G505: Blocklisted import crypto/md5: weak cryptographic primitive
	"crypto/sha1" //nolint:gosec // ok for now. G505: Blocklisted import crypto/sha1: weak cryptographic primitive
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
	mod.RawSetString("hmac_sha256", l.NewFunction(m.hmac_sha256))
	mod.RawSetString("hmac_sha512", l.NewFunction(m.hmac_sha512))
	mod.RawSetString("hmac_sha1", l.NewFunction(m.hmac_sha1))
	mod.RawSetString("hmac_md5", l.NewFunction(m.hmac_md5))

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

func computeHmacHash(h func() hash.Hash, data, secret string, raw bool) (lua.LValue, error) {
	hHmac := hmac.New(h, []byte(secret))
	_, err := hHmac.Write([]byte(data))
	if err != nil {
		return lua.LNil, err
	}

	result := hHmac.Sum(nil)
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
	result, err := computeHash(md5.New(), str, raw) //nolint:gosec // ok for now
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "md5"))
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
	result, err := computeHash(sha1.New(), str, raw) //nolint:gosec // ok for now
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "sha1"))
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
		l.Push(newHashOperationError(l, err, "sha256"))
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
		l.Push(newHashOperationError(l, err, "sha512"))
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
		l.Push(newHashOperationError(l, err, "fnv32"))
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
		l.Push(newHashOperationError(l, err, "fnv64"))
		return 2
	}
	l.Push(lua.LNumber(h.Sum64()))
	return 1
}

//nolint:revive // used in snakecase style
func (m *Module) hmac_sha256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTBool {
		raw = l.ToBool(3)
	}
	result, err := computeHmacHash(sha256.New, l.ToString(1), l.ToString(2), raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "hmac_sha256"))
		return 2
	}
	l.Push(result)

	return 1
}

//nolint:revive // used in snakecase style
func (m *Module) hmac_sha512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTBool {
		raw = l.ToBool(3)
	}

	result, err := computeHmacHash(sha512.New, l.ToString(1), l.ToString(2), raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "hmac_sha512"))
		return 2
	}
	l.Push(result)

	return 1
}

//nolint:revive // used in snakecase style
func (m *Module) hmac_sha1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTBool {
		raw = l.ToBool(3)
	}

	result, err := computeHmacHash(sha1.New, l.ToString(1), l.ToString(2), raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "hmac_sha1"))
		return 2
	}
	l.Push(result)

	return 1
}

//nolint:revive // used in snakecase style
func (m *Module) hmac_md5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}

	raw := false
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTBool {
		raw = l.ToBool(3)
	}

	result, err := computeHmacHash(md5.New, l.ToString(1), l.ToString(2), raw)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHashOperationError(l, err, "hmac_md5"))
		return 2
	}
	l.Push(result)

	return 1
}
