// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"bufio"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type acceptResult struct {
	conn net.Conn
	err  error
}

func scriptedAcceptPeer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadString('\n'); err != nil {
		return
	}
	if _, err := io.WriteString(conn, "HELLO REPLY RESULT=OK VERSION=3.3\n"); err != nil {
		return
	}
	if _, err := reader.ReadString('\n'); err != nil {
		return
	}
	_, _ = io.WriteString(conn, "STREAM STATUS RESULT=OK\npeer-destination\n")
	_, _ = reader.ReadByte()
}

func listenerForAdoptionTest(t *testing.T) (*samListener, func()) {
	t.Helper()
	ctrl, ctrlPeer := net.Pipe()
	listener := &samListener{
		ctrlConn:  ctrl,
		closeCh:   make(chan struct{}),
		pending:   make(map[net.Conn]struct{}),
		sessionID: "test-session",
	}
	return listener, func() {
		_ = listener.Close()
		_ = ctrlPeer.Close()
	}
}

func requireRejectedAccept(t *testing.T, result <-chan acceptResult, recorder *closeCountingConn) {
	t.Helper()
	select {
	case accepted := <-result:
		if accepted.conn != nil {
			_ = accepted.conn.Close()
		}
		require.Nil(t, accepted.conn)
		require.ErrorIs(t, accepted.err, net.ErrClosed)
	case <-time.After(time.Second):
		t.Fatal("Accept did not finish after listener close")
	}
	require.Equal(t, int32(1), recorder.closes.Load(), "unadopted socket must close exactly once")
}

func TestN14ListenerCloseRejectsPreDialCompletion(t *testing.T) {
	listener, cleanup := listenerForAdoptionTest(t)
	defer cleanup()

	dialStarted := make(chan struct{})
	releaseDial := make(chan struct{})
	var started sync.Once
	client, peer := net.Pipe()
	recorder := &closeCountingConn{Conn: client}
	go scriptedAcceptPeer(peer)
	listener.dialContext = func(context.Context, string, string) (net.Conn, error) {
		started.Do(func() { close(dialStarted) })
		<-releaseDial
		return recorder, nil
	}

	result := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		result <- acceptResult{conn: conn, err: err}
	}()
	select {
	case <-dialStarted:
	case <-time.After(time.Second):
		t.Fatal("Accept did not enter injected dial")
	}
	require.NoError(t, listener.Close())
	close(releaseDial)
	requireRejectedAccept(t, result, recorder)
}

func TestN15ListenerCloseRejectsPostDialAdoption(t *testing.T) {
	listener, cleanup := listenerForAdoptionTest(t)
	defer cleanup()

	client, peer := net.Pipe()
	recorder := &closeCountingConn{Conn: client}
	go scriptedAcceptPeer(peer)
	listener.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return recorder, nil
	}

	adoptionReached := make(chan struct{})
	releaseAdoption := make(chan struct{})
	listener.beforeAdopt = func() {
		close(adoptionReached)
		<-releaseAdoption
	}

	result := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		result <- acceptResult{conn: conn, err: err}
	}()
	select {
	case <-adoptionReached:
	case <-time.After(time.Second):
		t.Fatal("Accept did not reach the adoption gate")
	}
	require.NoError(t, listener.Close())
	close(releaseAdoption)
	requireRejectedAccept(t, result, recorder)
}
