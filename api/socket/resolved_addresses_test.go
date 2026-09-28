// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResolvedAddresses_Bounds(t *testing.T) {
	t.Run("nil addresses", func(t *testing.T) {
		res, err := NewResolvedAddresses(nil)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Nil(t, res.DNSAddresses())
		require.NoError(t, res.Close())
	})

	t.Run("empty addresses", func(t *testing.T) {
		res, err := NewResolvedAddresses([]string{})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, []string{}, res.DNSAddresses())
		require.NoError(t, res.Close())
	})

	t.Run("exact max count 64", func(t *testing.T) {
		addrs := make([]string, MaxResolveAddresses)
		for i := range addrs {
			addrs[i] = "1.1.1.1"
		}
		res, err := NewResolvedAddresses(addrs)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Len(t, res.DNSAddresses(), MaxResolveAddresses)
		require.NoError(t, res.Close())
	})

	t.Run("exceeds max count 65", func(t *testing.T) {
		addrs := make([]string, MaxResolveAddresses+1)
		for i := range addrs {
			addrs[i] = "1.1.1.1"
		}
		res, err := NewResolvedAddresses(addrs)
		require.ErrorIs(t, err, ErrResolveLimit)
		assert.Nil(t, res)
	})

	t.Run("exact max bytes 4096", func(t *testing.T) {
		// Single address of exactly 4096 bytes
		longAddr := strings.Repeat("a", MaxResolveAddressBytes)
		res, err := NewResolvedAddresses([]string{longAddr})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, []string{longAddr}, res.DNSAddresses())
		require.NoError(t, res.Close())
	})

	t.Run("exceeds max bytes 4097 single address", func(t *testing.T) {
		longAddr := strings.Repeat("a", MaxResolveAddressBytes+1)
		res, err := NewResolvedAddresses([]string{longAddr})
		require.ErrorIs(t, err, ErrResolveLimit)
		assert.Nil(t, res)
	})

	t.Run("exceeds max bytes sum across multiple addresses", func(t *testing.T) {
		// 2 addresses summing to 4097 bytes
		addr1 := strings.Repeat("a", 2048)
		addr2 := strings.Repeat("b", 2049)
		res, err := NewResolvedAddresses([]string{addr1, addr2})
		require.ErrorIs(t, err, ErrResolveLimit)
		assert.Nil(t, res)
	})

	t.Run("exact sum across multiple addresses 4096 bytes", func(t *testing.T) {
		addr1 := strings.Repeat("a", 2048)
		addr2 := strings.Repeat("b", 2048)
		res, err := NewResolvedAddresses([]string{addr1, addr2})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, []string{addr1, addr2}, res.DNSAddresses())
		require.NoError(t, res.Close())
	})
}

func TestNewResolvedAddresses_SnapshotOwnership(t *testing.T) {
	input := []string{"192.168.1.1", "10.0.0.1"}
	res, err := NewResolvedAddresses(input)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Mutate input slice
	input[0] = "mutated.ip"
	input = append(input, "extra.ip")
	require.Len(t, input, 3)

	// Verify res.DNSAddresses() is isolated
	got := res.DNSAddresses()
	assert.Equal(t, []string{"192.168.1.1", "10.0.0.1"}, got)

	// Mutate got slice
	got[0] = "mutated.again"
	got2 := res.DNSAddresses()
	assert.Equal(t, []string{"192.168.1.1", "10.0.0.1"}, got2)

	require.NoError(t, res.Close())
}

func TestResolvedAddresses_CloseLifecycle(t *testing.T) {
	res, err := NewResolvedAddresses([]string{"127.0.0.1", "::1"})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, []string{"127.0.0.1", "::1"}, res.DNSAddresses())

	// Close clears references
	require.NoError(t, res.Close())
	assert.Nil(t, res.DNSAddresses())

	// Double Close is idempotent
	require.NoError(t, res.Close())
	assert.Nil(t, res.DNSAddresses())

	// Nil receiver safety
	var nilRes *ResolvedAddresses
	assert.Nil(t, nilRes.DNSAddresses())
	assert.NoError(t, nilRes.Close())
}

func TestResolvedAddresses_ConcurrentAccess(t *testing.T) {
	res, err := NewResolvedAddresses([]string{"1.2.3.4", "5.6.7.8"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id == 10 {
				_ = res.Close()
			} else {
				_ = res.DNSAddresses()
			}
		}(i)
	}
	wg.Wait()
	_ = res.Close()
}
