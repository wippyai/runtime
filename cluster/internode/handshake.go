package internode

import (
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
		return fmt.Errorf("data size %d exceeds max %d", len(data), maxNodeIDLength)
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
		return nil, fmt.Errorf("advertised size %d exceeds max %d", length, maxSize)
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

// PerformClientHandshake executes the client side of the handshake protocol.
// On any error, this function is responsible for closing the connection.
// On success, ownership of the connection is transferred to the returned NodeConnection.
func PerformClientHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID, expectedRemoteNodeID cluster.NodeID) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to set deadline: %w", err)}
	}

	// 1. Client writes its Node ID
	if err := writePrefixedBytes(conn, []byte(selfID)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to write self node ID: %w", err)}
	}

	// 2. Client reads the server's Node ID
	serverIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to read remote node ID: %w", err)}
	}

	// 3. Client verifies the server's Node ID
	remoteNodeID := cluster.NodeID(serverIDBytes)
	if remoteNodeID != expectedRemoteNodeID {
		_ = conn.Close()
		err := fmt.Errorf("expected remote node ID '%s' but got '%s'", expectedRemoteNodeID, remoteNodeID)
		return nil, &ConnectionError{Reason: ExitProtocolError, Err: err}
	}

	// Handshake successful, clear deadline and return connection object
	_ = conn.SetDeadline(time.Time{})

	return newNodeConnection(conn, remoteNodeID, config, logger), nil
}

// PerformServerHandshake executes the server side of the handshake protocol.
func PerformServerHandshake(conn net.Conn, config NodeConnectionConfig, logger *zap.Logger, selfID cluster.NodeID) (*NodeConnection, error) {
	if err := conn.SetDeadline(time.Now().Add(config.HandshakeTimeout)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to set deadline: %w", err)}
	}

	// 1. Server reads the client's Node ID
	clientIDBytes, err := readPrefixedBytes(conn, maxNodeIDLength)
	if err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to read client node ID: %w", err)}
	}
	remoteNodeID := cluster.NodeID(clientIDBytes)

	// 2. Server writes its own Node ID
	if err := writePrefixedBytes(conn, []byte(selfID)); err != nil {
		_ = conn.Close()
		return nil, &ConnectionError{Reason: ExitNetworkError, Err: fmt.Errorf("failed to write self node ID: %w", err)}
	}

	// Handshake successful, clear deadline and return connection object
	_ = conn.SetDeadline(time.Time{})

	return newNodeConnection(conn, remoteNodeID, config, logger), nil
}
