// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/hashicorp/memberlist"
)

func createMemberlist(ctx context.Context, cfg *memberlist.Config, open func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error)) (*memberlist.Memberlist, error) {
	if cfg.Transport != nil || cfg.BindPort != 0 {
		return memberlist.Create(cfg)
	}
	logger := cfg.Logger
	if logger == nil {
		output := cfg.LogOutput
		if output == nil {
			output = os.Stderr
		}
		logger = log.New(output, "", log.LstdFlags)
	}
	transportConfig := memberlist.NetTransportConfig{
		BindAddrs: []string{cfg.BindAddr}, Logger: logger, MetricLabels: cfg.MetricLabels,
	}
	var lastErr error
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt != 0 {
			// TCP's ephemeral allocator can walk a range unavailable to UDP
			// on Windows. Bind both protocols on a fresh candidate instead.
			candidate, err := rand.Int(rand.Reader, big.NewInt(16384))
			if err != nil {
				return nil, fmt.Errorf("choose membership port: %w", err)
			}
			transportConfig.BindPort = 49152 + int(candidate.Int64())
		}
		transport, err := open(&transportConfig)
		if err != nil {
			lastErr = err
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = transport.Shutdown()
			return nil, err
		}
		cfg.BindPort = transport.GetAutoBindPort()
		cfg.AdvertisePort = cfg.BindPort
		cfg.Transport = transport
		ml, err := memberlist.Create(cfg)
		if err != nil {
			_ = transport.Shutdown()
		}
		return ml, err
	}
	return nil, fmt.Errorf("could not allocate membership TCP/UDP listeners after 32 attempts: %w", lastErr)
}
