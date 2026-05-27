// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	downloadv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1/downloadv1connect"
	manifestv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
	publishv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/publish/v1/publishv1connect"
	"github.com/wippyai/runtime/api/version"
)

const (
	defaultTimeout = 5 * time.Minute
)

type Client struct {
	Publish  publishv1connect.PublishServiceClient
	Module   modulev1connect.ModuleServiceClient
	Download downloadv1connect.DownloadServiceClient
	Manifest manifestv1connect.ManifestServiceClient

	httpClient *http.Client
	baseURL    string
	token      string
}

type Options struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

func NewClient(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1" ||
		// Container-side aliases for local Docker stacks: host.docker.internal
		// (Docker Desktop / OrbStack) and host.containers.internal (Podman).
		// They never resolve to anything but the host machine, so plaintext
		// HTTP across them is no riskier than localhost.
		host == "host.docker.internal" || host == "host.containers.internal"
	if u.Scheme != "https" && !isLocal {
		return nil, fmt.Errorf("registry must use HTTPS")
	}

	baseURL := strings.TrimSuffix(opts.BaseURL, "/")

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	var connectOpts []connect.ClientOption
	if opts.Token != "" {
		connectOpts = append(connectOpts, connect.WithInterceptors(
			newAuthInterceptor(opts.Token),
		))
	}

	return &Client{
		Publish:    publishv1connect.NewPublishServiceClient(httpClient, baseURL, connectOpts...),
		Module:     modulev1connect.NewModuleServiceClient(httpClient, baseURL, connectOpts...),
		Download:   downloadv1connect.NewDownloadServiceClient(httpClient, baseURL, connectOpts...),
		Manifest:   manifestv1connect.NewManifestServiceClient(httpClient, baseURL, connectOpts...),
		httpClient: httpClient,
		baseURL:    baseURL,
		token:      opts.Token,
	}, nil
}

func (c *Client) Upload(ctx context.Context, uploadURL, filePath string) error {
	return uploadFile(ctx, c.httpClient, uploadURL, filePath)
}

func newAuthInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			req.Header().Set("User-Agent", "wippy-cli/"+version.Version)
			return next(ctx, req)
		}
	}
}
