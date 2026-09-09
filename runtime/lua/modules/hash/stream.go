// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"crypto/md5"  //nolint:gosec
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const hasherTypeName = "hash.Hasher"

// hasher accumulates input across calls so that data too large to hold in
// one string, such as a file read in chunks, digests the same as one-shot.
type hasher struct {
	algorithm string
	h         hash.Hash
}

var hasherMetatable *lua.LTable

func init() {
	hasherMetatable = value.RegisterTypeMethods(nil, hasherTypeName,
		map[string]lua.LGoFunc{"__tostring": hasherToString},
		map[string]lua.LGoFunc{
			"update": hasherUpdate,
			"sum":    hasherSum,
			"reset":  hasherReset,
		})
}

func newHashFunc(algorithm string) (func() hash.Hash, bool) {
	switch algorithm {
	case "md5":
		return md5.New, true //nolint:gosec
	case "sha1":
		return sha1.New, true //nolint:gosec
	case "sha256":
		return sha256.New, true
	case "sha512":
		return sha512.New, true
	default:
		return nil, false
	}
}

// hashNew creates a streaming hasher: hash.new(algorithm) -> Hasher, error
func hashNew(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "algorithm must be a string")
	}
	algorithm := l.ToString(1)
	constructor, ok := newHashFunc(algorithm)
	if !ok {
		return invalidError(l, fmt.Sprintf("unsupported hash function: %s", algorithm))
	}
	value.PushUserData(l, &hasher{algorithm: algorithm, h: constructor()}, hasherMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkHasher(l *lua.LState, n int) *hasher {
	ud := l.CheckUserData(n)
	h, ok := ud.Value.(*hasher)
	if !ok {
		l.ArgError(n, "hasher expected")
		return nil
	}
	return h
}

func hasherToString(l *lua.LState) int {
	h := checkHasher(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LString("hash.Hasher(" + h.algorithm + ")"))
	return 1
}

// hasherUpdate appends data: hasher:update(data) -> boolean, error
func hasherUpdate(l *lua.LState) int {
	h := checkHasher(l, 1)
	if h == nil {
		return 0
	}
	if l.Get(2).Type() != lua.LTString {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "data must be a string").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	h.h.Write([]byte(l.ToString(2)))
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// hasherSum returns the digest of everything appended so far without
// resetting the state: hasher:sum(raw?) -> string, error
func hasherSum(l *lua.LState) int {
	h := checkHasher(l, 1)
	if h == nil {
		return 0
	}
	raw := l.GetTop() >= 2 && l.ToBool(2)
	result := h.h.Sum(nil)
	if raw {
		l.Push(lua.LString(result))
	} else {
		l.Push(lua.LString(hex.EncodeToString(result)))
	}
	l.Push(lua.LNil)
	return 2
}

// hasherReset discards everything appended so far: hasher:reset()
func hasherReset(l *lua.LState) int {
	h := checkHasher(l, 1)
	if h == nil {
		return 0
	}
	h.h.Reset()
	return 0
}
