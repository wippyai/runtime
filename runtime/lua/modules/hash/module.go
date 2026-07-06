// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/fnv"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// Module is the hash module definition.
var Module = &luaapi.ModuleDef{
	Name:        "hash",
	Description: "Cryptographic hash functions, HMAC, and PBKDF2",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassSecurity, luaapi.ClassDeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 11)
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
	mod.RawSetString("pbkdf2", lua.LGoFunc(pbkdf2Derive))
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

const maxPBKDF2Iterations = 10_000_000

func derivePBKDF2(password, salt []byte, iterations, keyLength int, newHash func() hash.Hash) []byte {
	prf := hmac.New(newHash, password)
	hashLen := prf.Size()
	out := make([]byte, keyLength)
	u := make([]byte, 0, hashLen)
	block := make([]byte, hashLen)
	saltBlock := make([]byte, len(salt)+4)
	copy(saltBlock, salt)

	for blockIndex, offset := uint32(1), 0; offset < keyLength; blockIndex++ {
		binary.BigEndian.PutUint32(saltBlock[len(salt):], blockIndex)

		prf.Reset()
		_, _ = prf.Write(saltBlock)
		u = prf.Sum(u[:0])
		copy(block, u)

		for i := 1; i < iterations; i++ {
			prf.Reset()
			_, _ = prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range block {
				block[j] ^= u[j]
			}
		}

		offset += copy(out[offset:], block)
		clear(block)
	}

	clear(u)
	clear(saltBlock)
	return out
}

func pbkdf2Derive(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "password must be a string")
	}
	if l.Get(2).Type() != lua.LTString {
		return invalidError(l, "salt must be a string")
	}

	password := l.ToString(1)
	salt := l.ToString(2)
	iterations := l.CheckInt(3)
	keyLength := l.CheckInt(4)

	if password == "" {
		return invalidError(l, "password cannot be empty")
	}
	if salt == "" {
		return invalidError(l, "salt cannot be empty")
	}
	if iterations <= 0 {
		return invalidError(l, "iterations must be positive")
	}
	if iterations > maxPBKDF2Iterations {
		return invalidError(l, fmt.Sprintf("iterations exceeds maximum (%d)", maxPBKDF2Iterations))
	}
	if keyLength <= 0 {
		return invalidError(l, "key length must be positive")
	}

	var newHash func() hash.Hash
	algo := "sha256"
	if l.GetTop() >= 5 && l.Get(5).Type() == lua.LTString {
		algo = l.ToString(5)
	}
	switch algo {
	case "sha256":
		newHash = sha256.New
	case "sha512":
		newHash = sha512.New
	default:
		return invalidError(l, fmt.Sprintf("unsupported hash function: %s", algo))
	}

	key := derivePBKDF2([]byte(password), []byte(salt), iterations, keyLength, newHash)
	l.Push(lua.LString(key))
	l.Push(lua.LNil)
	clear(key)
	return 2
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
