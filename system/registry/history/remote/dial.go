package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type DialConfig struct {
	Endpoint   string
	Token      string
	TokenFile  string
	CAFile     string
	ServerName string
	CertFile   string
	KeyFile    string
	Config
}

type bearerToken string

func (token bearerToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(token)}, nil
}
func (bearerToken) RequireTransportSecurity() bool { return true }

func Dial(ctx context.Context, cfg DialConfig) (*History, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("history endpoint is required")
	}
	credential := strings.TrimSpace(cfg.Token)
	if cfg.TokenFile != "" {
		token, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("read history token: %w", err)
		}
		credential = strings.TrimSpace(string(token))
	}
	if credential == "" {
		return nil, errors.New("history authentication is required: set WIPPY_TOKEN or run wippy auth login")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: cfg.ServerName}
	if cfg.CAFile != "" {
		ca, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read history CA: %w", err)
		}
		tlsConfig.RootCAs = x509.NewCertPool()
		if !tlsConfig.RootCAs.AppendCertsFromPEM(ca) {
			return nil, errors.New("history CA file has no certificates")
		}
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("read history client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = MaxMessageBytes
	}
	connection, err := grpc.NewClient(cfg.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithPerRPCCredentials(bearerToken(credential)), grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(cfg.MaxMessageBytes), grpc.MaxCallRecvMsgSize(cfg.MaxMessageBytes)))
	if err != nil {
		return nil, err
	}
	history, err := New(connection, cfg.Config)
	if err != nil {
		connection.Close()
		return nil, err
	}
	history.closer = connection
	if _, err := history.ReadPublished(ctx, 0); err != nil {
		connection.Close()
		return nil, fmt.Errorf("connect to history: %w", err)
	}
	return history, nil
}

func (h *History) Close() error {
	if h.closer != nil {
		return h.closer.Close()
	}
	return nil
}
