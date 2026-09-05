// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type stubInspector struct{}
type stubStreamer struct{}
type stubRegistry struct{}

func (stubInspector) List() []SourceInfo            { return nil }
func (stubInspector) Get(string) (SourceInfo, bool) { return SourceInfo{}, false }
func (stubStreamer) Stream(context.Context, string, StreamOptions) (ChangeStream, SourceInfo, error) {
	return nil, SourceInfo{}, nil
}
func (stubRegistry) List() []SourceInfo             { return nil }
func (stubRegistry) Get(registry.ID) (Source, bool) { return nil, false }

func TestWithSourceInspectorRoundTrip(t *testing.T) {
	ctx := WithSourceInspector(context.Background(), stubInspector{})
	got := GetSourceInspector(ctx)
	assert.NotNil(t, got)
}

func TestWithSourceInspectorNilDoesNotAttach(t *testing.T) {
	ctx := WithSourceInspector(context.Background(), nil)
	assert.Nil(t, GetSourceInspector(ctx))
}

func TestGetSourceInspectorEmptyCtx(t *testing.T) {
	assert.Nil(t, GetSourceInspector(context.Background()))
}

func TestWithSourceStreamerRoundTrip(t *testing.T) {
	ctx := WithSourceStreamer(context.Background(), stubStreamer{})
	got := GetSourceStreamer(ctx)
	assert.NotNil(t, got)
}

func TestWithSourceStreamerNilDoesNotAttach(t *testing.T) {
	ctx := WithSourceStreamer(context.Background(), nil)
	assert.Nil(t, GetSourceStreamer(ctx))
}

func TestGetSourceStreamerEmptyCtx(t *testing.T) {
	assert.Nil(t, GetSourceStreamer(context.Background()))
}

func TestWithRegistryRoundTripOnApplicationContext(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	reg := stubRegistry{}
	ctx = WithRegistry(ctx, reg)
	assert.Equal(t, reg, GetRegistry(ctx))
}

func TestWithRegistryDoesNotAttachToPlainContext(t *testing.T) {
	reg := stubRegistry{}
	ctx := WithRegistry(context.Background(), reg)
	assert.Nil(t, GetRegistry(ctx))
}

func TestWithRegistryNilDoesNotAttach(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = WithRegistry(ctx, nil)
	assert.Nil(t, GetRegistry(ctx))
}
