// SPDX-License-Identifier: MPL-2.0

package net

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// NetworkKind is the base registry kind for all network overlay entries.
const NetworkKind registry.Kind = "network"

// Network provider kind constants.
const (
	KindTor       registry.Kind = "network.tor"
	KindI2P       registry.Kind = "network.i2p"
	KindTailscale registry.Kind = "network.tailscale"
	KindOpenVPN   registry.Kind = "network.openvpn"
)

// NetworkConfig holds common configuration for all network entries.
type NetworkConfig struct {
	Meta attrs.Bag `json:"meta,omitempty" msgpack:"meta,omitempty"`
	Host string    `json:"host" msgpack:"host"`
	Port int       `json:"port" msgpack:"port"`
}

// SetMeta sets the metadata for NetworkConfig.
func (c *NetworkConfig) SetMeta(meta attrs.Bag) {
	c.Meta = meta
}

// Validate checks that the common network config fields are set correctly.
func (c *NetworkConfig) Validate() error {
	if c.Host == "" {
		return ErrHostRequired
	}
	if c.Port <= 0 || c.Port > 65535 {
		return ErrInvalidPort
	}
	return nil
}

// TorConfig holds Tor SOCKS5 proxy configuration.
type TorConfig struct {
	NetworkConfig
	// IsolateStreams enables per-connection stream isolation.
	IsolateStreams bool `json:"isolate_streams,omitempty" msgpack:"isolate_streams,omitempty"`
}

// I2PConfig holds I2P SAM v3 bridge configuration.
type I2PConfig struct {
	SessionName string `json:"session_name,omitempty" msgpack:"session_name,omitempty"`
	NetworkConfig
}

// TailscaleConfig holds Tailscale tsnet node configuration.
type TailscaleConfig struct {
	Meta       attrs.Bag `json:"meta,omitempty" msgpack:"meta,omitempty"`
	Hostname   string    `json:"hostname,omitempty" msgpack:"hostname,omitempty"`
	AuthKey    string    `json:"auth_key,omitempty" msgpack:"auth_key,omitempty"`
	AuthKeyEnv string    `json:"auth_key_env,omitempty" msgpack:"auth_key_env,omitempty"`
	StateDir   string    `json:"state_dir,omitempty" msgpack:"state_dir,omitempty"`
	ControlURL string    `json:"control_url,omitempty" msgpack:"control_url,omitempty"`
	Ephemeral  bool      `json:"ephemeral,omitempty" msgpack:"ephemeral,omitempty"`
}

// SetMeta sets the metadata for TailscaleConfig.
func (c *TailscaleConfig) SetMeta(meta attrs.Bag) {
	c.Meta = meta
}

// Validate checks that the Tailscale config has the minimum required fields.
// Either AuthKey or AuthKeyEnv must be provided for non-interactive auth.
func (c *TailscaleConfig) Validate() error {
	if c.AuthKey == "" && c.AuthKeyEnv == "" {
		return ErrAuthKeyRequired
	}
	return nil
}

// OpenVPNConfig holds OpenVPN management interface configuration.
// OpenVPN creates a TUN interface and routes traffic through the VPN tunnel.
// This service connects to the OpenVPN management interface to query the
// VPN's assigned local IP, then binds outbound connections to that IP so
// the OS routing table directs them through the VPN tunnel.
type OpenVPNConfig struct {
	Meta attrs.Bag `json:"meta,omitempty" msgpack:"meta,omitempty"`
	// ManagementAddress is the TCP address of the OpenVPN management interface
	// (e.g. "127.0.0.1:7505"). Required.
	ManagementAddress string `json:"management_address" msgpack:"management_address"`
	// ManagementPassword is the optional password for the management interface.
	ManagementPassword string `json:"management_password,omitempty" msgpack:"management_password,omitempty"`
}

// SetMeta sets the metadata for OpenVPNConfig.
func (c *OpenVPNConfig) SetMeta(meta attrs.Bag) {
	c.Meta = meta
}

// Validate checks that the OpenVPN config has the minimum required fields.
func (c *OpenVPNConfig) Validate() error {
	if c.ManagementAddress == "" {
		return ErrManagementAddressRequired
	}
	return nil
}
