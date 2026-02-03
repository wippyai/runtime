package auth

import (
	"os"
	"path/filepath"
)

const (
	LocalCredentialsDir   = ".wippy"
	LocalCredentialsFile  = "credentials.yaml"
	GlobalCredentialsDir  = "wippy"
	GlobalCredentialsFile = "credentials.yaml"

	EnvToken    = "WIPPY_TOKEN"
	EnvRegistry = "WIPPY_REGISTRY"

	DefaultRegistry = "https://hub.wippy.ai"

	TokenPrefix    = "wpy_"
	TokenMinLength = 20
)

// Config holds authentication configuration.
type Config struct {
	ProjectDir string
	GlobalDir  string
}

// NewConfig creates auth configuration for the given project directory.
func NewConfig(projectDir string) *Config {
	return &Config{
		ProjectDir: projectDir,
		GlobalDir:  globalConfigDir(),
	}
}

// LocalPath returns the project-local credentials file path.
func (c *Config) LocalPath() string {
	if c.ProjectDir == "" {
		return ""
	}
	return filepath.Join(c.ProjectDir, LocalCredentialsDir, LocalCredentialsFile)
}

// GlobalPath returns the global credentials file path.
func (c *Config) GlobalPath() string {
	if c.GlobalDir == "" {
		return ""
	}
	return filepath.Join(c.GlobalDir, GlobalCredentialsFile)
}

// TokenFromEnv returns the token from environment variable if set.
func TokenFromEnv() string {
	return os.Getenv(EnvToken)
}

// RegistryFromEnv returns the registry URL from environment variable if set.
func RegistryFromEnv() string {
	return os.Getenv(EnvRegistry)
}

func globalConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, GlobalCredentialsDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", GlobalCredentialsDir)
	}
	return ""
}
