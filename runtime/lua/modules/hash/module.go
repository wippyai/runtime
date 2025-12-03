package hash

import (
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"hash/fnv"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton hash module instance.
var Module = &hashModule{}

type hashModule struct{}

func (m *hashModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "hash",
		Description: "Cryptographic hash functions and HMAC",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassSecurity, luaapi.ClassDeterministic},
	}
}

func (m *hashModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}
		mod.RawSetString("md5", lua.LGoFunc(hashMD5))
		mod.RawSetString("sha1", lua.LGoFunc(hashSHA1))
		mod.RawSetString("sha256", lua.LGoFunc(hashSHA256))
		mod.RawSetString("sha512", lua.LGoFunc(hashSHA512))
		mod.RawSetString("fnv32", lua.LGoFunc(hashFNV32))
		mod.RawSetString("fnv64", lua.LGoFunc(hashFNV64))
		mod.RawSetString("hmac_sha256", lua.LGoFunc(hmacSHA256))
		mod.RawSetString("hmac_sha512", lua.LGoFunc(hmacSHA512))
		mod.RawSetString("hmac_sha1", lua.LGoFunc(hmacSHA1))
		mod.RawSetString("hmac_md5", lua.LGoFunc(hmacMD5))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *hashModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead. todo: clean up everywhere
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func computeHash(h hash.Hash, data string, raw bool) lua.LValue {
	h.Write([]byte(data))
	result := h.Sum(nil)
	if raw {
		return lua.LString(string(result))
	}
	return lua.LString(hex.EncodeToString(result))
}

func computeHmac(newHash func() hash.Hash, data, secret string, raw bool) lua.LValue {
	h := hmac.New(newHash, []byte(secret))
	h.Write([]byte(data))
	result := h.Sum(nil)
	if raw {
		return lua.LString(string(result))
	}
	return lua.LString(hex.EncodeToString(result))
}

func hashMD5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(md5.New(), l.ToString(1), raw)) //nolint:gosec
	return 1
}

func hashSHA1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha1.New(), l.ToString(1), raw)) //nolint:gosec
	return 1
}

func hashSHA256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha256.New(), l.ToString(1), raw))
	return 1
}

func hashSHA512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha512.New(), l.ToString(1), raw))
	return 1
}

func hashFNV32(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	h := fnv.New32()
	h.Write([]byte(l.ToString(1)))
	l.Push(lua.LNumber(h.Sum32()))
	return 1
}

func hashFNV64(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	h := fnv.New64()
	h.Write([]byte(l.ToString(1)))
	l.Push(lua.LNumber(h.Sum64()))
	return 1
}

func hmacSHA256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha256.New, l.ToString(1), l.ToString(2), raw))
	return 1
}

func hmacSHA512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha512.New, l.ToString(1), l.ToString(2), raw))
	return 1
}

func hmacSHA1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha1.New, l.ToString(1), l.ToString(2), raw))
	return 1
}

func hmacMD5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.ArgError(2, "string expected")
		return 0
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(md5.New, l.ToString(1), l.ToString(2), raw))
	return 1
}
