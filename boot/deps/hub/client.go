// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/tls"
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
	defaultTimeout         = 5 * time.Minute
	defaultMaxIdleConns    = 100
	defaultMaxIdlePerHost  = 10
	defaultIdleConnTimeout = 90 * time.Second
)

var sharedTransport = &http.Transport{
	TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
	},
	MaxIdleConns:        defaultMaxIdleConns,
	MaxIdleConnsPerHost: defaultMaxIdlePerHost,
	IdleConnTimeout:     defaultIdleConnTimeout,
}

type Client struct {
	Publish  publishv1connect.PublishServiceClient
	Module   modulev1connect.ModuleServiceClient
	Download downloadv1connect.DownloadServiceClient
	Manifest manifestv1connect.ManifestServiceClient

	tokenSource func() string
	httpClient  *http.Client
	baseURL     string
	token       string
}

type Options struct {
	// TokenSource, when set, overrides Token and is read before each Hub
	// request. An empty result sends no credential. It must be safe for
	// concurrent calls; the client never caches its result.
	TokenSource func() string
	BaseURL     string
	Token       string
	Timeout     time.Duration
}

func NewClient(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, NewClientConfigError("base URL is required", nil)
	}

	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, NewClientConfigError("invalid registry URL", err)
	}

	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1" ||
		// Container-side aliases for local Docker stacks: host.docker.internal
		// (Docker Desktop / OrbStack) and host.containers.internal (Podman).
		// They never resolve to anything but the host machine, so plaintext
		// HTTP across them is no riskier than localhost.
		host == "host.docker.internal" || host == "host.containers.internal"
	if u.Scheme != "https" && !isLocal {
		return nil, NewClientConfigError("registry must use HTTPS", nil)
	}

	baseURL := strings.TrimSuffix(opts.BaseURL, "/")

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport,
	}

	client := &Client{
		httpClient:  httpClient,
		baseURL:     baseURL,
		token:       opts.Token,
		tokenSource: opts.TokenSource,
	}
	var connectOpts []connect.ClientOption
	if opts.Token != "" || opts.TokenSource != nil {
		connectOpts = append(connectOpts, connect.WithInterceptors(
			newAuthInterceptor(client.currentToken),
		))
	}

	client.Publish = publishv1connect.NewPublishServiceClient(httpClient, baseURL, connectOpts...)
	client.Module = modulev1connect.NewModuleServiceClient(httpClient, baseURL, connectOpts...)
	client.Download = downloadv1connect.NewDownloadServiceClient(httpClient, baseURL, connectOpts...)
	client.Manifest = manifestv1connect.NewManifestServiceClient(httpClient, baseURL, connectOpts...)
	return client, nil
}

func (c *Client) currentToken() string {
	if c.tokenSource != nil {
		return c.tokenSource()
	}
	return c.token
}

func (c *Client) Upload(ctx context.Context, uploadURL, filePath string) error {
	return uploadFile(ctx, c.httpClient, uploadURL, filePath)
}

func newAuthInterceptor(tokenSource func() string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token := tokenSource(); token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			} else {
				req.Header().Del("Authorization")
			}
			req.Header().Set("User-Agent", "wippy-cli/"+version.Version)
			return next(ctx, req)
		}
	}
}
