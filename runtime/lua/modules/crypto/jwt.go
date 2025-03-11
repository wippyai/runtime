package crypto

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	luaconvert "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// registerJWT registers the JWT submodule
func registerJWT(l *lua.LState, mod *lua.LTable) {
	// Create JWT submodule table with exact size
	jwtMod := l.CreateTable(0, 2) // encode, verify

	// Register functions using RawSetString
	jwtMod.RawSetString("encode", l.NewFunction(jwtEncode))
	jwtMod.RawSetString("verify", l.NewFunction(jwtVerify))

	// Add submodule to main module
	mod.RawSetString("jwt", jwtMod)
}

// jwtEncode creates and signs a JWT
// Params:
//
//	payload (table): JWT claims
//	key (string): Signing key
//	alg (string, optional): Algorithm to use ('HS256', 'HS384', 'HS512', default: 'HS256')
//
// Returns: (string) JWT token or (nil, error_message) on failure
func jwtEncode(l *lua.LState) int {
	// Check parameters
	payloadTable := l.CheckTable(1)
	key := l.CheckString(2)
	alg := l.OptString(3, "HS256")

	// Create a new token
	token := jwt.New(getSigningMethod(alg))

	// Convert Lua table to Go map
	payloadMap, ok := luaconvert.ToGoAny(payloadTable).(map[string]any)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to convert payload to map"))
		return 2
	}

	// Set claims
	claims := token.Claims.(jwt.MapClaims)
	for k, v := range payloadMap {
		claims[k] = v
	}

	// Sign the token with the specified key
	tokenString, err := token.SignedString([]byte(key))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to sign token: %v", err)))
		return 2
	}

	// Return the signed token
	l.Push(lua.LString(tokenString))
	l.Push(lua.LNil)
	return 2
}

// jwtVerify verifies and decodes a JWT
// Params:
//
//	token (string): JWT to verify
//	key (string): Verification key
//	alg (string, optional): Expected algorithm ('HS256', 'HS384', 'HS512', default: 'HS256')
//
// Returns: (table) JWT payload or (nil, error_message) on failure
func jwtVerify(l *lua.LState) int {
	// Check parameters
	tokenString := l.CheckString(1)
	key := l.CheckString(2)
	alg := l.OptString(3, "HS256")

	// Parse and verify the token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if token.Method.Alg() != alg {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Return the key
		return []byte(key), nil
	})

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to verify token: %v", err)))
		return 2
	}

	// Check if token is valid
	if !token.Valid {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid token"))
		return 2
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to extract claims"))
		return 2
	}

	// Convert Go map to Lua table
	luaValue, err := luaconvert.GoToLua(claims)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to convert claims to table: %v", err)))
		return 2
	}

	// Return the claims table
	l.Push(luaValue)
	l.Push(lua.LNil)
	return 2
}

// getSigningMethod returns the JWT signing method based on the algorithm string
func getSigningMethod(alg string) jwt.SigningMethod {
	switch alg {
	case "HS256":
		return jwt.SigningMethodHS256
	case "HS384":
		return jwt.SigningMethodHS384
	case "HS512":
		return jwt.SigningMethodHS512
	default:
		return jwt.SigningMethodHS256
	}
}
