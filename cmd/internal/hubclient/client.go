// Package hubclient provides utilities for creating authenticated hub clients.
package hubclient

import (
	"fmt"
	"os"

	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
)

// Options configures hub client creation.
type Options struct {
	// RegistryURL overrides the default registry URL from auth config.
	// If empty, uses the default registry from stored credentials.
	RegistryURL string
	// ProjectDir is the directory to use for finding auth config.
	// If empty, uses the current working directory.
	ProjectDir string
}

// New creates a new hub client using stored credentials.
// Returns a client configured with the appropriate token for the registry.
func New(opts Options) (*hub.Client, error) {
	projectDir := opts.ProjectDir
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}

	authCfg := auth.NewConfig(projectDir)
	store := auth.NewStore(authCfg)

	registryURL := opts.RegistryURL
	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, _ := store.Get(registryURL)
	var token string
	if cred != nil {
		token = cred.Token
	}

	client, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return nil, fmt.Errorf("registry %s: %w", registryURL, err)
	}
	return client, nil
}

// NewDefault creates a hub client with default options.
// Uses current working directory and default registry URL.
func NewDefault() (*hub.Client, error) {
	return New(Options{})
}
