// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSourceDefaults(t *testing.T) {
	s := NewSource(SourceOptions{})
	assert.Equal(t, defaultStandbyInterval, s.standbyInterval)
	assert.Equal(t, defaultStatusInterval, s.statusInterval)
	assert.Equal(t, defaultSnapshotFetchSize, s.snapshotFetchSize)
	assert.NotNil(t, s.log)
}

func TestNewSourceHonorsOverrides(t *testing.T) {
	s := NewSource(SourceOptions{
		StandbyInterval:   1 * time.Second,
		StatusInterval:    2 * time.Second,
		SnapshotFetchSize: 4096,
	})
	assert.Equal(t, 1*time.Second, s.standbyInterval)
	assert.Equal(t, 2*time.Second, s.statusInterval)
	assert.Equal(t, 4096, s.snapshotFetchSize)
}

func TestStopBeforeStartIsSafe(t *testing.T) {
	s := NewSource(SourceOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))
	require.NoError(t, s.Stop(ctx))
}
