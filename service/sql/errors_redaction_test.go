// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRedactConnectionString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		context string
		secrets []string
	}{
		{
			name:    "postgres keyword DSN with quoted and escaped password",
			input:   `dial failed: host='db.internal' user='alice' password='s3cr\'et value' dbname='app'`,
			context: "host='db.internal'",
			secrets: []string{"s3cr", "et value"},
		},
		{
			name:    "postgres URL userinfo",
			input:   "driver rejected postgres://alice:url-secret@db.internal:5432/app",
			context: "db.internal:5432/app",
			secrets: []string{"url-secret"},
		},
		{
			name:    "postgresql URL query password",
			input:   "driver rejected postgresql://alice@db.internal/app?sslmode=require&password=query-secret%2Fencoded",
			context: "sslmode=require",
			secrets: []string{"query-secret"},
		},
		{
			name:    "malformed unterminated keyword password",
			input:   "outer: password='broken-secret trailing diagnostic",
			context: "outer:",
			secrets: []string{"broken-secret"},
		},
		{
			name:    "nested connection strings",
			input:   "outer postgres://alice:outer-secret@primary.internal/app: inner postgresql://bob:nested-secret@replica.internal/app?password=final-secret",
			context: "primary.internal/app",
			secrets: []string{"outer-secret", "nested-secret", "final-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeCause(errors.New(tt.input)).Error()
			for _, secret := range tt.secrets {
				assert.NotContains(t, got, secret)
			}
			assert.Contains(t, got, tt.context)
			assert.Contains(t, got, "[REDACTED]")
		})
	}
}

func TestSQLErrorCausesAreRedactedAtEverySink(t *testing.T) {
	const secret = "postgres-password-that-must-not-leak"
	cause := fmt.Errorf("outer driver error: %w", errors.New(
		"postgresql://alice:"+secret+"@db.internal/app?password="+secret+"; password='"+secret+"'",
	))

	constructors := []struct {
		name      string
		new       func(error) apierror.Error
		kind      apierror.Kind
		retryable apierror.Ternary
	}{
		{"ping", NewPingError, apierror.Unavailable, apierror.True},
		{"invalid config", NewInvalidConfigError, apierror.Invalid, apierror.False},
		{"connection pool", NewConnectionPoolCreationError, apierror.Internal, apierror.False},
		{"WAL", NewWALModeError, apierror.Internal, apierror.False},
		{"invalid DSN", NewInvalidDSNError, apierror.Invalid, apierror.False},
		{"pool update", NewPoolUpdateError, apierror.Internal, apierror.False},
	}

	for _, tt := range constructors {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.new(cause)
			assert.Equal(t, tt.kind, err.Kind())
			assert.Equal(t, tt.retryable, err.Retryable())
			assertNoSecret(t, secret, err.Error())
			assert.Contains(t, err.Error(), "outer driver error")
			assertNoSecret(t, secret, err.Details().GetString("cause", ""))

			unwrapped := errors.Unwrap(err)
			require.Error(t, unwrapped)
			assertNoSecret(t, secret, unwrapped.Error())

			chain := apierror.BuildChain(err)
			require.NotNil(t, chain)
			for _, chained := range chain.Errors {
				assertNoSecret(t, secret, chained.Message)
				for _, detail := range chained.Details {
					assertNoSecret(t, secret, fmt.Sprint(detail))
				}
			}

			l := lua.NewState()
			luaErr := lua.WrapErrorWithLua(l, err, "sql operation")
			require.NotNil(t, luaErr)
			assertNoSecret(t, secret, luaErr.Error())
			assertNoSecret(t, secret, luaErr.String())
			l.Close()
		})
	}
}

func TestSQLErrorCauseRedactionAlsoProtectsZap(t *testing.T) {
	const secret = "zap-password-that-must-not-leak"
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&output),
		zapcore.DebugLevel,
	))

	logger.Error("database startup failed", zap.Error(NewPingError(errors.New(
		"postgres://alice:"+secret+"@db.internal/app password="+secret,
	))))

	assertNoSecret(t, secret, output.String())
	assert.Contains(t, output.String(), "database startup failed")
	assert.Contains(t, output.String(), "[REDACTED]")
}

func TestSQLErrorCausePreservesOrdinaryCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := NewPingError(cause)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, cause.Error(), err.Details().GetString("cause", ""))
}

func TestSanitizeCauseDoesNotTreatPostgresHostPortAsCredentials(t *testing.T) {
	cause := errors.New("connection refused: postgres://localhost:5432/database")
	assert.Same(t, cause, sanitizeCause(cause))
}

func assertNoSecret(t *testing.T, secret, value string) {
	t.Helper()
	assert.NotContains(t, strings.ToLower(value), strings.ToLower(secret), value)
}
