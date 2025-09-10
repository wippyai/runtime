package main

import (
	"net/http"
	"os"

	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
)

// ClientFactory creates HTTP clients for module registry services
type ClientFactory struct {
	baseURL string
	client  *http.Client
}

// NewClientFactory creates a new client factory
func NewClientFactory() *ClientFactory {
	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	return &ClientFactory{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// CreateClients creates all necessary clients for module operations
func (cf *ClientFactory) CreateClients() *ModuleClients {
	return &ModuleClients{
		Organization: identityv1connect.NewOrganizationServiceClient(cf.client, cf.baseURL),
		Module:       modulev1connect.NewModuleServiceClient(cf.client, cf.baseURL),
		Label:        modulev1connect.NewLabelServiceClient(cf.client, cf.baseURL),
		Commit:       modulev1connect.NewCommitServiceClient(cf.client, cf.baseURL),
		Download:     modulev1connect.NewDownloadServiceClient(cf.client, cf.baseURL),
	}
}

// ModuleClients holds all the module registry clients
type ModuleClients struct {
	Organization identityv1connect.OrganizationServiceClient
	Module       modulev1connect.ModuleServiceClient
	Label        modulev1connect.LabelServiceClient
	Commit       modulev1connect.CommitServiceClient
	Download     modulev1connect.DownloadServiceClient
}
