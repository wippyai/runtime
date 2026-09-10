// SPDX-License-Identifier: MPL-2.0

package system

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cluster/internode"
)

// clusterTLSConfig selects the same TLS implementation used by native stacks.
// Invalid explicit configuration never silently selects plaintext.
func clusterTLSConfig(cluster boot.Config) (internode.ManagerTLSConfig, error) {
	if _, present := cluster.Get("internode.tls"); present {
		return internode.ManagerTLSConfig{}, fmt.Errorf("cluster.internode.tls requires named settings such as enabled and cert_file, not a root value")
	}
	cfg := cluster.Sub("internode.tls")
	var result internode.ManagerTLSConfig
	for _, key := range cfg.Keys() {
		raw, _ := cfg.Get(key)
		switch key {
		case "enabled":
			enabled, ok := raw.(bool)
			if !ok {
				return result, fmt.Errorf("cluster.internode.tls.enabled must be a boolean")
			}
			result.Enabled = enabled
		case "cert_file", "key_file", "ca_file":
			path, ok := raw.(string)
			if !ok || strings.TrimSpace(path) == "" {
				return result, fmt.Errorf("cluster.internode.tls.%s must be a nonempty path", key)
			}
			switch key {
			case "cert_file":
				result.CertFile = path
			case "key_file":
				result.KeyFile = path
			case "ca_file":
				result.CAFile = path
			}
		default:
			return result, fmt.Errorf("unknown cluster.internode.tls setting %q", key)
		}
	}
	if result.Enabled && (result.CertFile == "" || result.KeyFile == "" || result.CAFile == "") {
		return result, fmt.Errorf("cluster.internode.tls requires cert_file, key_file and ca_file when enabled")
	}
	if !result.Enabled && (result.CertFile != "" || result.KeyFile != "" || result.CAFile != "") {
		return result, fmt.Errorf("cluster.internode.tls credential paths require enabled=true")
	}
	return result, nil
}
