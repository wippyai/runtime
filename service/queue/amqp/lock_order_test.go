// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// The reconnect path takes d.mu.Lock THEN d.pubMu.Lock. Publish historically
// took d.pubMu.Lock THEN d.mu.RLock (inside getPublishChannel → getChannel).
// That AB/BA ordering deadlocks when reconnect fires between Publish's
// initial TryRLock and its getPublishChannel call.
//
// The fix captures the connection reference under the initial RLock and
// passes it into getPublishChannel, so the pubMu critical section never
// touches d.mu. Asserting the helper completes while d.mu is write-locked
// would hang before the fix.
func TestGetPublishChannel_DoesNotAcquireDriverMutex(t *testing.T) {
	d := &Driver{logger: zap.NewNop()}

	d.mu.Lock()
	defer d.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		d.pubMu.Lock()
		defer d.pubMu.Unlock()
		_, err := d.getPublishChannel(nil)
		done <- err
	}()

	select {
	case err := <-done:
		// nil conn returns ErrDriverNotStarted — we care about NOT blocking.
		assert.Error(t, err, "nil conn should yield a clean error, not a hang")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("getPublishChannel blocked on d.mu while another goroutine held it — lock-order inversion")
	}
}
