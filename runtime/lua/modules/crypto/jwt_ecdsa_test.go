// SPDX-License-Identifier: MPL-2.0

package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
)

func newJWTTestState(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	setTimeGlobals(l)
	return l
}

func ecPublicKeyPEM(t *testing.T, key *ecdsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func signECJWT(t *testing.T, key *ecdsa.PrivateKey, claims string) string {
	t.Helper()
	encode := base64.RawURLEncoding.EncodeToString
	input := encode([]byte(`{"alg":"ES256","typ":"JWT"}`)) + "." + encode([]byte(claims))
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	// P-224 integers also fit the ES256 wire width. This deliberately constructs
	// the non-P-256 token that the underlying JWT verifier otherwise accepts.
	width := max(32, (key.Curve.Params().BitSize+7)/8)
	signature := make([]byte, 2*width)
	r.FillBytes(signature[:width])
	s.FillBytes(signature[width:])
	return input + "." + encode(signature)
}

func TestJWTES256VerifyRejectsNonP256Keys(t *testing.T) {
	for _, curve := range []elliptic.Curve{elliptic.P224(), elliptic.P384(), elliptic.P521()} {
		t.Run(curve.Params().Name, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(curve, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			l := newJWTTestState(t)
			l.SetGlobal("TOKEN", lua.LString(signECJWT(t, key, fmt.Sprintf(`{"exp":%d}`, time.Now().Unix()+3600))))
			l.SetGlobal("PUB", lua.LString(ecPublicKeyPEM(t, &key.PublicKey)))
			if err := l.DoString(`
				local claims, err = crypto.jwt.verify(TOKEN, PUB, "ES256")
				assert(claims == nil and err ~= nil, "non-P-256 key was accepted")
				assert(err:kind() == "Internal", "verification error kind changed")
				assert(string.find(tostring(err), "P-256 public key", 1, true), tostring(err))
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJWTES256KeyFormatsAndInteroperability(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"SEC1", "PKCS8"} {
		t.Run(format, func(t *testing.T) {
			var der []byte
			var err error
			pemType := "PRIVATE KEY"
			if format == "SEC1" {
				pemType = "EC PRIVATE KEY"
				der, err = x509.MarshalECPrivateKey(key)
			} else {
				der, err = x509.MarshalPKCS8PrivateKey(key)
			}
			if err != nil {
				t.Fatal(err)
			}
			l := newJWTTestState(t)
			l.SetGlobal("PRIV", lua.LString(pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: der})))
			l.SetGlobal("PUB", lua.LString(ecPublicKeyPEM(t, &key.PublicKey)))
			l.SetGlobal("EXTERNAL", lua.LString(signECJWT(t, key, fmt.Sprintf(`{"sub":"external","exp":%d}`, time.Now().Unix()+3600))))
			if err := l.DoString(`
				local err
				TOKEN, err = crypto.jwt.encode({sub="client", exp=FUTURE_TIME, _header={kid="test-key"}}, PRIV, "ES256")
				assert(TOKEN, tostring(err))
				local claims
				claims, err = crypto.jwt.verify(EXTERNAL, PUB, "ES256")
				assert(claims, tostring(err))
				assert(claims.sub == "external")
			`); err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(string(l.GetGlobal("TOKEN").(lua.LString)), ".")
			if len(parts) != 3 {
				t.Fatalf("expected three JWT segments, got %d", len(parts))
			}
			headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				t.Fatal(err)
			}
			var header map[string]any
			if err := json.Unmarshal(headerBytes, &header); err != nil {
				t.Fatal(err)
			}
			if header["alg"] != "ES256" || header["kid"] != "test-key" {
				t.Fatalf("unexpected header: %s", headerBytes)
			}
			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Fatal(err)
			}
			if len(signature) != 64 {
				t.Fatalf("expected 64-byte JOSE signature, got %d", len(signature))
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
				t.Fatal("standard-library verification of the Lua-produced token failed")
			}
		})
	}
}

func TestJWTES256VerificationBoundaries(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := ecPublicKeyPEM(t, &key.PublicKey)
	futureClaims := fmt.Sprintf(`{"exp":%d}`, time.Now().Unix()+3600)
	token := signECJWT(t, key, futureClaims)
	parts := strings.Split(token, ".")
	input := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(input))
	der, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), signature...)
	tampered[0] ^= 1
	encode := base64.RawURLEncoding.EncodeToString
	hmacInput := encode([]byte(`{"alg":"HS256"}`)) + "." + encode([]byte(futureClaims))
	mac := hmac.New(sha256.New, []byte(publicKey))
	_, _ = mac.Write([]byte(hmacInput))
	expired := signECJWT(t, key, fmt.Sprintf(`{"exp":%d}`, time.Now().Unix()-3600))
	missingExp := signECJWT(t, key, `{"sub":"no-expiration"}`)
	for _, tc := range []struct {
		name       string
		token      string
		requireExp bool
		valid      bool
	}{
		{name: "valid", token: token, requireExp: true, valid: true},
		{name: "DER signature", token: input + "." + encode(der), requireExp: true},
		{name: "short signature", token: input + "." + encode(signature[:63]), requireExp: true},
		{name: "long signature", token: input + "." + encode(append(signature, 0)), requireExp: true},
		{name: "tampered signature", token: input + "." + encode(tampered), requireExp: true},
		{name: "HMAC with EC public key", token: hmacInput + "." + encode(mac.Sum(nil)), requireExp: true},
		{name: "expired", token: expired, requireExp: true},
		{name: "expired with optional exp", token: expired},
		{name: "missing required exp", token: missingExp, requireExp: true},
		{name: "optional exp", token: missingExp, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newJWTTestState(t)
			l.SetGlobal("TOKEN", lua.LString(tc.token))
			l.SetGlobal("PUB", lua.LString(publicKey))
			l.SetGlobal("REQUIRE_EXP", lua.LBool(tc.requireExp))
			l.SetGlobal("VALID", lua.LBool(tc.valid))
			if err := l.DoString(`
				local claims, err = crypto.jwt.verify(TOKEN, PUB, "ES256", REQUIRE_EXP)
				if VALID then
					assert(claims ~= nil and err == nil, tostring(err))
				else
					assert(claims == nil and err ~= nil, "invalid token was accepted")
					assert(err:kind() == "Internal", "verification error kind changed")
				end
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
