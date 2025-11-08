package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// Encryption key for token encryption - CHANGE THIS IN PRODUCTION!
const encryptionKey = "change-this-32byte-secret-key!!!"

func generateToken(deploymentID, tokenType string) string {
	h := hmac.New(sha256.New, []byte(encryptionKey))
	h.Write([]byte(deploymentID + ":" + tokenType))
	return hex.EncodeToString(h.Sum(nil))
}

func generateConfigToken(deploymentID string) string {
	return generateToken(deploymentID, "config")
}

func generateStateToken(deploymentID string) string {
	return generateToken(deploymentID, "state")
}

type DeploymentConfig struct {
	Name          string            `json:"name"`
	StateFile     string            `json:"state_file"`
	EnvVariables  map[string]string `json:"env_variables"`
	RuntimeConfig []string          `json:"runtime_config"`
}

type RemoteToken struct {
	ConfigURL string `json:"config_url"`
	StateURL  string `json:"state_url"`
}

type ConfigServer struct {
	logger         *zap.Logger
	configs        map[string]*DeploymentConfig
	configTokenMap map[string]*DeploymentConfig // config_token -> config
	stateTokenMap  map[string]*DeploymentConfig // state_token -> config
	dataDir        string
	baseURL        string
}

func encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(encrypted string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func generateEncryptedToken(serverURL, deploymentID string) (string, error) {
	configToken := generateConfigToken(deploymentID)
	stateToken := generateStateToken(deploymentID)

	token := RemoteToken{
		ConfigURL: fmt.Sprintf("%s/config?token=%s", serverURL, configToken),
		StateURL:  fmt.Sprintf("%s/state?token=%s", serverURL, stateToken),
	}

	jsonData, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}

	key := []byte(encryptionKey)
	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	encrypted, err := encrypt(string(jsonData), key)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	return encrypted, nil
}

func loadConfigs(configFile string) (map[string]*DeploymentConfig, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var configs map[string]*DeploymentConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("unmarshal configs: %w", err)
	}

	return configs, nil
}

func NewConfigServer(configFile, dataDir, baseURL string, logger *zap.Logger) (*ConfigServer, error) {
	configs, err := loadConfigs(configFile)
	if err != nil {
		return nil, fmt.Errorf("load configs: %w", err)
	}

	configTokenMap := make(map[string]*DeploymentConfig)
	stateTokenMap := make(map[string]*DeploymentConfig)

	for deploymentID, config := range configs {
		configToken := generateConfigToken(deploymentID)
		stateToken := generateStateToken(deploymentID)

		configTokenMap[configToken] = config
		stateTokenMap[stateToken] = config
	}

	return &ConfigServer{
		logger:         logger,
		configs:        configs,
		configTokenMap: configTokenMap,
		stateTokenMap:  stateTokenMap,
		dataDir:        dataDir,
		baseURL:        baseURL,
	}, nil
}

func (s *ConfigServer) HandleConfig(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		s.logger.Warn("config request without token")
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	config, exists := s.configTokenMap[token]
	if !exists {
		s.logger.Warn("invalid config token", zap.String("token", token))
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Find deployment ID for this config
	var deploymentID string
	for id, cfg := range s.configs {
		if cfg == config {
			deploymentID = id
			break
		}
	}

	stateToken := generateStateToken(deploymentID)

	response := map[string]interface{}{
		"name":           config.Name,
		"state_url":      fmt.Sprintf("%s/state?token=%s", s.baseURL, stateToken),
		"env_variables":  config.EnvVariables,
		"runtime_config": config.RuntimeConfig,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("served config",
		zap.String("deployment", config.Name))
}

func (s *ConfigServer) HandleState(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		s.logger.Warn("state request without token")
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	config, exists := s.stateTokenMap[token]
	if !exists {
		s.logger.Warn("invalid state token", zap.String("token", token))
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	statePath := filepath.Join(s.dataDir, config.StateFile)
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		s.logger.Error("failed to read state file",
			zap.String("path", statePath),
			zap.Error(err))
		http.Error(w, "state file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(stateData)

	s.logger.Info("served state",
		zap.String("deployment", config.Name),
		zap.Int("size", len(stateData)))
}

func createLogger() (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	return config.Build()
}

var rootCmd = &cobra.Command{
	Use:   "config-server",
	Short: "Remote configuration and state server",
	Long:  "Standalone server to manage and serve deployment configurations",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")
		configFile, _ := cmd.Flags().GetString("config-file")
		dataDir, _ := cmd.Flags().GetString("data-dir")

		baseURL := fmt.Sprintf("http://%s:%d", host, port)

		server, err := NewConfigServer(configFile, dataDir, baseURL, logger)
		if err != nil {
			return fmt.Errorf("failed to create config server: %w", err)
		}

		http.HandleFunc("/config", server.HandleConfig)
		http.HandleFunc("/state", server.HandleState)

		addr := fmt.Sprintf(":%d", port)
		logger.Info("starting config server",
			zap.Int("port", port),
			zap.String("host", host),
			zap.String("base_url", baseURL),
			zap.String("config_file", configFile),
			zap.String("data_dir", dataDir),
			zap.Int("deployments", len(server.configs)))

		if err := http.ListenAndServe(addr, nil); err != nil {
			return fmt.Errorf("server failed: %w", err)
		}

		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all deployment configurations",
	RunE: func(cmd *cobra.Command, _ []string) error {
		configFile, _ := cmd.Flags().GetString("config-file")
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")

		baseURL := fmt.Sprintf("http://%s:%d", host, port)

		configs, err := loadConfigs(configFile)
		if err != nil {
			return fmt.Errorf("failed to load configs: %w", err)
		}

		if len(configs) == 0 {
			fmt.Println("No deployments configured")
			return nil
		}

		fmt.Printf("\n%-25s %-35s %-50s\n", "DEPLOYMENT ID", "NAME", "STATE FILE")
		fmt.Println(strings.Repeat("=", 110))

		for deploymentID, config := range configs {
			fmt.Printf("%-25s %-35s %-50s\n", deploymentID, config.Name, config.StateFile)

			configToken := generateConfigToken(deploymentID)
			stateToken := generateStateToken(deploymentID)

			encryptedToken, err := generateEncryptedToken(baseURL, deploymentID)
			if err != nil {
				fmt.Printf("  ERROR generating encrypted token: %v\n\n", err)
				continue
			}

			fmt.Printf("  Encrypted deployment token:\n  %s\n", encryptedToken)
			fmt.Printf("  Config URL: %s/config?token=%s\n", baseURL, configToken)
			fmt.Printf("  State URL:  %s/state?token=%s\n\n", baseURL, stateToken)
		}

		fmt.Printf("Total deployments: %d\n", len(configs))
		fmt.Printf("Server URL: %s\n", baseURL)
		fmt.Printf("Encryption key: %s\n\n", encryptionKey)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(listCmd)

	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
	serveCmd.Flags().StringP("host", "H", "localhost", "hostname or IP address")
	serveCmd.Flags().StringP("config-file", "c", "configs.json", "path to configs file")
	serveCmd.Flags().StringP("data-dir", "d", "states", "directory containing state dump files")

	listCmd.Flags().StringP("config-file", "c", "configs.json", "path to configs file")
	listCmd.Flags().StringP("host", "H", "localhost", "hostname or IP address for URLs")
	listCmd.Flags().IntP("port", "p", 8080, "port for URLs")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
