// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"net"
)

// bindHandshakeCancellation binds only handshake-owned socket I/O. Call the
// returned function exactly once before transferring the session: it disarms
// cancellation or joins an already-running abort. Session ownership starts
// afterward and has its own cancellation path.
func bindHandshakeCancellation(ctx context.Context, conn net.Conn) func() {
	if ctx == nil {
		return func() {}
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(canceled); abortConnection(conn) })
	return func() {
		if !stop() {
			<-canceled
		}
	}
}
