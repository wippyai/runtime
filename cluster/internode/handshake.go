// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

const maxNodeIDLength = 255 // Maximum length for a node ID.

// writePrefixedBytes writes a length-prefixed byte slice to the writer.
// The prefix is a single byte representing the length of the data.
func writePrefixedBytes(w io.Writer, data []byte) error {
	if len(data) > maxNodeIDLength {
		return ErrDataSizeExceedsMax
	}
	if _, err := w.Write([]byte{byte(len(data))}); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// readPrefixedBytes reads a length-prefixed byte slice from the reader.
func readPrefixedBytes(r io.Reader, maxSize int) ([]byte, error) {
	var lengthByte [1]byte
	if _, err := io.ReadFull(r, lengthByte[:]); err != nil {
		return nil, err
	}

	length := int(lengthByte[0])
	if length > maxSize {
		return nil, ErrAdvertisedSizeExceedsMax
	}
	if length == 0 {
		return []byte{}, nil
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

var authenticatedHandshakeMagic = [4]byte{'W', 'I', 'P', 3}

const (
	handshakeNonceSize     = 32
	handshakeTagSize       = sha256.Size
	handshakeSignatureSize = ed25519.SignatureSize
)

func writeHandshakeBytes(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// handshakeEndpoint is one side's claimed identity in the handshake.
type handshakeEndpoint struct {
	id          cluster.NodeID
	incarnation uint64
}

// writeIncarnation writes a nonzero incarnation as 8 little-endian bytes.
func writeIncarnation(w io.Writer, incarnation uint64) error {
	if incarnation == 0 {
		return errZeroIncarnation
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], incarnation)
	return writeHandshakeBytes(w, b[:])
}

// readIncarnation reads the peer's incarnation and rejects zero.
func readIncarnation(r io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	incarnation := binary.LittleEndian.Uint64(b[:])
	if incarnation == 0 {
		return 0, errZeroIncarnation
	}
	return incarnation, nil
}

func handshakeTranscript(role string, client, server handshakeEndpoint, clientNonce, serverNonce []byte) []byte {
	clientID, serverID := client.id, server.id
	transcript := make([]byte, 0, 80+len(clientID)+len(serverID)+len(clientNonce)+len(serverNonce))
	transcript = append(transcript, []byte("wippy/internode/auth/v3\x00")...)
	transcript = append(transcript, role...)
	transcript = append(transcript, 0)
	transcript = append(transcript, authenticatedHandshakeMagic[:]...)
	transcript = append(transcript, byte(len(clientID)))
	transcript = append(transcript, clientID...)
	transcript = append(transcript, byte(len(serverID)))
	transcript = append(transcript, serverID...)
	transcript = binary.LittleEndian.AppendUint64(transcript, client.incarnation)
	transcript = binary.LittleEndian.AppendUint64(transcript, server.incarnation)
	transcript = append(transcript, clientNonce...)
	transcript = append(transcript, serverNonce...)
	return transcript
}

func handshakeTag(key []byte, transcript []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(transcript)
	return mac.Sum(nil)
}

func performAuthenticatedClientHandshake(conn net.Conn, config NodeConnectionConfig, self handshakeEndpoint, expectedRemoteNodeID cluster.NodeID) (handshakeEndpoint, error) {
	clientNonce := make([]byte, handshakeNonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, authenticatedHandshakeMagic[:]); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writePrefixedBytes(conn, []byte(self.id)); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeIncarnation(conn, self.incarnation); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, clientNonce); err != nil {
		return handshakeEndpoint{}, err
	}

	var magic [len(authenticatedHandshakeMagic)]byte
	if _, err := io.ReadFull(conn, magic[:]); err != nil {
		return handshakeEndpoint{}, err
	}
	if !bytes.Equal(magic[:], authenticatedHandshakeMagic[:]) {
		return handshakeEndpoint{}, fmt.Errorf("invalid internode handshake protocol")
	}
	serverIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	remote := handshakeEndpoint{id: cluster.NodeID(serverIDBytes)}
	if remote.id != expectedRemoteNodeID {
		return handshakeEndpoint{}, NewNodeIDMismatchError(expectedRemoteNodeID, remote.id)
	}
	if remote.incarnation, err = readIncarnation(conn); err != nil {
		return handshakeEndpoint{}, err
	}
	serverNonce := make([]byte, handshakeNonceSize)
	if _, err := io.ReadFull(conn, serverNonce); err != nil {
		return handshakeEndpoint{}, err
	}
	serverTag := make([]byte, handshakeTagSize)
	if _, err := io.ReadFull(conn, serverTag); err != nil {
		return handshakeEndpoint{}, err
	}
	serverSignature := make([]byte, handshakeSignatureSize)
	if _, err := io.ReadFull(conn, serverSignature); err != nil {
		return handshakeEndpoint{}, err
	}
	serverTranscript := handshakeTranscript("server", self, remote, clientNonce, serverNonce)
	expectedServerTag := handshakeTag(config.AuthenticationKey, serverTranscript)
	peerKey, ok := config.ResolvePeerKey(remote.id)
	if !ok || len(peerKey) != ed25519.PublicKeySize || !hmac.Equal(serverTag, expectedServerTag) ||
		!ed25519.Verify(peerKey, serverTranscript, serverSignature) {
		return handshakeEndpoint{}, fmt.Errorf("internode server authentication failed")
	}
	if config.AuthorizePeer == nil || !config.AuthorizePeer(remote.id, conn.RemoteAddr()) {
		return handshakeEndpoint{}, newPeerNotAuthorizedError(remote.id)
	}
	clientTranscript := handshakeTranscript("client", self, remote, clientNonce, serverNonce)
	clientTag := handshakeTag(config.AuthenticationKey, clientTranscript)
	if err := writeHandshakeBytes(conn, clientTag); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, ed25519.Sign(config.SigningKey, clientTranscript)); err != nil {
		return handshakeEndpoint{}, err
	}
	return remote, nil
}

func performAuthenticatedServerHandshake(conn net.Conn, config NodeConnectionConfig, self handshakeEndpoint) (handshakeEndpoint, error) {
	var magic [len(authenticatedHandshakeMagic)]byte
	if _, err := io.ReadFull(conn, magic[:]); err != nil {
		return handshakeEndpoint{}, err
	}
	if !bytes.Equal(magic[:], authenticatedHandshakeMagic[:]) {
		return handshakeEndpoint{}, fmt.Errorf("invalid internode handshake protocol")
	}
	clientIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	remote := handshakeEndpoint{id: cluster.NodeID(clientIDBytes)}
	if remote.id == "" || remote.id == self.id {
		return handshakeEndpoint{}, fmt.Errorf("internode peer %q is not authorized", remote.id)
	}
	if remote.incarnation, err = readIncarnation(conn); err != nil {
		return handshakeEndpoint{}, err
	}
	clientNonce := make([]byte, handshakeNonceSize)
	if _, err := io.ReadFull(conn, clientNonce); err != nil {
		return handshakeEndpoint{}, err
	}
	serverNonce := make([]byte, handshakeNonceSize)
	if _, err := rand.Read(serverNonce); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, authenticatedHandshakeMagic[:]); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writePrefixedBytes(conn, []byte(self.id)); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeIncarnation(conn, self.incarnation); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, serverNonce); err != nil {
		return handshakeEndpoint{}, err
	}
	serverTranscript := handshakeTranscript("server", remote, self, clientNonce, serverNonce)
	serverTag := handshakeTag(config.AuthenticationKey, serverTranscript)
	if err := writeHandshakeBytes(conn, serverTag); err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writeHandshakeBytes(conn, ed25519.Sign(config.SigningKey, serverTranscript)); err != nil {
		return handshakeEndpoint{}, err
	}
	clientTag := make([]byte, handshakeTagSize)
	if _, err := io.ReadFull(conn, clientTag); err != nil {
		return handshakeEndpoint{}, err
	}
	clientSignature := make([]byte, handshakeSignatureSize)
	if _, err := io.ReadFull(conn, clientSignature); err != nil {
		return handshakeEndpoint{}, err
	}
	clientTranscript := handshakeTranscript("client", remote, self, clientNonce, serverNonce)
	expectedClientTag := handshakeTag(config.AuthenticationKey, clientTranscript)
	peerKey, ok := config.ResolvePeerKey(remote.id)
	if !ok || len(peerKey) != ed25519.PublicKeySize || !hmac.Equal(clientTag, expectedClientTag) ||
		!ed25519.Verify(peerKey, clientTranscript, clientSignature) {
		return handshakeEndpoint{}, fmt.Errorf("internode client authentication failed")
	}
	if config.AuthorizePeer == nil || !config.AuthorizePeer(remote.id, conn.RemoteAddr()) {
		return handshakeEndpoint{}, newPeerNotAuthorizedError(remote.id)
	}
	return remote, nil
}

// PerformClientHandshake executes the client side of the handshake protocol.
// Both sides exchange their node ID and nonzero process incarnation; the
// authenticated variant binds both into the signed transcript.
// On any error, this function is responsible for closing the connection.
// On success, ownership of the connection is transferred to the returned NodeConnection.
func PerformClientHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID cluster.NodeID, selfIncarnation uint64, expectedRemoteNodeID cluster.NodeID) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		abortConnection(conn)
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: NewSetDeadlineError(err)}
	}

	self := handshakeEndpoint{id: selfID, incarnation: selfIncarnation}
	var remote handshakeEndpoint
	var err error
	if config.RequireAuthentication {
		remote, err = performAuthenticatedClientHandshake(conn, config, self, expectedRemoteNodeID)
	} else {
		remote, err = performPlainClientHandshake(conn, self, expectedRemoteNodeID)
	}
	if err != nil {
		abortConnection(conn)
		return nil, &ConnectionError{Reason: ExitProtocolError, Err: err}
	}

	_ = conn.SetDeadline(time.Time{})
	nodeConn := newNodeConnection(conn, remote.id, remote.incarnation, config, logger)
	nodeConn.dialed = true
	return nodeConn, nil
}

// PerformServerHandshake executes the server side of the handshake protocol.
func PerformServerHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID cluster.NodeID, selfIncarnation uint64) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		abortConnection(conn)
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: NewSetDeadlineError(err)}
	}

	self := handshakeEndpoint{id: selfID, incarnation: selfIncarnation}
	var remote handshakeEndpoint
	var err error
	if config.RequireAuthentication {
		remote, err = performAuthenticatedServerHandshake(conn, config, self)
	} else {
		remote, err = performPlainServerHandshake(conn, self)
	}
	if err != nil {
		abortConnection(conn)
		return nil, &ConnectionError{Reason: ExitProtocolError, Err: err}
	}

	_ = conn.SetDeadline(time.Time{})
	return newNodeConnection(conn, remote.id, remote.incarnation, config, logger), nil
}

// writePlainEndpoint writes a node ID and incarnation as one message.
func writePlainEndpoint(w io.Writer, self handshakeEndpoint) error {
	if len(self.id) > maxNodeIDLength {
		return ErrDataSizeExceedsMax
	}
	if self.incarnation == 0 {
		return errZeroIncarnation
	}
	msg := make([]byte, 0, 1+len(self.id)+8)
	msg = append(msg, byte(len(self.id)))
	msg = append(msg, self.id...)
	msg = binary.LittleEndian.AppendUint64(msg, self.incarnation)
	return writeHandshakeBytes(w, msg)
}

// readPlainEndpoint reads a peer's node ID and incarnation.
func readPlainEndpoint(r io.Reader) (handshakeEndpoint, error) {
	id, err := readPrefixedBytes(r, maxNodeIDLength)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	incarnation, err := readIncarnation(r)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	return handshakeEndpoint{id: cluster.NodeID(id), incarnation: incarnation}, nil
}

func performPlainClientHandshake(conn net.Conn, self handshakeEndpoint, expectedRemoteNodeID cluster.NodeID) (handshakeEndpoint, error) {
	if err := writePlainEndpoint(conn, self); err != nil {
		return handshakeEndpoint{}, err
	}
	remote, err := readPlainEndpoint(conn)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	if remote.id != expectedRemoteNodeID {
		return handshakeEndpoint{}, NewNodeIDMismatchError(expectedRemoteNodeID, remote.id)
	}
	return remote, nil
}

func performPlainServerHandshake(conn net.Conn, self handshakeEndpoint) (handshakeEndpoint, error) {
	remote, err := readPlainEndpoint(conn)
	if err != nil {
		return handshakeEndpoint{}, err
	}
	if err := writePlainEndpoint(conn, self); err != nil {
		return handshakeEndpoint{}, err
	}
	return remote, nil
}
