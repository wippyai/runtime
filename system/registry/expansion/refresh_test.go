// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestPlanner_Expand_CarriesRefresh(t *testing.T) {
	assets := registry.NewID("mod", "assets")
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{Applied: true, Refresh: []registry.ID{assets}}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"svc": {dir}}, nil, zap.NewNop())
	plan, err := p.Expand(context.Background(), registry.ChangeSet{newOp(registry.EntryUpdate, newEntry("app", "item", "svc"))}, nil)
	require.NoError(t, err)
	assert.True(t, plan.Expanded)
	assert.Equal(t, []registry.ID{assets}, plan.Refresh)
}

func TestPlanner_Expand_RejectsRefreshWithoutApplied(t *testing.T) {
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{Refresh: []registry.ID{registry.NewID("mod", "assets")}}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"svc": {dir}}, nil, zap.NewNop())
	_, err := p.Expand(context.Background(), registry.ChangeSet{newOp(registry.EntryUpdate, newEntry("app", "item", "svc"))}, nil)
	require.Error(t, err)
}
