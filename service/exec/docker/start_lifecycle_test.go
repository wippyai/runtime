// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type startupTransport func(*http.Request) (*http.Response, error)

func (f startupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStartHasNoImplicitDeadline(t *testing.T) {
	refusal := errors.New("daemon refused create")
	cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
		_, deadline := r.Context().Deadline()
		require.False(t, deadline, "startup must use its owner's budget")
		return nil, refusal
	})}))
	require.NoError(t, err)
	defer cli.Close()
	p := &Process{cli: cli, log: zap.NewNop(), image: "fixture"}
	require.ErrorContains(t, p.Start(), refusal.Error())
	require.False(t, p.started)
}

func TestStartCanceledByLifecycle(t *testing.T) {
	entered := make(chan struct{})
	cli, err := client.New(client.WithHost("http://daemon.invalid"), client.WithAPIVersion("1.44"), client.WithHTTPClient(&http.Client{Transport: startupTransport(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}))
	require.NoError(t, err)
	defer cli.Close()
	p := &Process{cli: cli, log: zap.NewNop(), image: "fixture"}
	starter, ok := any(p).(interface{ StartContext(context.Context) error })
	require.True(t, ok, "Docker must support lifecycle cancellation")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- starter.StartContext(ctx) }()
	<-entered
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.False(t, p.started)
	require.Empty(t, p.containerID)
}
