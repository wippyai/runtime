package crypto

import (
	"crypto/rand"
	"fmt"

	"github.com/google/uuid"
	lua "github.com/yuin/gopher-lua"
)

// registerRandom registers the random submodule
func registerRandom(l *lua.LState, mod *lua.LTable) {
	// Create random submodule table with exact size
	randomMod := l.CreateTable(0, 3) // bytes, string, uuid

	// Register functions using RawSetString for better performance
	randomMod.RawSetString("bytes", l.NewFunction(randomBytes))
	randomMod.RawSetString("string", l.NewFunction(randomString))
	randomMod.RawSetString("uuid", l.NewFunction(randomUUID))

	// Add submodule to main module
	mod.RawSetString("random", randomMod)
}

// randomBytes generates cryptographically secure random bytes
// Params: length (number) - Number of random bytes to generate
// Returns: (string) Random bytes or (nil, error_message) on failure
func randomBytes(l *lua.LState) int {
	// Validate parameter
	length := l.CheckInt(1)
	if length <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("length must be a positive integer"))
		return 2
	}

	// Create a buffer to hold the random bytes
	buf := make([]byte, length)

	// Generate random bytes using crypto/rand
	_, err := rand.Read(buf)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Return the random bytes as a string
	l.Push(lua.LString(string(buf)))
	l.Push(lua.LNil)
	return 2
}

// randomString generates a random string using a specified character set
// Params:
//
//	length (number): Length of the string
//	charset (string, optional): Characters to use (default: alphanumeric)
//
// Returns: (string) Random string or (nil, error_message) on failure
func randomString(l *lua.LState) int {
	// Validate length parameter
	length := l.CheckInt(1)
	if length <= 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("length must be a positive integer"))
		return 2
	}

	// Get optional charset parameter, default to alphanumeric
	var charset string
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTString {
		charset = l.ToString(2)
		if len(charset) == 0 {
			l.Push(lua.LNil)
			l.Push(lua.LString("charset cannot be empty"))
			return 2
		}
	} else {
		// Default charset: alphanumeric
		charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	}

	// Create a buffer to hold the random bytes (1 byte per character)
	buf := make([]byte, length)

	// Generate random bytes
	_, err := rand.Read(buf)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert random bytes to characters from the charset
	charsetLength := len(charset)
	result := make([]byte, length)
	for i, b := range buf {
		result[i] = charset[int(b)%charsetLength]
	}

	// Return the random string
	l.Push(lua.LString(string(result)))
	l.Push(lua.LNil)
	return 2
}

// randomUUID generates a random UUID (v4)
// Returns: (string) UUID string or (nil, error_message) on failure
func randomUUID(l *lua.LState) int {
	// Generate UUID
	id, err := uuid.NewRandom()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to generate UUID: %v", err)))
		return 2
	}

	// Return the UUID as a string
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}
