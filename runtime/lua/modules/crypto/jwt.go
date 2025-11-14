package crypto

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	luaconvert "github.com/wippyai/runtime/system/payload/lua"
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
//	key (string): Signing key (HMAC secret or RSA private key in PEM format)
//	alg (string, optional): Algorithm to use ('HS256', 'HS384', 'HS512', 'RS256', default: 'HS256')
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

	// Check for custom header fields
	if headerValue, exists := payloadMap["_header"]; exists {
		if headerMap, isMap := headerValue.(map[string]any); isMap {
			// Set custom header fields
			for k, v := range headerMap {
				token.Header[k] = v
			}
			// Remove _header from payload
			delete(payloadMap, "_header")
		}
	}

	// Set claims
	claims := token.Claims.(jwt.MapClaims)
	for k, v := range payloadMap {
		claims[k] = v
	}

	var tokenString string
	var err error

	// Handle signing based on algorithm
	if alg == "RS256" {
		// Parse RSA private key
		var privateKey *rsa.PrivateKey
		privateKey, err = parsePrivateKey(key)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("invalid RSA private key: %v", err)))
			return 2
		}

		// Sign token with RSA private key
		tokenString, err = token.SignedString(privateKey)
	} else {
		// For HMAC algorithms, use the key directly
		tokenString, err = token.SignedString([]byte(key))
	}

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
//	key (string): Verification key (HMAC secret or RSA public key in PEM format)
//	alg (string, optional): Expected algorithm ('HS256', 'HS384', 'HS512', 'RS256', default: 'HS256')
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

		// Return appropriate key based on algorithm
		if alg == "RS256" {
			return parsePublicKey(key)
		}

		// For HMAC, return the key as bytes
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
	case "RS256":
		return jwt.SigningMethodRS256
	default:
		return jwt.SigningMethodHS256
	}
}

// parsePrivateKey parses a PEM encoded private key
func parsePrivateKey(pemString string) (*rsa.PrivateKey, error) {
	// Decode PEM block
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing private key")
	}

	// Parse the key
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format if PKCS1 fails
		privateKey, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("failed to parse private key: %w (PKCS1), %w (PKCS8)", err, err2)
		}

		rsaKey, ok := privateKey.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not an RSA private key")
		}
		return rsaKey, nil
	}

	return key, nil
}

// parsePublicKey parses a PEM encoded public key
func parsePublicKey(pemString string) (*rsa.PublicKey, error) {
	// Decode PEM block
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing public key")
	}

	// Parse the key
	// Try X509 certificate first
	cert, err := x509.ParseCertificate(block.Bytes)
	if err == nil {
		rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("certificate does not contain an RSA public key")
		}
		return rsaKey, nil
	}

	// Try PKIX public key
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	rsaKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA public key")
	}

	return rsaKey, nil
}
