// SPDX-License-Identifier: MPL-2.0

// Command hubpublish registers a published GitHub release with hub.wippy.ai so
// the hub keeps the release record and download counter while the binaries are
// served from GitHub. For each binary it computes the SHA256, an Ed25519
// signature over that digest (verified by the hub), and the GitHub asset URL,
// then POSTs the set to the hub's internal release endpoint.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	Platform              string `json:"platform"`
	Arch                  string `json:"arch"`
	Filename              string `json:"filename"`
	URL                   string `json:"url"`
	SHA256                string `json:"sha256"`
	Signature             string `json:"signature"`
	SigningKeyFingerprint string `json:"signing_key_fingerprint"`
	SizeBytes             int64  `json:"size_bytes"`
}

type payload struct {
	Version    string  `json:"version"`
	Changelog  string  `json:"changelog"`
	Assets     []asset `json:"assets"`
	Prerelease bool    `json:"prerelease"`
}

func loadKey(hexKey string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("decode hex key: %w", err)
	}

	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("key is %d bytes, want %d or %d", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func fingerprint(priv ed25519.PrivateKey) (string, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("derived public key is not ed25519")
	}

	sum := sha256.Sum256(pub)

	return hex.EncodeToString(sum[:]), nil
}

// platformArch derives the hub platform/arch from a wippy-<os>-<arch>[.exe] name.
func platformArch(filename string) (string, string, error) {
	name := strings.TrimSuffix(filename, ".exe")
	name = strings.TrimPrefix(name, "wippy-")
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot derive platform/arch from %q", filename)
	}

	return parts[0], parts[1], nil
}

func buildAsset(priv ed25519.PrivateKey, fp, repo, tag, path string) (asset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return asset{}, err
	}

	filename := filepath.Base(path)
	platform, arch, err := platformArch(filename)
	if err != nil {
		return asset{}, err
	}

	digest := sha256.Sum256(data)
	sig := ed25519.Sign(priv, digest[:])

	return asset{
		Platform:              platform,
		Arch:                  arch,
		Filename:              filename,
		URL:                   fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, filename),
		SHA256:                hex.EncodeToString(digest[:]),
		SizeBytes:             int64(len(data)),
		Signature:             base64.StdEncoding.EncodeToString(sig),
		SigningKeyFingerprint: fp,
	}, nil
}

func buildPayload(priv ed25519.PrivateKey, repo, tag, changelog string, prerelease bool, paths []string) (payload, error) {
	fp, err := fingerprint(priv)
	if err != nil {
		return payload{}, err
	}

	assets := make([]asset, 0, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, ".sig") {
			continue
		}

		a, err := buildAsset(priv, fp, repo, tag, path)
		if err != nil {
			return payload{}, fmt.Errorf("asset %s: %w", path, err)
		}

		assets = append(assets, a)
	}

	if len(assets) == 0 {
		return payload{}, errors.New("no binaries to publish")
	}

	return payload{
		Version:    strings.TrimPrefix(tag, "v"),
		Changelog:  changelog,
		Prerelease: prerelease,
		Assets:     assets,
	}, nil
}

func post(hubURL, secret string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/internal/v1/releases/github", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-service-secret", secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub responded %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	fmt.Printf("hub: %s\n", strings.TrimSpace(string(respBody)))

	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("provide at least one binary to publish")
	}

	priv, err := loadKey(os.Getenv("RELEASE_SIGNING_KEY"))
	if err != nil {
		return err
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	tag := os.Getenv("RELEASE_TAG")
	if repo == "" || tag == "" {
		return errors.New("GITHUB_REPOSITORY and RELEASE_TAG are required")
	}

	p, err := buildPayload(priv, repo, tag, os.Getenv("CHANGELOG"), os.Getenv("PRERELEASE") == "true", args)
	if err != nil {
		return err
	}

	body, err := json.Marshal(p)
	if err != nil {
		return err
	}

	if os.Getenv("DRY_RUN") == "true" {
		fmt.Println(string(body))
		return nil
	}

	secret := os.Getenv("HUB_SERVICE_SECRET")
	if secret == "" {
		return errors.New("HUB_SERVICE_SECRET is required")
	}

	hubURL := os.Getenv("HUB_URL")
	if hubURL == "" {
		hubURL = "https://hub.wippy.ai"
	}

	return post(hubURL, secret, body)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hubpublish:", err)
		os.Exit(1)
	}
}
