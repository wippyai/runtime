package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/google/uuid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/pbkdf2"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton crypto module instance.
var Module = &cryptoModule{}

type cryptoModule struct{}

func (m *cryptoModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "crypto",
		Description: "Cryptographic operations: encryption, random generation, key derivation",
		Class:       []string{luaapi.ClassSecurity, luaapi.ClassNondeterministic},
	}
}

func (m *cryptoModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}

		// Random submodule
		randomMod := &lua.LTable{}
		randomMod.RawSetString("bytes", lua.LGoFunc(randomBytes))
		randomMod.RawSetString("string", lua.LGoFunc(randomString))
		randomMod.RawSetString("uuid", lua.LGoFunc(randomUUID))
		randomMod.Immutable = true
		mod.RawSetString("random", randomMod)

		// Encrypt submodule
		encryptMod := &lua.LTable{}
		encryptMod.RawSetString("aes", lua.LGoFunc(encryptAES))
		encryptMod.RawSetString("chacha20", lua.LGoFunc(encryptChaCha20))
		encryptMod.Immutable = true
		mod.RawSetString("encrypt", encryptMod)

		// Decrypt submodule
		decryptMod := &lua.LTable{}
		decryptMod.RawSetString("aes", lua.LGoFunc(decryptAES))
		decryptMod.RawSetString("chacha20", lua.LGoFunc(decryptChaCha20))
		decryptMod.Immutable = true
		mod.RawSetString("decrypt", decryptMod)

		// Top-level functions
		mod.RawSetString("pbkdf2", lua.LGoFunc(pbkdf2Derive))
		mod.RawSetString("constant_time_compare", lua.LGoFunc(constantTimeCompare))

		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *cryptoModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func randomBytes(l *lua.LState) int {
	length := l.CheckInt(1)
	if length <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("length must be a positive integer"))
		return 2
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(string(buf)))
	l.Push(lua.LNil)
	return 2
}

func randomString(l *lua.LState) int {
	length := l.CheckInt(1)
	if length <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("length must be a positive integer"))
		return 2
	}

	charset := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTString {
		charset = l.ToString(2)
		if len(charset) == 0 {
			l.Push(lua.LNil)
			l.Push(lua.LString("charset cannot be empty"))
			return 2
		}
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	charsetLen := len(charset)
	result := make([]byte, length)
	for i, b := range buf {
		result[i] = charset[int(b)%charsetLen]
	}

	l.Push(lua.LString(string(result)))
	l.Push(lua.LNil)
	return 2
}

func randomUUID(l *lua.LState) int {
	id, err := uuid.NewRandom()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func encryptAES(l *lua.LState) int {
	data := l.CheckString(1)
	key := l.CheckString(2)

	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	keyBytes := []byte(key)
	switch len(keyBytes) {
	case 16, 24, 32:
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("key must be 16, 24, or 32 bytes"))
		return 2
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create AES cipher: %v", err)))
		return 2
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create GCM: %v", err)))
		return 2
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to generate nonce: %v", err)))
		return 2
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(data), aad)
	l.Push(lua.LString(string(ciphertext)))
	l.Push(lua.LNil)
	return 2
}

func decryptAES(l *lua.LState) int {
	data := l.CheckString(1)
	key := l.CheckString(2)

	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	keyBytes := []byte(key)
	switch len(keyBytes) {
	case 16, 24, 32:
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("key must be 16, 24, or 32 bytes"))
		return 2
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create AES cipher: %v", err)))
		return 2
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create GCM: %v", err)))
		return 2
	}

	dataBytes := []byte(data)
	nonceSize := aesGCM.NonceSize()
	if len(dataBytes) < nonceSize {
		l.Push(lua.LNil)
		l.Push(lua.LString("ciphertext too short"))
		return 2
	}

	nonce, ciphertext := dataBytes[:nonceSize], dataBytes[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("decryption failed: %v", err)))
		return 2
	}

	l.Push(lua.LString(string(plaintext)))
	l.Push(lua.LNil)
	return 2
}

func encryptChaCha20(l *lua.LState) int {
	data := l.CheckString(1)
	key := l.CheckString(2)

	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	keyBytes := []byte(key)
	if len(keyBytes) != chacha20poly1305.KeySize {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("key must be %d bytes", chacha20poly1305.KeySize)))
		return 2
	}

	aead, err := chacha20poly1305.New(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create ChaCha20-Poly1305: %v", err)))
		return 2
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to generate nonce: %v", err)))
		return 2
	}

	ciphertext := aead.Seal(nonce, nonce, []byte(data), aad)
	l.Push(lua.LString(string(ciphertext)))
	l.Push(lua.LNil)
	return 2
}

func decryptChaCha20(l *lua.LState) int {
	data := l.CheckString(1)
	key := l.CheckString(2)

	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	keyBytes := []byte(key)
	if len(keyBytes) != chacha20poly1305.KeySize {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("key must be %d bytes", chacha20poly1305.KeySize)))
		return 2
	}

	aead, err := chacha20poly1305.New(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create ChaCha20-Poly1305: %v", err)))
		return 2
	}

	dataBytes := []byte(data)
	nonceSize := aead.NonceSize()
	if len(dataBytes) < nonceSize {
		l.Push(lua.LNil)
		l.Push(lua.LString("ciphertext too short"))
		return 2
	}

	nonce, ciphertext := dataBytes[:nonceSize], dataBytes[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("decryption failed: %v", err)))
		return 2
	}

	l.Push(lua.LString(string(plaintext)))
	l.Push(lua.LNil)
	return 2
}

func pbkdf2Derive(l *lua.LState) int {
	password := l.CheckString(1)
	salt := l.CheckString(2)
	iterations := l.CheckInt(3)
	keyLength := l.CheckInt(4)

	if password == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("password cannot be empty"))
		return 2
	}

	if salt == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("salt cannot be empty"))
		return 2
	}

	if iterations <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("iterations must be positive"))
		return 2
	}

	if keyLength <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("key length must be positive"))
		return 2
	}

	var hashFunc func() hash.Hash
	hashName := "sha256"
	if l.GetTop() >= 5 && l.Get(5).Type() == lua.LTString {
		hashName = l.ToString(5)
	}

	switch hashName {
	case "sha256":
		hashFunc = sha256.New
	case "sha512":
		hashFunc = sha512.New
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported hash function: %s", hashName)))
		return 2
	}

	derivedKey := pbkdf2.Key([]byte(password), []byte(salt), iterations, keyLength, hashFunc)
	l.Push(lua.LString(string(derivedKey)))
	l.Push(lua.LNil)
	return 2
}

func constantTimeCompare(l *lua.LState) int {
	a := l.CheckString(1)
	b := l.CheckString(2)

	result := subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
	l.Push(lua.LBool(result))
	return 1
}
