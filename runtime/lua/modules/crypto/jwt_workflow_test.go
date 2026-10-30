// SPDX-License-Identifier: MPL-2.0

package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
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
	luaworkflow "github.com/wippyai/runtime/runtime/lua/workflow"
)

type jwtWorkflowClock struct{ now time.Time }

func (c jwtWorkflowClock) Now() time.Time       { return c.now }
func (c jwtWorkflowClock) StartTime() time.Time { return c.now }

func TestJWTES256VerificationUsesWorkflowTime(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		claims string
		valid  bool
	}{
		{name: "valid at workflow time", claims: `{"exp":2000}`, valid: true},
		{name: "expired at workflow time", claims: `{"exp":999}`},
		{name: "not yet valid at workflow time", claims: `{"exp":2000,"nbf":1001}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			l.SetGlobal("TOKEN", lua.LString(signECJWT(t, key, tc.claims)))
			l.SetGlobal("PUB", lua.LString(ecPublicKeyPEM(t, &key.PublicKey)))
			l.SetGlobal("VALID", lua.LBool(tc.valid))
			if err := l.DoString(`
				local claims, err = crypto.jwt.verify(TOKEN, PUB, "ES256")
				if VALID then
					assert(claims and not err, tostring(err))
				else
					assert(not claims and err, "invalid time claims accepted")
				end
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJWTES256WorkflowRecordsSignature(t *testing.T) {
	l := newJWTTestState(t)
	ctx, frame := ctxapi.OpenFrameContext(t.Context())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	if err := workflowapi.SetDeterministic(ctx); err != nil {
		t.Fatal(err)
	}
	l.SetContext(ctx)
	private, public := generateECKeyPEM(t)
	l.Push(l.NewTable())
	l.Push(lua.LString(private))
	l.Push(lua.LString("ES256"))
	if result := jwtEncode(l); result != -1 {
		t.Fatalf("workflow signing must yield a recorded side effect, got %d return values", result)
	}
	yield, ok := l.Get(-1).(*luaworkflow.Yield)
	if !ok {
		t.Fatalf("expected a side-effect yield, got %T", l.Get(-1))
	}
	defer yield.Release()
	value, err := yield.Cmd.Fn()
	if err != nil {
		t.Fatal(err)
	}
	token, ok := value.(string)
	if !ok {
		t.Fatalf("expected a recorded string token, got %T", value)
	}
	key, err := jwt.ParseECPublicKeyFromPEM([]byte(public))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"ES256"})); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		values := yield.HandleResult(l, workflowapi.Result{Value: token}, nil)
		if len(values) != 2 || values[0] != lua.LString(token) || values[1] != lua.LNil {
			t.Fatalf("recorded signature changed when resumed: %v", values)
		}
	}
}

func TestJWTExistingAlgorithmsRemainSynchronousInWorkflows(t *testing.T) {
	for _, algorithm := range []string{"default", "HS256", "HS384", "HS512", "RS256"} {
		t.Run(algorithm, func(t *testing.T) {
			key := "test-secret"
			if algorithm == "RS256" {
				private, err := rsa.GenerateKey(rand.Reader, 2048)
				if err != nil {
					t.Fatal(err)
				}
				key = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}))
			}
			l := newJWTTestState(t)
			ctx, frame := ctxapi.OpenFrameContext(t.Context())
			t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
			if err := workflowapi.SetDeterministic(ctx); err != nil {
				t.Fatal(err)
			}
			l.SetContext(ctx)
			l.Push(l.NewTable())
			l.Push(lua.LString(key))
			if algorithm != "default" {
				l.Push(lua.LString(algorithm))
			}
			if result := jwtEncode(l); result != 2 {
				t.Fatalf("existing workflow signing must remain synchronous, got %d", result)
			}
			if l.Get(-2).Type() != lua.LTString || l.Get(-1) != lua.LNil {
				t.Fatal("expected a token and nil error")
			}
		})
	}
}
