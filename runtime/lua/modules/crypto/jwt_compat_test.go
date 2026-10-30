// SPDX-License-Identifier: MPL-2.0

package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	lua "github.com/wippyai/go-lua"
	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
)

func TestJWTHMACAndRSAInteroperability(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodHS384, jwt.SigningMethodHS512, jwt.SigningMethodRS256} {
		t.Run(method.Alg(), func(t *testing.T) {
			var signingKey, verifyingKey any = []byte("test-secret"), []byte("test-secret")
			private, public := "test-secret", "test-secret"
			if method.Alg() == "RS256" {
				signingKey, verifyingKey = key, &key.PublicKey
				private, public = string(privatePEM), string(publicPEM)
			}
			l := newJWTTestState(t)
			ctx, frame := ctxapi.OpenFrameContext(t.Context())
			t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
			if err := workflowapi.SetDeterministic(ctx); err != nil {
				t.Fatal(err)
			}
			if err := clockapi.WithTimeReference(ctx, jwtWorkflowClock{now: time.Unix(1000, 0)}); err != nil {
				t.Fatal(err)
			}
			l.SetContext(ctx)
			l.SetGlobal("ALG", lua.LString(method.Alg()))
			l.SetGlobal("PRIV", lua.LString(private))
			l.SetGlobal("PUB", lua.LString(public))
			if err := l.DoString(`
				local err
				TOKEN, err = crypto.jwt.encode({sub="client", exp=FUTURE_TIME}, PRIV, ALG)
				assert(TOKEN, tostring(err))
			`); err != nil {
				t.Fatal(err)
			}
			token, err := jwt.Parse(string(l.GetGlobal("TOKEN").(lua.LString)), func(*jwt.Token) (any, error) {
				return verifyingKey, nil
			}, jwt.WithValidMethods([]string{method.Alg()}), jwt.WithExpirationRequired())
			if err != nil {
				t.Fatal(err)
			}
			if !token.Valid || token.Claims.(jwt.MapClaims)["sub"] != "client" {
				t.Fatal("Go verification of the Lua-produced token failed")
			}
			external, err := jwt.NewWithClaims(method, jwt.MapClaims{"sub": "external", "exp": time.Now().Unix() + 3600}).SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			l.SetGlobal("EXTERNAL", lua.LString(external))
			if err := l.DoString(`
				local claims, err = crypto.jwt.verify(EXTERNAL, PUB, ALG)
				assert(claims, tostring(err))
				assert(claims.sub == "external")
			`); err != nil {
				t.Fatal(err)
			}
			// These existing algorithms keep their wall-clock validation behavior
			// even when a workflow clock is present in the frame.
			expired, err := jwt.NewWithClaims(method, jwt.MapClaims{"exp": 2000}).SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			l.SetGlobal("EXPIRED", lua.LString(expired))
			if err := l.DoString(`
				local claims, err = crypto.jwt.verify(EXPIRED, PUB, ALG)
				assert(claims == nil and err ~= nil, "existing expiration behavior changed")
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
