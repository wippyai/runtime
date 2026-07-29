// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	failControlHello  = "control HELLO"
	failSessionCreate = "SESSION CREATE"
	failStreamHello   = "stream HELLO"
	failStreamConnect = "STREAM CONNECT"
)

type samScriptResult struct {
	err    error
	opened int32
	closed int32
}

type scriptedSAM struct {
	listener net.Listener
	done     chan samScriptResult
	failAt   string
	payload  string
	once     sync.Once
	opened   atomic.Int32
	closed   atomic.Int32
}

func newScriptedSAM(t *testing.T, failAt, payload string) *scriptedSAM {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &scriptedSAM{
		listener: ln,
		failAt:   failAt,
		payload:  payload,
		done:     make(chan samScriptResult, 1),
	}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *scriptedSAM) finish(err error) {
	s.once.Do(func() {
		s.done <- samScriptResult{opened: s.opened.Load(), closed: s.closed.Load(), err: err}
	})
}

func (s *scriptedSAM) accept() (net.Conn, *bufio.Reader, error) {
	conn, err := s.listener.Accept()
	if err != nil {
		return nil, nil, err
	}
	s.opened.Add(1)
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, bufio.NewReader(conn), nil
}

func readSAMCommand(reader *bufio.Reader, prefix string) error {
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("expected %s command, got %q", prefix, line)
	}
	return nil
}

func writeSAMReply(conn net.Conn, reply string) error {
	_, err := io.WriteString(conn, reply)
	return err
}

func (s *scriptedSAM) observeClose(conn net.Conn) error {
	buffer := make([]byte, 1)
	for {
		_, err := conn.Read(buffer)
		if err != nil {
			s.closed.Add(1)
			_ = conn.Close()
			return nil
		}
	}
}

func (s *scriptedSAM) serve() {
	ctrl, ctrlReader, err := s.accept()
	if err != nil {
		s.finish(err)
		return
	}
	if err = readSAMCommand(ctrlReader, "HELLO VERSION"); err != nil {
		s.finish(err)
		return
	}
	if s.failAt == failControlHello {
		err = writeSAMReply(ctrl, "HELLO REPLY RESULT=I2P_ERROR MESSAGE=injected-control-hello\n")
		if err == nil {
			err = s.observeClose(ctrl)
		}
		s.finish(err)
		return
	}
	if err = writeSAMReply(ctrl, "HELLO REPLY RESULT=OK VERSION=3.3\n"); err != nil {
		s.finish(err)
		return
	}
	if err = readSAMCommand(ctrlReader, "SESSION CREATE"); err != nil {
		s.finish(err)
		return
	}
	if s.failAt == failSessionCreate {
		err = writeSAMReply(ctrl, "SESSION STATUS RESULT=I2P_ERROR MESSAGE=injected-session-create\n")
		if err == nil {
			err = s.observeClose(ctrl)
		}
		s.finish(err)
		return
	}
	if err = writeSAMReply(ctrl, "SESSION STATUS RESULT=OK DESTINATION=transient\n"); err != nil {
		s.finish(err)
		return
	}

	ctrlClosed := make(chan error, 1)
	go func() { ctrlClosed <- s.observeClose(ctrl) }()

	stream, streamReader, err := s.accept()
	if err != nil {
		s.finish(err)
		return
	}
	if err = readSAMCommand(streamReader, "HELLO VERSION"); err != nil {
		s.finish(err)
		return
	}
	if s.failAt == failStreamHello {
		err = writeSAMReply(stream, "HELLO REPLY RESULT=I2P_ERROR MESSAGE=injected-stream-hello\n")
		if err == nil {
			err = s.observeClose(stream)
		}
		if ctrlErr := <-ctrlClosed; err == nil {
			err = ctrlErr
		}
		s.finish(err)
		return
	}
	if err = writeSAMReply(stream, "HELLO REPLY RESULT=OK VERSION=3.3\n"); err != nil {
		s.finish(err)
		return
	}
	if err = readSAMCommand(streamReader, "STREAM CONNECT"); err != nil {
		s.finish(err)
		return
	}
	if s.failAt == failStreamConnect {
		err = writeSAMReply(stream, "STREAM STATUS RESULT=I2P_ERROR MESSAGE=injected-stream-connect\n")
	} else {
		err = writeSAMReply(stream, "STREAM STATUS RESULT=OK\n"+s.payload)
	}
	if err == nil {
		err = s.observeClose(stream)
	}
	if ctrlErr := <-ctrlClosed; err == nil {
		err = ctrlErr
	}
	s.finish(err)
}

func (s *scriptedSAM) address() string { return s.listener.Addr().String() }

func (s *scriptedSAM) requireClosed(t *testing.T, expected int32) {
	t.Helper()
	select {
	case result := <-s.done:
		require.NoError(t, result.err)
		require.Equal(t, expected, result.opened)
		require.Equal(t, expected, result.closed)
	case <-time.After(3 * time.Second):
		t.Fatal("scripted SAM did not observe socket cleanup")
	}
}

type closeCountingConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *closeCountingConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

func TestN09I2PControlHelloFailureClosesSockets(t *testing.T) {
	sam := newScriptedSAM(t, failControlHello, "")
	conn, err := samDial(context.Background(), sam.address(), "test", "tcp", "peer.i2p:80")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "SAM handshake")
	require.ErrorContains(t, err, "injected-control-hello")
	sam.requireClosed(t, 1)
}

func TestN10I2PSessionCreateFailureClosesSockets(t *testing.T) {
	sam := newScriptedSAM(t, failSessionCreate, "")
	conn, err := samDial(context.Background(), sam.address(), "test", "tcp", "peer.i2p:80")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "SESSION CREATE")
	require.ErrorContains(t, err, "injected-session-create")
	sam.requireClosed(t, 1)
}

func TestN11I2PStreamHelloFailureClosesSockets(t *testing.T) {
	sam := newScriptedSAM(t, failStreamHello, "")
	conn, err := samDial(context.Background(), sam.address(), "test", "tcp", "peer.i2p:80")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "stream SAM handshake")
	require.ErrorContains(t, err, "injected-stream-hello")
	sam.requireClosed(t, 2)
}

func TestN12I2PStreamConnectFailureClosesSockets(t *testing.T) {
	sam := newScriptedSAM(t, failStreamConnect, "")
	conn, err := samDial(context.Background(), sam.address(), "test", "tcp", "peer.i2p:80")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "STREAM CONNECT")
	require.ErrorContains(t, err, "injected-stream-connect")
	sam.requireClosed(t, 2)
}

func TestN13I2PStreamSuccessOwnership(t *testing.T) {
	const payload = "coalesced-application-payload"
	sam := newScriptedSAM(t, "", payload)
	conn, err := samDial(context.Background(), sam.address(), "test", "tcp", "peer.i2p:80")
	require.NoError(t, err)

	owned := conn.(*streamConn)
	streamRecorder := &closeCountingConn{Conn: owned.Conn}
	controlRecorder := &closeCountingConn{Conn: owned.ctrl}
	owned.Conn = streamRecorder
	owned.ctrl = controlRecorder

	got := make([]byte, len(payload))
	_, err = io.ReadFull(conn, got)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
	require.NoError(t, conn.Close())
	require.NoError(t, conn.Close())
	require.Equal(t, int32(1), streamRecorder.closes.Load())
	require.Equal(t, int32(1), controlRecorder.closes.Load())
	sam.requireClosed(t, 2)
}
