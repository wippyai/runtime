// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/require"
)

func automaticTransportConfig() *memberlist.Config {
	cfg := memberlist.DefaultLocalConfig()
	cfg.Name = "transport-test"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.LogOutput = io.Discard
	return cfg
}

func TestAutomaticTransportRetriesAndRetainsBothListeners(t *testing.T) {
	cfg := automaticTransportConfig()
	attempts := 0
	ml, err := createMemberlist(t.Context(), cfg, func(nc *memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		if attempts == 1 {
			require.Zero(t, nc.BindPort)
			return nil, errors.New("UDP port excluded")
		}
		require.GreaterOrEqual(t, nc.BindPort, 49152)
		require.LessOrEqual(t, nc.BindPort, 65535)
		return memberlist.NewNetTransport(nc)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ml.Shutdown() })
	require.GreaterOrEqual(t, attempts, 2)
	require.Equal(t, cfg.BindPort, cfg.AdvertisePort)
	require.Equal(t, cfg.BindPort, int(ml.LocalNode().Port))
	address := net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.BindPort))
	tcp, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
	if tcp != nil {
		_ = tcp.Close()
	}
	require.Error(t, err, "membership must retain its TCP listener")
	udp, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", address)
	if udp != nil {
		_ = udp.Close()
	}
	require.Error(t, err, "membership must retain its UDP listener")
	require.NoError(t, ml.Shutdown())
	assertTransportPortReleased(t, address)
}

func TestAutomaticTransportFailureIsBounded(t *testing.T) {
	cfg := automaticTransportConfig()
	want := errors.New("bind denied")
	attempts := 0
	_, err := createMemberlist(t.Context(), cfg, func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		return nil, want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 32, attempts)
	require.Nil(t, cfg.Transport)
	require.Zero(t, cfg.BindPort)
}

func TestAutomaticTransportCancellationStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	_, err := createMemberlist(ctx, automaticTransportConfig(), func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		cancel()
		return nil, errors.New("bind denied")
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestAutomaticTransportClosesOnMemberlistFailure(t *testing.T) {
	cfg := automaticTransportConfig()
	cfg.AdvertiseAddr = "invalid-ip"
	_, err := createMemberlist(t.Context(), cfg, memberlist.NewNetTransport)
	require.ErrorContains(t, err, "advertise address")
	require.Positive(t, cfg.BindPort)
	assertTransportPortReleased(t, net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.BindPort)))
}

func TestAutomaticTransportClosesWhenCanceledAfterBind(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var address string
	_, err := createMemberlist(ctx, automaticTransportConfig(), func(nc *memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		transport, err := memberlist.NewNetTransport(nc)
		if err == nil {
			address = net.JoinHostPort(nc.BindAddrs[0], strconv.Itoa(transport.GetAutoBindPort()))
			cancel()
		}
		return transport, err
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotEmpty(t, address)
	assertTransportPortReleased(t, address)
}

func TestFixedTransportDoesNotUseAutomaticAllocator(t *testing.T) {
	cfg := automaticTransportConfig()
	cfg.BindPort = -1
	_, err := createMemberlist(t.Context(), cfg, func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		t.Fatal("explicit port must not use automatic allocator")
		return nil, nil
	})
	require.Error(t, err)
	require.Equal(t, -1, cfg.BindPort)
}

func assertTransportPortReleased(t *testing.T, address string) {
	t.Helper()
	tcp, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
	require.NoError(t, err)
	defer tcp.Close()
	udp, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", address)
	require.NoError(t, err)
	require.NoError(t, udp.Close())
}
