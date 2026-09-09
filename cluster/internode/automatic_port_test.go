// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestZeroPortReportsRetainedEndpoint(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(strconv.FormatBool(automatic), func(t *testing.T) {
			cfg := insecureManagerConfig()
			cfg.Logger = zap.NewNop()
			cfg.BindAddr, cfg.BindPort, cfg.AutoPort = "127.0.0.1", 0, automatic
			manager := NewConnectionManager(cfg, nil)
			require.NoError(t, manager.Start(context.Background(), func(cluster.NodeID, []byte) {}))
			defer manager.Stop()
			port := manager.GetListenPort()
			require.Positive(t, port)
			listener, err := net.Listen("tcp", net.JoinHostPort(cfg.BindAddr, strconv.Itoa(port)))
			if listener != nil {
				_ = listener.Close()
			}
			require.Error(t, err)
		})
	}
}
