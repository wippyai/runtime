package temporal

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	System events.System = "temporal"

	KindClient   registry.Kind = "temporal.client"
	KindActivity registry.Kind = "temporal.activity"
	KindWorkflow registry.Kind = "temporal.workflow"
)

// ClientConfig represents configuration for a Temporal client connection to a desired namespace.
type ClientConfig struct {
	Meta      registry.Metadata `json:"meta"`
	Address   string            `json:"address"`   // Temporal server address
	Namespace string            `json:"namespace"` // Temporal namespace
	TLS       *TLSConfig        `json:"tls"`       // TLS configuration
}

// TLSConfig represents TLS/SSL configuration
type TLSConfig struct {
	Key        string         `mapstructure:"key"`
	Cert       string         `mapstructure:"cert"`
	RootCA     string         `mapstructure:"root_ca"`
	AuthType   ClientAuthType `mapstructure:"client_auth_type"`
	ServerName string         `mapstructure:"server_name"`
	UseH2C     bool           `mapstructure:"use_h2c"`
}

type ClientAuthType string

const (
	NoClientCert               ClientAuthType = "no_client_cert"
	RequestClientCert          ClientAuthType = "request_client_cert"
	RequireAnyClientCert       ClientAuthType = "require_any_client_cert"
	VerifyClientCertIfGiven    ClientAuthType = "verify_client_cert_if_given"
	RequireAndVerifyClientCert ClientAuthType = "require_and_verify_client_cert"
)
