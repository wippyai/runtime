package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestWithRegistry(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		registry Registry
		want     bool
	}{
		{
			name:     "add registry to context with app context",
			setupCtx: ctxapi.NewRootContext,
			registry: &mockRegistry{},
			want:     true,
		},
		{
			name:     "add registry to context without app context",
			setupCtx: context.Background,
			registry: &mockRegistry{},
			want:     false,
		},
		{
			name: "add registry when already exists",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithRegistry(ctx, &mockRegistry{})
			},
			registry: &mockRegistry{},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			resultCtx := WithRegistry(ctx, tt.registry)

			reg := GetRegistry(resultCtx)
			if tt.want {
				assert.NotNil(t, reg)
			} else {
				assert.Nil(t, reg)
			}
		})
	}
}

func TestGetRegistry(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		wantNil  bool
	}{
		{
			name: "get registry from context with registry",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithRegistry(ctx, &mockRegistry{})
			},
			wantNil: false,
		},
		{
			name:     "get registry from context without registry",
			setupCtx: ctxapi.NewRootContext,
			wantNil:  true,
		},
		{
			name:     "get registry from context without app context",
			setupCtx: context.Background,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			reg := GetRegistry(ctx)

			if tt.wantNil {
				assert.Nil(t, reg)
			} else {
				assert.NotNil(t, reg)
			}
		})
	}
}

type mockRegistry struct{}

func (m *mockRegistry) GetAllEntries() ([]Entry, error) { return nil, nil }
func (m *mockRegistry) GetEntry(ID) (Entry, error)      { return Entry{}, nil }
func (m *mockRegistry) Apply(context.Context, ChangeSet) (Version, error) {
	return nil, nil
}
func (m *mockRegistry) ApplyVersion(context.Context, Version) error { return nil }
func (m *mockRegistry) Current() (Version, error)                   { return nil, nil }
func (m *mockRegistry) History() History                            { return nil }
func (m *mockRegistry) RegisterDependencyPattern(_ DependencyPattern) error {
	return nil
}
