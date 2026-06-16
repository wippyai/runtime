// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStream struct{ ch chan Change }

func (f *fakeStream) Changes() <-chan Change { return f.ch }
func (f *fakeStream) Close()                 {}

type fakeEngine struct {
	infos  map[string]SourceInfo
	opened []string
}

func (e *fakeEngine) List() []SourceInfo {
	out := make([]SourceInfo, 0, len(e.infos))
	for _, i := range e.infos {
		out = append(out, i)
	}
	return out
}

func (e *fakeEngine) Get(name string) (SourceInfo, bool) {
	i, ok := e.infos[name]
	return i, ok
}

func (e *fakeEngine) Stream(_ context.Context, name string, _ StreamOptions) (ChangeStream, SourceInfo, error) {
	e.opened = append(e.opened, name)
	return &fakeStream{ch: make(chan Change)}, e.infos[name], nil
}

func TestCompositeListAggregates(t *testing.T) {
	a := &fakeEngine{infos: map[string]SourceInfo{"pg": {Name: "pg", Engine: "postgres"}}}
	b := &fakeEngine{infos: map[string]SourceInfo{"lite": {Name: "lite", Engine: "sqlite"}}}
	c := NewComposite(a, b)

	infos := c.List()
	assert.Len(t, infos, 2)
}

func TestCompositeGetAndStreamRouting(t *testing.T) {
	a := &fakeEngine{infos: map[string]SourceInfo{"pg": {Name: "pg", Engine: "postgres"}}}
	b := &fakeEngine{infos: map[string]SourceInfo{"lite": {Name: "lite", Engine: "sqlite"}}}
	c := NewComposite(a, b)

	info, ok := c.Get("lite")
	require.True(t, ok)
	assert.Equal(t, "sqlite", info.Engine)

	_, _, err := c.Stream(context.Background(), "lite", StreamOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"lite"}, b.opened)
	assert.Empty(t, a.opened)
}

func TestCompositeStreamNotFound(t *testing.T) {
	c := NewComposite(&fakeEngine{infos: map[string]SourceInfo{}})
	_, _, err := c.Stream(context.Background(), "missing", StreamOptions{})
	assert.ErrorIs(t, err, ErrSourceNotFound)
}
