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
	downloadv1connect "git.spiralscout.com/wippy/proto-go/wippy/api/hub/download/v1/downloadv1connect"
	manifestv1connect "git.spiralscout.com/wippy/proto-go/wippy/api/hub/manifest/v1/manifestv1connect"
	modulev1connect "git.spiralscout.com/wippy/proto-go/wippy/api/hub/module/v1/modulev1connect"
	publishv1connect "git.spiralscout.com/wippy/proto-go/wippy/api/hub/publish/v1/publishv1connect"
	"github.com/wippyai/runtime/cmd/wippy/version"
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

	isLocal := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
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
	}, nil
}

func (c *Client) Upload(uploadURL, filePath string) error {
	return uploadFile(c.httpClient, uploadURL, filePath)
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
