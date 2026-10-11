// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"net"

	"github.com/moby/moby/client"
)

// The Moby hijack handshake reads directly from a socket and does not observe
// request cancellation after dialing. Use the configured daemon dialer (also
// preserving TLS) and close that socket if startup is canceled. Stop watching
// after the handshake so a successful attachment has its own stream lifetime.
func (p *Process) attachForStartup(ctx context.Context, options client.ContainerAttachOptions) (client.ContainerAttachResult, error) {
	stopWatch := func() bool { return true }
	defer func() { stopWatch() }()
	cli, err := client.New(
		client.WithHost(p.cli.DaemonHost()),
		client.WithAPIVersion(p.cli.ClientVersion()),
		client.WithDialContext(func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			conn, err := p.cli.Dialer()(dialCtx)
			if err != nil {
				return nil, err
			}
			stopWatch = context.AfterFunc(ctx, func() { _ = conn.Close() })
			return conn, nil
		}),
	)
	if err != nil {
		return client.ContainerAttachResult{}, err
	}
	defer cli.Close()
	attached, err := cli.ContainerAttach(ctx, p.containerID, options)
	if ctx.Err() != nil {
		if err == nil {
			attached.Close()
		}
		return client.ContainerAttachResult{}, ctx.Err()
	}
	return attached, err
}
