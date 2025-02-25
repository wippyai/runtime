package crypto

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"fmt"
	"hash"

	lua "github.com/yuin/gopher-lua"
	"golang.org/x/crypto/pbkdf2"
)

// Module represents a crypto Lua module
type Module struct{}

// NewCryptoModule creates and returns a new instance of the crypto Module
func NewCryptoModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "crypto"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()

	// Register submodules
	registerRandom(l, mod)
	registerHMAC(l, mod)
	registerEncrypt(l, mod)
	registerDecrypt(l, mod)
	registerJWT(l, mod)

	// Register top-level utility functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"pbkdf2":                pbkdf2Func,
		"constant_time_compare": constantTimeCompare,
	})

	l.Push(mod)
	return 1
}

// pbkdf2Func derives a key using PBKDF2
// Params:
//
//	password (string): Base password/key
//	salt (string): Salt value
//	iterations (number): Number of iterations
//	key_length (number): Desired key length in bytes
//	hash_func (string, optional): Hash function to use ('sha256', 'sha512', default: 'sha256')
//
// Returns: (string) Derived key or (nil, error_message) on failure
func pbkdf2Func(l *lua.LState) int {
	// Check parameters
	password := l.CheckString(1)
	salt := l.CheckString(2)
	iterations := l.CheckInt(3)
	keyLength := l.CheckInt(4)

	// Validate parameters
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

	// Get optional hash function parameter, default to sha256
	var hashFunc func() hash.Hash
	hashName := l.OptString(5, "sha256")

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

	// Derive key
	derivedKey := pbkdf2.Key(
		[]byte(password),
		[]byte(salt),
		iterations,
		keyLength,
		hashFunc,
	)

	// Return derived key
	l.Push(lua.LString(string(derivedKey)))
	l.Push(lua.LNil)
	return 2
}

// constantTimeCompare compares two strings in constant time to prevent timing attacks
// Params:
//
//	a (string): First string
//	b (string): Second string
//
// Returns: (boolean) True if strings are equal, false otherwise
func constantTimeCompare(l *lua.LState) int {
	a := l.CheckString(1)
	b := l.CheckString(2)

	// Compare strings in constant time
	result := subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1

	// Return result as boolean
	l.Push(lua.LBool(result))
	return 1
}
