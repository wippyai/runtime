// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

func newTestSubscribeCtx() *subscribeContext {
	return &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}
}

// --- remove ---

func TestSubscribeContext_Remove(t *testing.T) {
	ctx := newTestSubscribeCtx()
	sub, err := ctx.add("topic", 5)
	require.NoError(t, err)

	err = ctx.remove(sub.channel)
	assert.NoError(t, err)

	_, ok := ctx.get("topic")
	assert.False(t, ok)
}

func TestSubscribeContext_Remove_NotFound(t *testing.T) {
	ctx := newTestSubscribeCtx()
	ch := NewChannel(5)

	err := ctx.remove(ch)
	assert.Equal(t, luaapi.ErrChannelNotFound, err)
}

func TestSubscribeContext_Remove_CleansUpBothMaps(t *testing.T) {
	ctx := newTestSubscribeCtx()
	sub, err := ctx.add("topic", 5)
	require.NoError(t, err)

	require.NoError(t, ctx.remove(sub.channel))

	// both maps should be cleaned
	_, inTopic := ctx.byTopic["topic"]
	assert.False(t, inTopic)

	_, inChannel := ctx.byChannel[sub.channel]
	assert.False(t, inChannel)
}

// --- get ---

func TestSubscribeContext_Get(t *testing.T) {
	ctx := newTestSubscribeCtx()
	_, err := ctx.add("topic", 5)
	require.NoError(t, err)

	sub, ok := ctx.get("topic")
	assert.True(t, ok)
	assert.Equal(t, "topic", sub.topic)
}

func TestSubscribeContext_Get_NotFound(t *testing.T) {
	ctx := newTestSubscribeCtx()
	_, ok := ctx.get("nonexistent")
	assert.False(t, ok)
}

// --- match ---

func TestSubscribeContext_Match_Exact(t *testing.T) {
	ctx := newTestSubscribeCtx()
	_, err := ctx.add("exact-topic", 5)
	require.NoError(t, err)

	sub, ok := ctx.match("exact-topic")
	assert.True(t, ok)
	assert.Equal(t, "exact-topic", sub.topic)
}

func TestSubscribeContext_Match_NoMatch(t *testing.T) {
	ctx := newTestSubscribeCtx()
	_, err := ctx.add("topic-a", 5)
	require.NoError(t, err)

	_, ok := ctx.match("topic-b")
	assert.False(t, ok)
}

// --- SubscribeRequest ---

func TestSubscribeRequest_String(t *testing.T) {
	r := &SubscribeRequest{Topic: "test"}
	assert.Equal(t, "<subscribe_request>", r.String())
}

func TestSubscribeRequest_Type(t *testing.T) {
	r := &SubscribeRequest{}
	assert.Equal(t, lua.LTUserData, r.Type())
}

// --- UnsubscribeRequest ---

func TestUnsubscribeRequest_String(t *testing.T) {
	r := &UnsubscribeRequest{}
	assert.Equal(t, "<unsubscribe_request>", r.String())
}

func TestUnsubscribeRequest_Type(t *testing.T) {
	r := &UnsubscribeRequest{}
	assert.Equal(t, lua.LTUserData, r.Type())
}
