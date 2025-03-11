package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	lua "github.com/yuin/gopher-lua"
	"golang.org/x/crypto/chacha20poly1305"
)

// registerEncrypt registers the encrypt submodule
func registerEncrypt(l *lua.LState, mod *lua.LTable) {
	// Create encrypt submodule table with exact size
	encryptMod := l.CreateTable(0, 2) // aes, chacha20

	// Register functions using RawSetString
	encryptMod.RawSetString("aes", l.NewFunction(encryptAes))
	encryptMod.RawSetString("chacha20", l.NewFunction(encryptChacha20))

	// Add submodule to main module
	mod.RawSetString("encrypt", encryptMod)
}

// encryptAes encrypts data using AES-GCM (authenticated encryption)
// Params:
//
//	data (string): Data to encrypt
//	key (string): Encryption key (16, 24, or 32 bytes)
//	aad (string, optional): Additional authenticated data
//
// Returns: (string) Encrypted data (nonce prefixed) or (nil, error_message) on failure
func encryptAes(l *lua.LState) int {
	// Check parameters
	data := l.CheckString(1)
	key := l.CheckString(2)

	// Get optional AAD
	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	// Validate key length
	keyBytes := []byte(key)
	switch len(keyBytes) {
	case 16, 24, 32: // AES-128, AES-192, AES-256
		// Valid key length
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("key must be 16, 24, or 32 bytes"))
		return 2
	}

	// Create a new AES cipher
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create AES cipher: %v", err)))
		return 2
	}

	// Create a new GCM AEAD
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create GCM: %v", err)))
		return 2
	}

	// Create a nonce
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to generate nonce: %v", err)))
		return 2
	}

	// Encrypt and authenticate
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(data), aad)

	// Return the encrypted data (nonce + ciphertext)
	l.Push(lua.LString(string(ciphertext)))
	l.Push(lua.LNil)
	return 2
}

// encryptChacha20 encrypts data using ChaCha20-Poly1305 (authenticated encryption)
// Params:
//
//	data (string): Data to encrypt
//	key (string): Encryption key (32 bytes)
//	aad (string, optional): Additional authenticated data
//
// Returns: (string) Encrypted data (nonce prefixed) or (nil, error_message) on failure
func encryptChacha20(l *lua.LState) int {
	// Check parameters
	data := l.CheckString(1)
	key := l.CheckString(2)

	// Get optional AAD
	var aad []byte
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
		aad = []byte(l.ToString(3))
	}

	// Validate key length
	keyBytes := []byte(key)
	if len(keyBytes) != chacha20poly1305.KeySize {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("key must be %d bytes", chacha20poly1305.KeySize)))
		return 2
	}

	// Create a new ChaCha20-Poly1305 AEAD
	aead, err := chacha20poly1305.New(keyBytes)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create ChaCha20-Poly1305: %v", err)))
		return 2
	}

	// Create a nonce
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to generate nonce: %v", err)))
		return 2
	}

	// Encrypt and authenticate
	ciphertext := aead.Seal(nonce, nonce, []byte(data), aad)

	// Return the encrypted data (nonce + ciphertext)
	l.Push(lua.LString(string(ciphertext)))
	l.Push(lua.LNil)
	return 2
}
