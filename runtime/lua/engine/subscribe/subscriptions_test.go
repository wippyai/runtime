package subscribe

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionManager(t *testing.T) {
	t.Run("basic subscription flow", func(t *testing.T) {
		manager := newSubscriptionContext()
		ch := channel.Named("test", 1)

		// Test adding subscription
		sub, err := manager.add("test-topic", ch)
		require.NoError(t, err)
		assert.Equal(t, "test-topic", sub.topic)
		assert.Equal(t, ch, sub.channel)

		// Test getting subscription
		foundSub, exists := manager.get("test-topic")
		assert.True(t, exists)
		assert.Equal(t, sub, foundSub)

		// Test removing subscription
		err = manager.remove(ch)
		require.NoError(t, err)

		// Verify subscription is removed
		_, exists = manager.get("test-topic")
		assert.False(t, exists)
	})

	t.Run("prevent duplicate topic subscriptions", func(t *testing.T) {
		manager := newSubscriptionContext()
		ch1 := channel.Named("test1", 1)
		ch2 := channel.Named("test2", 1)

		// AddCleanup first subscription
		sub1, err := manager.add("same-topic", ch1)
		require.NoError(t, err)

		// Try to add second subscription to same topic with different channel
		sub2, err := manager.add("same-topic", ch2)
		require.Error(t, err)
		assert.Nil(t, sub2)
		assert.Contains(t, err.Error(), "already has an active subscription")

		// Verify original subscription still exists
		foundSub, exists := manager.get("same-topic")
		assert.True(t, exists)
		assert.Equal(t, sub1, foundSub)
	})

	t.Run("allow same channel reuse", func(t *testing.T) {
		manager := newSubscriptionContext()
		ch := channel.Named("test", 1)

		// AddCleanup first subscription
		sub1, err := manager.add("topic1", ch)
		require.NoError(t, err)

		// Try to add same subscription again
		sub2, err := manager.add("topic1", ch)
		require.NoError(t, err)
		assert.Equal(t, sub1, sub2, "should return existing subscription")
	})

	t.Run("handle invalid unsubscribe", func(t *testing.T) {
		manager := newSubscriptionContext()
		ch := channel.Named("test", 1)

		// Try to remove non-existent subscription
		err := manager.remove(ch)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "channel not found")
	})
}
