// SPDX-License-Identifier: MPL-2.0

package net

import "errors"

var (
	ErrHostRequired       = errors.New("network: host is required")
	ErrInvalidPort        = errors.New("network: invalid port")
	ErrNetworkNotFound    = errors.New("network: service not found")
	ErrNetworkUnavailable = errors.New("network: service unavailable")
	ErrNotSupported       = errors.New("network: operation not supported by this provider")
	ErrAuthKeyRequired    = errors.New("network: tailscale auth_key or auth_key_env is required")
	ErrAccessDenied       = errors.New("network: access denied")
)
