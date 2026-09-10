// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/tls"
	"net"
)

// abortConnection releases a failed or retired session without requiring peer
// cooperation. Internode teardown is not an application delivery receipt: queued
// and in-flight work is resolved by the session lifecycle. Close the TLS socket
// first so close_notify cannot hold shutdown or handshake rejection open.
func abortConnection(conn net.Conn) {
	if secured, ok := conn.(*tls.Conn); ok {
		_ = secured.NetConn().Close()
	}
	_ = conn.Close()
}
