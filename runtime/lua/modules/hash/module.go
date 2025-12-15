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

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module is the hash module definition.
var Module = &luaapi.ModuleDef{
	Name:        "hash",
	Description: "Cryptographic hash functions and HMAC",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassSecurity, luaapi.ClassDeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 10)
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

func computeHash(h hash.Hash, data string, raw bool) lua.LValue {
	h.Write([]byte(data))
	result := h.Sum(nil)
	if raw {
		return lua.LString(result)
	}
	return lua.LString(hex.EncodeToString(result))
}

func computeHmac(newHash func() hash.Hash, data, secret string, raw bool) lua.LValue {
	h := hmac.New(newHash, []byte(secret))
	h.Write([]byte(data))
	result := h.Sum(nil)
	if raw {
		return lua.LString(result)
	}
	return lua.LString(hex.EncodeToString(result))
}

func hashMD5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(md5.New(), l.ToString(1), raw)) //nolint:gosec
	l.Push(lua.LNil)
	return 2
}

func hashSHA1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha1.New(), l.ToString(1), raw)) //nolint:gosec
	l.Push(lua.LNil)
	return 2
}

func hashSHA256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha256.New(), l.ToString(1), raw))
	l.Push(lua.LNil)
	return 2
}

func hashSHA512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	l.Push(computeHash(sha512.New(), l.ToString(1), raw))
	l.Push(lua.LNil)
	return 2
}

func hashFNV32(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	h := fnv.New32()
	_, _ = h.Write([]byte(l.ToString(1)))
	l.Push(lua.LNumber(h.Sum32()))
	l.Push(lua.LNil)
	return 2
}

func hashFNV64(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	h := fnv.New64()
	_, _ = h.Write([]byte(l.ToString(1)))
	l.Push(lua.LNumber(h.Sum64()))
	l.Push(lua.LNil)
	return 2
}

func hmacSHA256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	if l.Get(2).Type() != lua.LTString {
		return invalidError(l, "secret must be a string")
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha256.New, l.ToString(1), l.ToString(2), raw))
	l.Push(lua.LNil)
	return 2
}

func hmacSHA512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	if l.Get(2).Type() != lua.LTString {
		return invalidError(l, "secret must be a string")
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha512.New, l.ToString(1), l.ToString(2), raw))
	l.Push(lua.LNil)
	return 2
}

func hmacSHA1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	if l.Get(2).Type() != lua.LTString {
		return invalidError(l, "secret must be a string")
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(sha1.New, l.ToString(1), l.ToString(2), raw))
	l.Push(lua.LNil)
	return 2
}

func hmacMD5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "data must be a string")
	}
	if l.Get(2).Type() != lua.LTString {
		return invalidError(l, "secret must be a string")
	}
	raw := l.GetTop() >= 3 && l.ToBool(3)
	l.Push(computeHmac(md5.New, l.ToString(1), l.ToString(2), raw))
	l.Push(lua.LNil)
	return 2
}
