// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShutdownWaitsForActiveActionResult(t *testing.T) {
	s := NewService("shutdown", nil, nil)
	_, err := s.Start(context.Background())
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.submitAndWait(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	s.cancel()
	select {
	case <-result:
		close(release)
		t.Fatal("returned before active action completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-result)
	require.NoError(t, s.Stop(context.Background()))
}
