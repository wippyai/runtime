package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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
		Description: "Cryptographic operations (hashing, encryption, JWT)",
		Class:       []string{luaapi.ClassSecurity, luaapi.ClassNondeterministic},
	}
}

func (m *cryptoModule) Register(*lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}

		// Random submodule
		randomMod := &lua.LTable{}
		randomMod.RawSetString("bytes", lua.LGoFunc(randomBytes))
		randomMod.RawSetString("string", lua.LGoFunc(randomString))
		randomMod.RawSetString("uuid", lua.LGoFunc(randomUUID))
		randomMod.Immutable = true
		mod.RawSetString("random", randomMod)

		// HMAC submodule
		hmacMod := &lua.LTable{}
		hmacMod.RawSetString("sha256", lua.LGoFunc(hmacSha256))
		hmacMod.RawSetString("sha512", lua.LGoFunc(hmacSha512))
		hmacMod.Immutable = true
		mod.RawSetString("hmac", hmacMod)

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

		// JWT submodule
		jwtMod := &lua.LTable{}
		jwtMod.RawSetString("encode", lua.LGoFunc(jwtEncode))
		jwtMod.RawSetString("verify", lua.LGoFunc(jwtVerify))
		jwtMod.Immutable = true
		mod.RawSetString("jwt", jwtMod)

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

// Error helpers

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.KindInvalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.KindInternal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalErrorMsg(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.KindInternal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

// Random functions

func randomBytes(l *lua.LState) int {
	length := l.CheckInt(1)
	if length <= 0 {
		return invalidError(l, "length must be a positive integer")
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return internalError(l, err, "random bytes")
	}

	l.Push(lua.LString(string(buf)))
	l.Push(lua.LNil)
	return 2
}

func randomString(l *lua.LState) int {
	length := l.CheckInt(1)
	if length <= 0 {
		return invalidError(l, "length must be a positive integer")
	}

	charset := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTString {
		charset = l.ToString(2)
		if len(charset) == 0 {
			return invalidError(l, "charset cannot be empty")
		}
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return internalError(l, err, "random string")
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
		return internalError(l, err, "uuid generation")
	}

	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

// HMAC functions

func hmacSha256(l *lua.LState) int {
	key := l.CheckString(1)
	data := l.CheckString(2)

	if key == "" {
		return invalidError(l, "key cannot be empty")
	}

	h := hmac.New(sha256.New, []byte(key))
	_, err := h.Write([]byte(data))
	if err != nil {
		return internalError(l, err, "compute HMAC")
	}

	digest := h.Sum(nil)
	hexDigest := hex.EncodeToString(digest)

	l.Push(lua.LString(hexDigest))
	l.Push(lua.LNil)
	return 2
}

func hmacSha512(l *lua.LState) int {
	key := l.CheckString(1)
	data := l.CheckString(2)

	if key == "" {
		return invalidError(l, "key cannot be empty")
	}

	h := hmac.New(sha512.New, []byte(key))
	_, err := h.Write([]byte(data))
	if err != nil {
		return internalError(l, err, "compute HMAC")
	}

	digest := h.Sum(nil)
	hexDigest := hex.EncodeToString(digest)

	l.Push(lua.LString(hexDigest))
	l.Push(lua.LNil)
	return 2
}

// Encrypt functions

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
		return invalidError(l, "key must be 16, 24, or 32 bytes")
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return internalError(l, err, "create AES cipher")
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return internalError(l, err, "create GCM")
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return internalError(l, err, "generate nonce")
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
		return invalidError(l, "key must be 16, 24, or 32 bytes")
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return internalError(l, err, "create AES cipher")
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return internalError(l, err, "create GCM")
	}

	dataBytes := []byte(data)
	nonceSize := aesGCM.NonceSize()
	if len(dataBytes) < nonceSize {
		return invalidError(l, "encrypted data too short")
	}

	nonce, ciphertext := dataBytes[:nonceSize], dataBytes[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return internalError(l, err, "decrypt")
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
		return invalidError(l, fmt.Sprintf("key must be %d bytes", chacha20poly1305.KeySize))
	}

	aead, err := chacha20poly1305.New(keyBytes)
	if err != nil {
		return internalError(l, err, "create ChaCha20-Poly1305")
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return internalError(l, err, "generate nonce")
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
		return invalidError(l, fmt.Sprintf("key must be %d bytes", chacha20poly1305.KeySize))
	}

	aead, err := chacha20poly1305.New(keyBytes)
	if err != nil {
		return internalError(l, err, "create ChaCha20-Poly1305")
	}

	dataBytes := []byte(data)
	nonceSize := aead.NonceSize()
	if len(dataBytes) < nonceSize {
		return invalidError(l, "encrypted data too short")
	}

	nonce, ciphertext := dataBytes[:nonceSize], dataBytes[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return internalError(l, err, "decrypt")
	}

	l.Push(lua.LString(string(plaintext)))
	l.Push(lua.LNil)
	return 2
}

// JWT functions

func jwtEncode(l *lua.LState) int {
	payloadTable := l.CheckTable(1)
	key := l.CheckString(2)
	alg := l.OptString(3, "HS256")

	token := jwt.New(getSigningMethod(alg))

	payloadMap, ok := value.ToGoAny(payloadTable).(map[string]any)
	if !ok {
		return invalidError(l, "failed to convert payload to map")
	}

	if headerValue, exists := payloadMap["_header"]; exists {
		if headerMap, isMap := headerValue.(map[string]any); isMap {
			for k, v := range headerMap {
				token.Header[k] = v
			}
			delete(payloadMap, "_header")
		}
	}

	claims := token.Claims.(jwt.MapClaims)
	for k, v := range payloadMap {
		claims[k] = v
	}

	var tokenString string
	var err error

	if alg == "RS256" {
		var privateKey *rsa.PrivateKey
		privateKey, err = parsePrivateKey(key)
		if err != nil {
			return invalidError(l, fmt.Sprintf("invalid RSA private key: %v", err))
		}
		tokenString, err = token.SignedString(privateKey)
	} else {
		tokenString, err = token.SignedString([]byte(key))
	}

	if err != nil {
		return internalError(l, err, "sign token")
	}

	l.Push(lua.LString(tokenString))
	l.Push(lua.LNil)
	return 2
}

func jwtVerify(l *lua.LState) int {
	tokenString := l.CheckString(1)
	key := l.CheckString(2)
	alg := l.OptString(3, "HS256")

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != alg {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		if alg == "RS256" {
			return parsePublicKey(key)
		}

		return []byte(key), nil
	})

	if err != nil {
		return internalError(l, err, "verify token")
	}

	if !token.Valid {
		return invalidError(l, "invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return internalErrorMsg(l, "failed to extract claims")
	}

	luaValue, err := luaconv.GoToLua(claims)
	if err != nil {
		return internalError(l, err, "convert claims to table")
	}

	l.Push(luaValue)
	l.Push(lua.LNil)
	return 2
}

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

func parsePrivateKey(pemString string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
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

func parsePublicKey(pemString string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing public key")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err == nil {
		rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("certificate does not contain an RSA public key")
		}
		return rsaKey, nil
	}

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

// Utility functions

func pbkdf2Derive(l *lua.LState) int {
	password := l.CheckString(1)
	salt := l.CheckString(2)
	iterations := l.CheckInt(3)
	keyLength := l.CheckInt(4)

	if password == "" {
		return invalidError(l, "password cannot be empty")
	}

	if salt == "" {
		return invalidError(l, "salt cannot be empty")
	}

	if iterations <= 0 {
		return invalidError(l, "iterations must be positive")
	}

	if keyLength <= 0 {
		return invalidError(l, "key length must be positive")
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
		return invalidError(l, fmt.Sprintf("unsupported hash function: %s", hashName))
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
