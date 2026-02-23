// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type mockRegistry struct {
	closed bool
}

func (m *mockRegistry) GetFS(_ registry.ID) (fs.ReadDirFS, error) {
	return nil, fs.ErrNotExist
}

func (m *mockRegistry) Close() error {
	m.closed = true
	return nil
}

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "fs.embed", Kind)
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		mockReg := &mockRegistry{}
		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		mockReg := &mockRegistry{}
		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})

	t.Run("registry already set", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		mockReg1 := &mockRegistry{}
		ctx = WithRegistry(ctx, mockReg1)

		mockReg2 := &mockRegistry{}
		ctx = WithRegistry(ctx, mockReg2)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg1, retrieved)
	})
}

func TestConfig(t *testing.T) {
	cfg := Config{}
	assert.NotNil(t, cfg)
}
