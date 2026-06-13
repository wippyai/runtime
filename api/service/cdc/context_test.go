// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type stubInspector struct{}

func (stubInspector) List() []SourceInfo            { return nil }
func (stubInspector) Get(string) (SourceInfo, bool) { return SourceInfo{}, false }

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
