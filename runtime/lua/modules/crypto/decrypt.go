package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/crypto/chacha20poly1305"
)

// registerDecrypt registers the decrypt submodule
func registerDecrypt(l *lua.LState, mod *lua.LTable) {
	// Create decrypt submodule table
	decryptMod := l.NewTable()

	// Register functions
	l.SetField(decryptMod, "aes", l.NewFunction(decryptAes))
	l.SetField(decryptMod, "chacha20", l.NewFunction(decryptChacha20))

	// Add submodule to main module
	l.SetField(mod, "decrypt", decryptMod)
}

// decryptAes decrypts data using AES-GCM
// Params:
//
//	data (string): Encrypted data (with nonce prefixed)
//	key (string): Decryption key (16, 24, or 32 bytes)
//	aad (string, optional): Additional authenticated data
//
// Returns: (string) Decrypted data or (nil, error_message) on failure
func decryptAes(l *lua.LState) int {
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

	// Extract the nonce and ciphertext
	encryptedBytes := []byte(data)
	nonceSize := aesGCM.NonceSize()

	if len(encryptedBytes) < nonceSize {
		l.Push(lua.LNil)
		l.Push(lua.LString("encrypted data too short"))
		return 2
	}

	nonce := encryptedBytes[:nonceSize]
	ciphertext := encryptedBytes[nonceSize:]

	// Decrypt and verify
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to decrypt: %v", err)))
		return 2
	}

	// Return the decrypted data
	l.Push(lua.LString(string(plaintext)))
	l.Push(lua.LNil)
	return 2
}

// decryptChacha20 decrypts data using ChaCha20-Poly1305
// Params:
//
//	data (string): Encrypted data (with nonce prefixed)
//	key (string): Decryption key (32 bytes)
//	aad (string, optional): Additional authenticated data
//
// Returns: (string) Decrypted data or (nil, error_message) on failure
func decryptChacha20(l *lua.LState) int {
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

	// Extract the nonce and ciphertext
	encryptedBytes := []byte(data)
	nonceSize := aead.NonceSize()

	if len(encryptedBytes) < nonceSize {
		l.Push(lua.LNil)
		l.Push(lua.LString("encrypted data too short"))
		return 2
	}

	nonce := encryptedBytes[:nonceSize]
	ciphertext := encryptedBytes[nonceSize:]

	// Decrypt and verify
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to decrypt: %v", err)))
		return 2
	}

	// Return the decrypted data
	l.Push(lua.LString(string(plaintext)))
	l.Push(lua.LNil)
	return 2
}
