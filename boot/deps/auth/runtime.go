// SPDX-License-Identifier: MPL-2.0

package auth

import "sync"

var (
	runtimeMu     sync.RWMutex
	runtimeTokens = map[string]string{}
)

func SetRuntimeToken(registry, token string) {
	runtimeMu.Lock()
	runtimeTokens[registry] = token
	runtimeMu.Unlock()
}

func ClearRuntimeToken(registry string) {
	runtimeMu.Lock()
	delete(runtimeTokens, registry)
	runtimeMu.Unlock()
}

func runtimeToken(registry string) string {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()

	return runtimeTokens[registry]
}
