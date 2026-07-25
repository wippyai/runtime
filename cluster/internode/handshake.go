// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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

var authenticatedHandshakeMagic = [4]byte{'W', 'I', 'P', 2}

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

func handshakeTranscript(role string, clientID, serverID cluster.NodeID, clientNonce, serverNonce []byte) []byte {
	transcript := make([]byte, 0, 64+len(clientID)+len(serverID)+len(clientNonce)+len(serverNonce))
	transcript = append(transcript, []byte("wippy/internode/auth/v2\x00")...)
	transcript = append(transcript, role...)
	transcript = append(transcript, 0)
	transcript = append(transcript, authenticatedHandshakeMagic[:]...)
	transcript = append(transcript, byte(len(clientID)))
	transcript = append(transcript, clientID...)
	transcript = append(transcript, byte(len(serverID)))
	transcript = append(transcript, serverID...)
	transcript = append(transcript, clientNonce...)
	transcript = append(transcript, serverNonce...)
	return transcript
}

func handshakeTag(key []byte, transcript []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(transcript)
	return mac.Sum(nil)
}

func performAuthenticatedClientHandshake(conn net.Conn, config NodeConnectionConfig, selfID, expectedRemoteNodeID cluster.NodeID) (cluster.NodeID, error) {
	clientNonce := make([]byte, handshakeNonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, authenticatedHandshakeMagic[:]); err != nil {
		return "", err
	}
	if err := writePrefixedBytes(conn, []byte(selfID)); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, clientNonce); err != nil {
		return "", err
	}

	var magic [len(authenticatedHandshakeMagic)]byte
	if _, err := io.ReadFull(conn, magic[:]); err != nil {
		return "", err
	}
	if !bytes.Equal(magic[:], authenticatedHandshakeMagic[:]) {
		return "", fmt.Errorf("invalid internode handshake protocol")
	}
	serverIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		return "", err
	}
	remoteNodeID := cluster.NodeID(serverIDBytes)
	if remoteNodeID != expectedRemoteNodeID {
		return "", NewNodeIDMismatchError(expectedRemoteNodeID, remoteNodeID)
	}
	serverNonce := make([]byte, handshakeNonceSize)
	if _, err := io.ReadFull(conn, serverNonce); err != nil {
		return "", err
	}
	serverTag := make([]byte, handshakeTagSize)
	if _, err := io.ReadFull(conn, serverTag); err != nil {
		return "", err
	}
	serverSignature := make([]byte, handshakeSignatureSize)
	if _, err := io.ReadFull(conn, serverSignature); err != nil {
		return "", err
	}
	serverTranscript := handshakeTranscript("server", selfID, remoteNodeID, clientNonce, serverNonce)
	expectedServerTag := handshakeTag(config.AuthenticationKey, serverTranscript)
	peerKey, ok := config.ResolvePeerKey(remoteNodeID)
	if !ok || len(peerKey) != ed25519.PublicKeySize || !hmac.Equal(serverTag, expectedServerTag) ||
		!ed25519.Verify(peerKey, serverTranscript, serverSignature) {
		return "", fmt.Errorf("internode server authentication failed")
	}
	clientTranscript := handshakeTranscript("client", selfID, remoteNodeID, clientNonce, serverNonce)
	clientTag := handshakeTag(config.AuthenticationKey, clientTranscript)
	if err := writeHandshakeBytes(conn, clientTag); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, ed25519.Sign(config.SigningKey, clientTranscript)); err != nil {
		return "", err
	}
	return remoteNodeID, nil
}

func performAuthenticatedServerHandshake(conn net.Conn, config NodeConnectionConfig, selfID cluster.NodeID) (cluster.NodeID, error) {
	var magic [len(authenticatedHandshakeMagic)]byte
	if _, err := io.ReadFull(conn, magic[:]); err != nil {
		return "", err
	}
	if !bytes.Equal(magic[:], authenticatedHandshakeMagic[:]) {
		return "", fmt.Errorf("invalid internode handshake protocol")
	}
	clientIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		return "", err
	}
	remoteNodeID := cluster.NodeID(clientIDBytes)
	if remoteNodeID == "" || remoteNodeID == selfID {
		return "", fmt.Errorf("internode peer %q is not authorized", remoteNodeID)
	}
	clientNonce := make([]byte, handshakeNonceSize)
	if _, err := io.ReadFull(conn, clientNonce); err != nil {
		return "", err
	}
	serverNonce := make([]byte, handshakeNonceSize)
	if _, err := rand.Read(serverNonce); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, authenticatedHandshakeMagic[:]); err != nil {
		return "", err
	}
	if err := writePrefixedBytes(conn, []byte(selfID)); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, serverNonce); err != nil {
		return "", err
	}
	serverTranscript := handshakeTranscript("server", remoteNodeID, selfID, clientNonce, serverNonce)
	serverTag := handshakeTag(config.AuthenticationKey, serverTranscript)
	if err := writeHandshakeBytes(conn, serverTag); err != nil {
		return "", err
	}
	if err := writeHandshakeBytes(conn, ed25519.Sign(config.SigningKey, serverTranscript)); err != nil {
		return "", err
	}
	clientTag := make([]byte, handshakeTagSize)
	if _, err := io.ReadFull(conn, clientTag); err != nil {
		return "", err
	}
	clientSignature := make([]byte, handshakeSignatureSize)
	if _, err := io.ReadFull(conn, clientSignature); err != nil {
		return "", err
	}
	clientTranscript := handshakeTranscript("client", remoteNodeID, selfID, clientNonce, serverNonce)
	expectedClientTag := handshakeTag(config.AuthenticationKey, clientTranscript)
	peerKey, ok := config.ResolvePeerKey(remoteNodeID)
	if !ok || len(peerKey) != ed25519.PublicKeySize || !hmac.Equal(clientTag, expectedClientTag) ||
		!ed25519.Verify(peerKey, clientTranscript, clientSignature) {
		return "", fmt.Errorf("internode client authentication failed")
	}
	if config.AuthorizePeer == nil || !config.AuthorizePeer(remoteNodeID, conn.RemoteAddr()) {
		return "", fmt.Errorf("internode peer %q is not authorized", remoteNodeID)
	}
	return remoteNodeID, nil
}

// PerformClientHandshake executes the client side of the handshake protocol.
// On any error, this function is responsible for closing the connection.
// On success, ownership of the connection is transferred to the returned NodeConnection.
func PerformClientHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID, expectedRemoteNodeID cluster.NodeID) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: NewSetDeadlineError(err)}
	}

	var remoteNodeID cluster.NodeID
	var err error
	if config.RequireAuthentication {
		remoteNodeID, err = performAuthenticatedClientHandshake(conn, config, selfID, expectedRemoteNodeID)
	} else {
		if err = writePrefixedBytes(conn, []byte(selfID)); err == nil {
			var serverIDBytes []byte
			serverIDBytes, err = readPrefixedBytes(conn, maxNodeIDLength)
			remoteNodeID = cluster.NodeID(serverIDBytes)
			if err == nil && remoteNodeID != expectedRemoteNodeID {
				err = NewNodeIDMismatchError(expectedRemoteNodeID, remoteNodeID)
			}
		}
	}
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitProtocolError, Err: err}
	}

	_ = conn.SetDeadline(time.Time{})
	return newNodeConnection(conn, remoteNodeID, config, logger), nil
}

// PerformServerHandshake executes the server side of the handshake protocol.
func PerformServerHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID cluster.NodeID) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: NewSetDeadlineError(err)}
	}

	var remoteNodeID cluster.NodeID
	var err error
	if config.RequireAuthentication {
		remoteNodeID, err = performAuthenticatedServerHandshake(conn, config, selfID)
	} else {
		var clientIDBytes []byte
		clientIDBytes, err = readPrefixedBytes(conn, maxNodeIDLength)
		remoteNodeID = cluster.NodeID(clientIDBytes)
		if err == nil {
			err = writePrefixedBytes(conn, []byte(selfID))
		}
	}
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitProtocolError, Err: err}
	}

	_ = conn.SetDeadline(time.Time{})
	return newNodeConnection(conn, remoteNodeID, config, logger), nil
}
