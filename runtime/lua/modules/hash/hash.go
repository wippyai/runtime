package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"hash/fnv"

	"github.com/ponyruntime/go-lua"
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
	mod := l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"md5":    m.md5,
		"sha1":   m.sha1,
		"sha256": m.sha256,
		"sha512": m.sha512,
		"fnv32":  m.fnv32,
		"fnv64":  m.fnv64,
	})
	l.Push(mod)
	return 1
}

func computeHash(h hash.Hash, data string) (string, error) {
	_, err := h.Write([]byte(data))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Module) md5(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.Push(lua.LString("d41d8cd98f00b204e9800998ecf8427e")) // MD5 of empty string
		return 1
	}

	result, err := computeHash(md5.New(), str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(result))
	return 1
}

func (m *Module) sha1(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.Push(lua.LString("da39a3ee5e6b4b0d3255bfef95601890afd80709")) // SHA1 of empty string
		return 1
	}

	result, err := computeHash(sha1.New(), str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(result))
	return 1
}

func (m *Module) sha256(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.Push(lua.LString("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")) // SHA256 of empty string
		return 1
	}

	result, err := computeHash(sha256.New(), str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(result))
	return 1
}

func (m *Module) sha512(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		// SHA512 of empty string
		l.Push(lua.LString("cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"))
		return 1
	}

	result, err := computeHash(sha512.New(), str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(result))
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
