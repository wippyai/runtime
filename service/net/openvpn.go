// SPDX-License-Identifier: MPL-2.0

package net

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	netapi "github.com/wippyai/runtime/api/net"
)

var _ netapi.Service = (*OpenVPNService)(nil)

// mgmtDialer abstracts the connection to the OpenVPN management interface.
// This enables unit testing with a mock implementation.
type mgmtDialer interface {
	// QueryLocalIP connects to the management interface, authenticates if
	// needed, sends the "state" command, and returns the VPN's assigned local IP.
	QueryLocalIP(ctx context.Context) (net.IP, error)
}

// OpenVPNService routes connections through an OpenVPN TUN interface.
//
// It connects to the OpenVPN management interface to discover the VPN's
// assigned local IP address, then binds outbound connections to that IP
// using net.Dialer.LocalAddr. The OS routing table (configured by the
// OpenVPN client) handles actual routing through the VPN tunnel.
type OpenVPNService struct {
	mgmt    mgmtDialer
	localIP net.IP
	mu      sync.RWMutex
}

// NewOpenVPNService creates a new OpenVPN service by connecting to the
// management interface and querying the VPN's assigned local IP.
func NewOpenVPNService(cfg *netapi.OpenVPNConfig) (*OpenVPNService, error) {
	mgmt := &realMgmtDialer{
		addr:     cfg.ManagementAddress,
		password: cfg.ManagementPassword,
	}

	ip, err := mgmt.QueryLocalIP(context.Background())
	if err != nil {
		return nil, fmt.Errorf("openvpn: %w", err)
	}

	return &OpenVPNService{mgmt: mgmt, localIP: ip}, nil
}

// newOpenVPNServiceWithMgmt creates an OpenVPNService with an injected
// management dialer. This is used for testing with mock implementations.
func newOpenVPNServiceWithMgmt(mgmt mgmtDialer, localIP net.IP) *OpenVPNService {
	return &OpenVPNService{mgmt: mgmt, localIP: localIP}
}

func (s *OpenVPNService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	s.mu.RLock()
	ip := s.localIP
	s.mu.RUnlock()

	if ip == nil {
		return nil, fmt.Errorf("openvpn: no local VPN IP available")
	}

	d := net.Dialer{
		LocalAddr: &net.TCPAddr{IP: ip},
	}
	return d.DialContext(ctx, network, address)
}

func (s *OpenVPNService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("openvpn: %w (inbound connections not supported through VPN)", netapi.ErrNotSupported)
}

func (s *OpenVPNService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("openvpn: %w (UDP not available through management interface)", netapi.ErrNotSupported)
}

func (s *OpenVPNService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("openvpn: %w (use system resolver or VPN-pushed DNS)", netapi.ErrNotSupported)
}

// realMgmtDialer connects to a real OpenVPN management interface.
type realMgmtDialer struct {
	addr     string
	password string
}

// QueryLocalIP implements mgmtDialer by connecting to the management interface
// TCP socket, handling the optional password prompt, sending the "state"
// command, and parsing the VPN's assigned local IP from the response.
//
// Management protocol:
//
//	>INFO:OpenVPN Management Interface ...
//	ENTER PASSWORD:                       (only if password-protected)
//	>INFO:...
//	state
//	1234567890,CONNECTED,SUCCESS,10.8.0.6,1.2.3.4,1194,,
//	END
func (d *realMgmtDialer) QueryLocalIP(ctx context.Context) (net.IP, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, fmt.Errorf("management connect to %s: %w", d.addr, err)
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	// Read the initial lines from the management interface.
	// Real OpenVPN protocol order:
	//   Without password: >INFO:... (then waits for commands)
	//   With password: ENTER PASSWORD: → (client sends pw) → SUCCESS:... → >INFO:... (then waits)
	// After the final >INFO: line, the server is ready for commands.
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ENTER PASSWORD:") {
			if _, err := fmt.Fprintf(conn, "%s\n", d.password); err != nil {
				return nil, fmt.Errorf("management auth: %w", err)
			}
			continue
		}
		if strings.HasPrefix(line, "SUCCESS:") {
			continue
		}
		// Any other line (including >INFO: banner) means the server
		// is ready for commands.
		break
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("management read banner: %w", err)
	}

	// Send the "state" command
	if _, err := fmt.Fprintf(conn, "state\n"); err != nil {
		return nil, fmt.Errorf("management send state: %w", err)
	}

	// Parse the state response looking for CONNECTED line
	for scanner.Scan() {
		line := scanner.Text()
		if line == "END" {
			break
		}
		// State line format: timestamp,STATE,DESC,localIP,remoteIP,port,,
		// e.g. "1234567890,CONNECTED,SUCCESS,10.8.0.6,1.2.3.4,1194,,"
		parts := strings.Split(line, ",")
		if len(parts) >= 4 && parts[1] == "CONNECTED" {
			ip := net.ParseIP(strings.TrimSpace(parts[3]))
			if ip == nil {
				return nil, fmt.Errorf("management: invalid local IP %q in state", parts[3])
			}
			// Send quit to cleanly close the management session
			_, _ = fmt.Fprintf(conn, "quit\n")
			return ip, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("management read state: %w", err)
	}

	return nil, fmt.Errorf("management: VPN not in CONNECTED state")
}
