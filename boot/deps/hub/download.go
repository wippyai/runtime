package hub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	downloadv1 "git.spiralscout.com/wippy/proto-go/wippy/api/hub/download/v1"
	modulev1 "git.spiralscout.com/wippy/proto-go/wippy/api/hub/module/v1"
	versionv1 "git.spiralscout.com/wippy/proto-go/wippy/api/hub/version/v1"
)

type DownloadInfo struct {
	URL       string
	ExpiresAt time.Time
	Digest    string
	Size      uint64
	Version   string
	Protected bool
}

type DownloadParams struct {
	Org     string
	Module  string
	Version string
	Label   string
}

func (c *Client) GetDownloadURL(ctx context.Context, params *DownloadParams) (*DownloadInfo, error) {
	req := &downloadv1.GetDownloadURLRequest{
		Module: &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_Name{
				Name: &modulev1.ModuleName{
					Org:  params.Org,
					Name: params.Module,
				},
			},
		},
	}

	if params.Version != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Version{
				Version: params.Version,
			},
		}
	} else if params.Label != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Label{
				Label: params.Label,
			},
		}
	}

	resp, err := c.Download.GetDownloadURL(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	info := &DownloadInfo{
		Version:   resp.Msg.Version,
		Protected: resp.Msg.Protected,
	}

	if resp.Msg.Download != nil {
		info.URL = resp.Msg.Download.Url
		info.Digest = resp.Msg.Download.Digest
		info.Size = resp.Msg.Download.SizeBytes
		if resp.Msg.Download.ExpiresAt != nil {
			info.ExpiresAt = resp.Msg.Download.ExpiresAt.AsTime()
		}
	}

	return info, nil
}

func (c *Client) DownloadToFile(ctx context.Context, url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("sync file: %w", err)
	}

	return f.Close()
}
