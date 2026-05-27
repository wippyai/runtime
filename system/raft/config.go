// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"fmt"
	"net"

	hraft "github.com/hashicorp/raft"

	raftapi "github.com/wippyai/runtime/api/raft"
)

// toHashicorpConfig converts our config into a hashicorp/raft Config.
func toHashicorpConfig(localID string, cfg raftapi.Config) *hraft.Config {
	rc := hraft.DefaultConfig()
	rc.LocalID = hraft.ServerID(localID)
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ElectionTimeout = cfg.ElectionTimeout
	rc.CommitTimeout = cfg.CommitTimeout
	rc.SnapshotInterval = cfg.SnapshotInterval
	rc.SnapshotThreshold = cfg.SnapshotThreshold
	if cfg.TrailingLogs > 0 {
		rc.TrailingLogs = cfg.TrailingLogs
	}
	if cfg.MaxAppendEntries > 0 {
		rc.MaxAppendEntries = cfg.MaxAppendEntries
	}
	// Suppress raft internal logging by using a discard logger.
	// Leadership changes are published via our event bus instead.
	rc.LogLevel = "WARN"
	return rc
}

// resolveTransportAddr builds the TCP bind address from config. Used by
// the legacy TCP transport path that Raft instances without an
// internode connection manager fall back to.
func resolveTransportAddr(cfg raftapi.Config) string {
	//nolint:staticcheck // legacy TCP fallback path; mesh transport ignores BindAddr/BindPort.
	return net.JoinHostPort(cfg.BindAddr, fmt.Sprintf("%d", cfg.BindPort))
}

// resolveAdvertiseAddr builds the advertise address. If AdvertiseAddr is
// empty, falls back to BindAddr. If BindAddr is 0.0.0.0 (not
// advertisable), defaults to 127.0.0.1.
func resolveAdvertiseAddr(cfg raftapi.Config, actualPort int) *net.TCPAddr {
	//nolint:staticcheck // legacy TCP fallback path; mesh transport ignores these fields.
	host := cfg.AdvertiseAddr
	if host == "" {
		host = cfg.BindAddr //nolint:staticcheck // see above
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return &net.TCPAddr{
		IP:   net.ParseIP(host),
		Port: actualPort,
	}
}

// findAvailablePort tries ports in [base, base+9] and returns the first
// available one. Used by the legacy TCP transport path.
func findAvailablePort(bindAddr string, basePort int) (int, error) {
	for port := basePort; port < basePort+10; port++ {
		addr := net.JoinHostPort(bindAddr, fmt.Sprintf("%d", port))
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
		if err != nil {
			continue
		}
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no available port in range %d-%d", basePort, basePort+9)
}

// autoDetectPort finds an available port if AutoPort is set, otherwise
// returns the configured port. Used by the legacy TCP transport path.
func autoDetectPort(cfg raftapi.Config) (int, error) {
	//nolint:staticcheck // legacy TCP fallback path; mesh transport ignores these fields.
	if cfg.AutoPort {
		return findAvailablePort(cfg.BindAddr, cfg.BindPort)
	}
	return cfg.BindPort, nil //nolint:staticcheck // see above
}
