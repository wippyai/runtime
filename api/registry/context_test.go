// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestWithRegistry(t *testing.T) {
	tests := []struct {
		registry Registry
		setupCtx func() context.Context
		name     string
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
		setupCtx func() context.Context
		name     string
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
	return nil, nil //nolint:nilnil // test mock
}
func (m *mockRegistry) ApplyVersion(context.Context, Version) error { return nil }
func (m *mockRegistry) LoadState(context.Context, State, Version) error {
	return nil
}
func (m *mockRegistry) Current() (Version, error) { return nil, nil } //nolint:nilnil // test mock
func (m *mockRegistry) History() History          { return nil }
func (m *mockRegistry) RegisterDependencyPattern(_ DependencyPattern) error {
	return nil
}

type mockFinder struct{}

func (m *mockFinder) Find(_ attrs.Bag) ([]Entry, error) { return nil, nil }

type mockResolver struct{}

func (m *mockResolver) Extract(_ Entry) []string                  { return nil }
func (m *mockResolver) RegisterPattern(_ DependencyPattern) error { return nil }

func TestWithFinder(t *testing.T) {
	tests := []struct {
		finder   Finder
		setupCtx func() context.Context
		name     string
		want     bool
	}{
		{
			name:     "add finder to context with app context",
			setupCtx: ctxapi.NewRootContext,
			finder:   &mockFinder{},
			want:     true,
		},
		{
			name:     "add finder to context without app context",
			setupCtx: context.Background,
			finder:   &mockFinder{},
			want:     false,
		},
		{
			name: "add finder when already exists",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithFinder(ctx, &mockFinder{})
			},
			finder: &mockFinder{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			resultCtx := WithFinder(ctx, tt.finder)

			f := GetFinder(resultCtx)
			if tt.want {
				assert.NotNil(t, f)
			} else {
				assert.Nil(t, f)
			}
		})
	}
}

func TestGetFinder(t *testing.T) {
	tests := []struct {
		setupCtx func() context.Context
		name     string
		wantNil  bool
	}{
		{
			name: "get finder from context with finder",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithFinder(ctx, &mockFinder{})
			},
			wantNil: false,
		},
		{
			name:     "get finder from context without finder",
			setupCtx: ctxapi.NewRootContext,
			wantNil:  true,
		},
		{
			name:     "get finder from context without app context",
			setupCtx: context.Background,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			f := GetFinder(ctx)

			if tt.wantNil {
				assert.Nil(t, f)
			} else {
				assert.NotNil(t, f)
			}
		})
	}
}

func TestWithResolver(t *testing.T) {
	tests := []struct {
		resolver DependencyResolver
		setupCtx func() context.Context
		name     string
		want     bool
	}{
		{
			name:     "add resolver to context with app context",
			setupCtx: ctxapi.NewRootContext,
			resolver: &mockResolver{},
			want:     true,
		},
		{
			name:     "add resolver to context without app context",
			setupCtx: context.Background,
			resolver: &mockResolver{},
			want:     false,
		},
		{
			name: "add resolver when already exists",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithResolver(ctx, &mockResolver{})
			},
			resolver: &mockResolver{},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			resultCtx := WithResolver(ctx, tt.resolver)

			r := GetResolver(resultCtx)
			if tt.want {
				assert.NotNil(t, r)
			} else {
				assert.Nil(t, r)
			}
		})
	}
}

func TestGetResolver(t *testing.T) {
	tests := []struct {
		setupCtx func() context.Context
		name     string
		wantNil  bool
	}{
		{
			name: "get resolver from context with resolver",
			setupCtx: func() context.Context {
				ctx := ctxapi.NewRootContext()
				return WithResolver(ctx, &mockResolver{})
			},
			wantNil: false,
		},
		{
			name:     "get resolver from context without resolver",
			setupCtx: ctxapi.NewRootContext,
			wantNil:  true,
		},
		{
			name:     "get resolver from context without app context",
			setupCtx: context.Background,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			r := GetResolver(ctx)

			if tt.wantNil {
				assert.Nil(t, r)
			} else {
				assert.NotNil(t, r)
			}
		})
	}
}
