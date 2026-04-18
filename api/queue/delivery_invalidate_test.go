// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A Delivery carries a pooled *Message that the consumer releases back to
// sync.Pool after the handler returns. Any Lua userdata that kept a
// reference to the wrapper must stop dereferencing Message at that
// point — a recycled pool entry could hold another message's data.
// Delivery provides an atomic one-shot invalidation flag so wrapper
// accessors can gate themselves cleanly before touching Message.
func TestDelivery_InvalidateFlipsReleasedAtomically(t *testing.T) {
	var d Delivery
	assert.False(t, d.Released(),
		"a fresh Delivery must not report released")

	d.Invalidate()
	assert.True(t, d.Released(),
		"Invalidate must flip Released to true")

	// Invalidate is idempotent — a double-invalidate must not panic or
	// flip the flag back.
	d.Invalidate()
	assert.True(t, d.Released(),
		"double Invalidate must keep Released true")
}

// Ack coordination: a handler that called msg:ack() / msg:nack()
// through the Lua wrapper must prevent the consumer's post-handler
// auto-ack from double-acking the same delivery. MarkSettled is the
// atomic one-way flag that powers both the manual-path double-settle
// guard and the consumer's skip-when-already-settled check.
func TestDelivery_MarkSettledFlipsSettledAtomically(t *testing.T) {
	var d Delivery
	assert.False(t, d.Settled(),
		"a fresh Delivery must not report settled")

	assert.True(t, d.MarkSettled(),
		"first MarkSettled must claim the settle (return true)")
	assert.True(t, d.Settled(),
		"Settled must be true after MarkSettled")

	// Second caller must see the claim lost — this is how a double
	// manual ack detects it's racing with an earlier settler and can
	// bail out cleanly instead of calling Ack on the broker twice.
	assert.False(t, d.MarkSettled(),
		"second MarkSettled must return false (lost the race)")
	assert.True(t, d.Settled(),
		"Settled must stay true after a losing MarkSettled")
}

// Concurrent Invalidate + Released() under -race verifies the flag is
// backed by atomic operations, not a plain bool.
func TestDelivery_InvalidateIsRaceFree(t *testing.T) {
	const iterations = 500
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		d := &Delivery{}
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.Invalidate()
		}()
		go func() {
			defer wg.Done()
			_ = d.Released()
		}()
	}
	wg.Wait()
}
