// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
)

func TestConstants(t *testing.T) {
	t.Run("HostID", func(t *testing.T) {
		assert.Equal(t, relay.HostID("node:functions"), HostID)
	})

	t.Run("System", func(t *testing.T) {
		assert.Equal(t, event.System("function"), System)
	})
}

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     event.Kind
		expected string
	}{
		{"register", Register, "function.register"},
		{"delete", Delete, "function.delete"},
		{"accept", Accept, "function.accept"},
		{"reject", Reject, "function.reject"},
		{"options register", OptionsRegister, "function.optionsregister"},
		{"options delete", OptionsDelete, "function.optionsdelete"},
		{"options accept", OptionsAccept, "function.optionsaccept"},
		{"options reject", OptionsReject, "function.optionsreject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.kind))
		})
	}
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})
}
