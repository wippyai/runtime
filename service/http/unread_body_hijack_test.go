// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClosingRequestDoesNotTouchHijackedConnection(t *testing.T) {
	hijacked := make(chan net.Conn, 1)
	address := startUnreadBodyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.SetDeadline(time.Time{})
		_, _ = rw.WriteString("hijacked\n")
		_ = rw.Flush()
		hijacked <- conn
	}))
	client, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(t.Context(), "tcp", address)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = io.WriteString(client, "POST / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\nContent-Length: 1\r\n\r\n")
	require.NoError(t, err)
	line, err := bufio.NewReader(client).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "hijacked\n", line)
	var serverConn net.Conn
	select {
	case serverConn = <-hijacked:
	case <-time.After(3 * time.Second):
		t.Fatal("no hijacked connection")
	}
	defer serverConn.Close()
	// A hijacker owns the socket after ServeHTTP returns. No deferred HTTP
	// cleanup may consume its input or impose a new deadline.
	<-time.After(800 * time.Millisecond)
	_, err = io.WriteString(client, "x")
	require.NoError(t, err)
	one := make([]byte, 1)
	_, err = io.ReadFull(serverConn, one)
	require.NoError(t, err, "the server installed a read deadline after the handler hijacked the socket")
	require.Equal(t, "x", string(one))
}
