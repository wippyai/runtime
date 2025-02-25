package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

// registerHMAC registers the HMAC submodule
func registerHMAC(l *lua.LState, mod *lua.LTable) {
	// Create HMAC submodule table
	hmacMod := l.NewTable()

	// Register functions
	l.SetField(hmacMod, "sha256", l.NewFunction(hmacSha256))
	l.SetField(hmacMod, "sha512", l.NewFunction(hmacSha512))

	// Add submodule to main module
	l.SetField(mod, "hmac", hmacMod)
}

// hmacSha256 calculates HMAC-SHA256
// Params:
//
//	key (string): HMAC key
//	data (string): Data to authenticate
//
// Returns: (string) HMAC digest or (nil, error_message) on failure
func hmacSha256(l *lua.LState) int {
	// Check parameters
	key := l.CheckString(1)
	data := l.CheckString(2)

	// Validate parameters
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key cannot be empty"))
		return 2
	}

	// Create HMAC-SHA256
	h := hmac.New(sha256.New, []byte(key))
	_, err := h.Write([]byte(data))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to compute HMAC: %v", err)))
		return 2
	}

	// Get digest and encode as hex
	digest := h.Sum(nil)
	hexDigest := hex.EncodeToString(digest)

	// Return the digest
	l.Push(lua.LString(hexDigest))
	l.Push(lua.LNil)
	return 2
}

// hmacSha512 calculates HMAC-SHA512
// Params:
//
//	key (string): HMAC key
//	data (string): Data to authenticate
//
// Returns: (string) HMAC digest or (nil, error_message) on failure
func hmacSha512(l *lua.LState) int {
	// Check parameters
	key := l.CheckString(1)
	data := l.CheckString(2)

	// Validate parameters
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key cannot be empty"))
		return 2
	}

	// Create HMAC-SHA512
	h := hmac.New(sha512.New, []byte(key))
	_, err := h.Write([]byte(data))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to compute HMAC: %v", err)))
		return 2
	}

	// Get digest and encode as hex
	digest := h.Sum(nil)
	hexDigest := hex.EncodeToString(digest)

	// Return the digest
	l.Push(lua.LString(hexDigest))
	l.Push(lua.LNil)
	return 2
}
