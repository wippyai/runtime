// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"sync"
	"testing"

	authapi "github.com/wippyai/runtime/api/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeToken_OverridesFile(t *testing.T) {
	store, _ := newTestStore(t)
	reg := "https://override-file.example.com"

	require.NoError(t, store.Set(&authapi.Credential{Token: "wpy_filetoken1234567890", Registry: reg}, false))

	SetRuntimeToken(reg, "wpy_runtimetoken123456789")
	t.Cleanup(func() { ClearRuntimeToken(reg) })

	got, err := store.Get(reg)
	require.NoError(t, err)
	assert.Equal(t, "wpy_runtimetoken123456789", got.Token)
	assert.Equal(t, reg, got.Registry)
}

func TestRuntimeToken_BeatsEnv(t *testing.T) {
	store, _ := newTestStore(t)
	reg := "https://override-env.example.com"

	t.Setenv(EnvToken, "wpy_envtoken123456789012")

	SetRuntimeToken(reg, "wpy_runtimetoken123456789")
	t.Cleanup(func() { ClearRuntimeToken(reg) })

	got, err := store.Get(reg)
	require.NoError(t, err)
	assert.Equal(t, "wpy_runtimetoken123456789", got.Token)
}

func TestRuntimeToken_PerRegistry(t *testing.T) {
	store, _ := newTestStore(t)
	regA := "https://a.example.com"
	regB := "https://b.example.com"

	SetRuntimeToken(regA, "wpy_tokenA12345678901234")
	t.Cleanup(func() { ClearRuntimeToken(regA) })

	got, err := store.Get(regA)
	require.NoError(t, err)
	assert.Equal(t, "wpy_tokenA12345678901234", got.Token)

	_, err = store.Get(regB)
	assert.Equal(t, authapi.ErrNotAuthenticated, err)
}

func TestRuntimeToken_GetEmptyResolvesToDefaultKey(t *testing.T) {
	store, _ := newTestStore(t)

	SetRuntimeToken(store.DefaultRegistry(), "wpy_defaulttoken12345678")
	t.Cleanup(func() { ClearRuntimeToken(store.DefaultRegistry()) })

	got, err := store.Get(DefaultRegistry)
	require.NoError(t, err)
	assert.Equal(t, "wpy_defaulttoken12345678", got.Token)

	got, err = store.Get("")
	require.NoError(t, err)
	assert.Equal(t, "wpy_defaulttoken12345678", got.Token)
}

func TestRuntimeToken_Clear(t *testing.T) {
	store, _ := newTestStore(t)
	reg := "https://clear.example.com"

	SetRuntimeToken(reg, "wpy_cleartoken1234567890")
	got, err := store.Get(reg)
	require.NoError(t, err)
	assert.Equal(t, "wpy_cleartoken1234567890", got.Token)

	ClearRuntimeToken(reg)

	_, err = store.Get(reg)
	assert.Equal(t, authapi.ErrNotAuthenticated, err)
}

func TestRuntimeToken_Concurrent(t *testing.T) {
	reg := "https://concurrent.example.com"
	t.Cleanup(func() { ClearRuntimeToken(reg) })

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(3)

		go func() {
			defer wg.Done()
			SetRuntimeToken(reg, "wpy_concurrenttoken12345")
		}()

		go func() {
			defer wg.Done()
			_ = runtimeToken(reg)
		}()

		go func() {
			defer wg.Done()
			ClearRuntimeToken(reg)
		}()
	}

	wg.Wait()
}
