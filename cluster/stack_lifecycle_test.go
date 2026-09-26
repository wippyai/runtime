// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

// A stack is single-use: once stopped it refuses to start again, and a new
// execution assembles a new stack.
func TestStackIsSingleUse(t *testing.T) {
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	stack, err := AssembleStack(StackConfig{
		NodeName: "single-use", Logger: zap.NewNop(), Bus: eventbus.NewBus(),
		Transcoder: payload.NewTranscoder(), Collector: collector,
		MembershipBindAddr: "127.0.0.1", InternodeBindAddr: "127.0.0.1",
		SecretKey:                base64.StdEncoding.EncodeToString(secret),
		InternodeIdentityKey:     base64.RawStdEncoding.EncodeToString(key),
		InternodeTrustedPeerKeys: map[string]string{"single-use": base64.RawStdEncoding.EncodeToString(pub)},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, stack.Start(ctx))
	require.NoError(t, stack.Stop())
	require.ErrorContains(t, stack.Start(ctx), "single-use")
}
