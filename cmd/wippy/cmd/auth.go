// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/auth"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"golang.org/x/term"
)

var (
	authLabelStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	authSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	authErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	authInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	authDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// authPrintf prints formatted output with optional styling for console mode.
func authPrintf(console bool, format string, style lipgloss.Style, args ...any) {
	if console {
		styled := style.Render(fmt.Sprintf(format, args...))
		fmt.Println(styled)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}

// authPrintField prints a label: value pair with appropriate styling.
func authPrintField(console bool, label, value string, valueStyle lipgloss.Style) {
	if console {
		fmt.Printf("%s %s\n", authLabelStyle.Render(label), valueStyle.Render(value))
	} else {
		fmt.Printf("%s %s\n", label, value)
	}
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage registry authentication",
	Long: `Manage authentication with the wippy registry.

Examples:
  wippy auth login                    # Login with interactive token prompt
  wippy auth login --token wpy_xxx    # Login with token from flag
  wippy auth status                   # Check current authentication status
  wippy auth logout                   # Remove stored credentials`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with the registry",
	Long: `Authenticate with the wippy registry using an API token or SSH key.

API tokens can be created at https://modules.wippy.ai/settings/tokens.
SSH keys can be registered under Account > SSH Keys; the same key used for
'git push' works for 'wippy publish'.

By default, credentials are stored globally in ~/.config/wippy/credentials.yaml.
Use --local to store credentials in the current project's .wippy/ directory.

Examples:
  wippy auth login                           # Interactive token prompt
  wippy auth login --token wpy_xxx           # Use token from flag
  wippy auth login --ssh                     # SSH key (default key in ~/.ssh)
  wippy auth login --ssh --key ~/.ssh/wippy  # Specific SSH key
  wippy auth login --registry https://...    # Use custom registry
  wippy auth login --local                   # Store in project config`,
	RunE: runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	RunE:  runAuthLogout,
}

var authStatusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"whoami"},
	Short:   "Show authentication status",
	RunE:    runAuthStatus,
}

var authTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Print the stored token (for use in scripts and Docker secrets)",
	Long: `Print the stored access token for the configured registry.

Resolution order matches every other auth-aware command:
WIPPY_TOKEN env > project-local credentials > global credentials.

Examples:
  wippy auth token                    # Print token to stdout
  wippy auth token --registry https://hub.example.com`,
	RunE: runAuthToken,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authTokenCmd)

	authLoginCmd.Flags().String("token", "", "API token")
	authLoginCmd.Flags().String("registry", "", "registry URL")
	authLoginCmd.Flags().Bool("local", false, "store in project config")
	authLoginCmd.Flags().Bool("ssh", false, "authenticate using a registered SSH key")
	authLoginCmd.Flags().String("key", "", "path to SSH private key (default: ~/.ssh/id_ed25519, id_ecdsa, id_rsa)")

	authLogoutCmd.Flags().String("registry", "", "registry URL")
	authLogoutCmd.Flags().Bool("local", false, "remove from project config")

	authStatusCmd.Flags().Bool("json", false, "output as JSON")

	authTokenCmd.Flags().String("registry", "", "registry URL")
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	isConsole := console

	useSSH, _ := cmd.Flags().GetBool("ssh")
	keyPath, _ := cmd.Flags().GetString("key")
	if keyPath != "" && !useSSH {
		// Specifying --key implies --ssh; saves the user a flag.
		useSSH = true
	}

	token, _ := cmd.Flags().GetString("token")
	registry, _ := cmd.Flags().GetString("registry")
	local, _ := cmd.Flags().GetBool("local")

	projectDir, err := os.Getwd()
	if err != nil {
		return bootauth.NewTokenReadError(err)
	}

	cfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(cfg)

	if registry == "" {
		registry = store.DefaultRegistry()
	}

	if useSSH {
		return runAuthLoginSSH(cmd, store, registry, keyPath, local, isConsole)
	}

	if token == "" {
		token, err = readTokenInteractive(registry)
		if err != nil {
			return bootauth.NewTokenReadError(err)
		}
	}

	if token == "" {
		return bootauth.NewTokenEmptyError()
	}

	if err := bootauth.ValidateTokenFormat(token); err != nil {
		return bootauth.NewTokenInvalidError(err)
	}

	client, err := bootauth.NewClient(registry)
	if err != nil {
		return bootauth.NewClientError(registry, err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	result, err := client.Validate(ctx, token)
	if err != nil {
		return bootauth.NewValidationError(registry, err)
	}

	cred := &auth.Credential{
		Token:    token,
		Registry: registry,
		Scope:    auth.ScopeRead,
	}

	if len(result.Orgs) > 0 {
		cred.Orgs = make([]string, len(result.Orgs))
		for i, org := range result.Orgs {
			cred.Orgs[i] = org.Name
		}
	}

	if err := store.Set(cred, !local); err != nil {
		return bootauth.NewStoreError(registry, err)
	}

	printLoginSuccess(registry, cred.Orgs, local, isConsole)
	return nil
}

// runAuthLoginSSH performs the SSH challenge-response handshake against the
// registry and persists the resulting short-lived JWT alongside the path of
// the key that produced it, so future commands can refresh transparently.
func runAuthLoginSSH(cmd *cobra.Command, store *bootauth.Store, registry, keyPath string, local, isConsole bool) error {
	if keyPath == "" {
		keyPath = bootauth.SSHKeyFromEnv()
	}
	if keyPath == "" {
		keyPath = bootauth.FindDefaultSSHKey()
	}
	if keyPath == "" {
		return fmt.Errorf("no ssh key found; pass --key or set %s", bootauth.EnvSSHKey)
	}

	signer, err := bootauth.LoadSSHSigner(keyPath, sshPassphrasePrompter)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	result, err := bootauth.ExchangeSSHForToken(ctx, registry, signer)
	if err != nil {
		return err
	}

	cred := &auth.Credential{
		Token:     result.Token,
		Registry:  registry,
		Scope:     auth.ScopeRead,
		ExpiresAt: result.ExpiresAt,
	}
	if err := store.SetWithSSHKey(cred, signer.KeyPath(), !local); err != nil {
		return bootauth.NewStoreError(registry, err)
	}

	printSSHLoginSuccess(registry, signer.Fingerprint(), result.ExpiresAt, local, isConsole)
	return nil
}

func sshPassphrasePrompter(keyPath string) ([]byte, error) {
	fmt.Printf("Passphrase for %s: ", keyPath)
	bytesPass, err := term.ReadPassword(stdinFd())
	fmt.Println()
	if err != nil {
		return nil, err
	}
	return bytesPass, nil
}

func printSSHLoginSuccess(registry, fingerprint string, expiresAt time.Time, local, isConsole bool) {
	authPrintf(isConsole, "Logged in via SSH key", authSuccessStyle)
	if isConsole {
		fmt.Printf("  Registry:    %s\n", authInfoStyle.Render(registry))
		fmt.Printf("  Fingerprint: %s\n", authDimStyle.Render(fingerprint))
		if !expiresAt.IsZero() {
			fmt.Printf("  Token TTL:   %s (auto-refreshed using the same key)\n", authDimStyle.Render(time.Until(expiresAt).Round(time.Minute).String()))
		}
		storage := "global config"
		if local {
			storage = "project config"
		}
		fmt.Printf("  Stored in:   %s\n", authInfoStyle.Render(storage))
		return
	}
	fmt.Printf("Registry: %s\n", registry)
	fmt.Printf("Fingerprint: %s\n", fingerprint)
}

func runAuthLogout(cmd *cobra.Command, _ []string) error {
	isConsole := console

	registry, _ := cmd.Flags().GetString("registry")
	local, _ := cmd.Flags().GetBool("local")

	projectDir, _ := os.Getwd()
	cfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(cfg)

	if registry == "" {
		registry = store.DefaultRegistry()
	}

	if err := store.Remove(registry, !local); err != nil {
		return bootauth.NewRemoveError(registry, err)
	}

	if isConsole {
		fmt.Printf("%s from %s\n", authSuccessStyle.Render("Logged out"), registry)
	} else {
		fmt.Printf("Logged out from %s\n", registry)
	}

	return nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	isConsole := console

	jsonOutput, _ := cmd.Flags().GetBool("json")

	projectDir, _ := os.Getwd()
	cfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(cfg)
	registry := store.DefaultRegistry()

	cred, err := store.Get(registry)
	if err != nil || cred == nil {
		if jsonOutput {
			return printStatusJSON(registry, nil, err)
		}
		return printStatusTable(registry, nil, err, isConsole)
	}

	// Validate token against server
	client, clientErr := bootauth.NewClient(registry)
	if clientErr != nil {
		if jsonOutput {
			return printStatusJSON(registry, cred, clientErr)
		}
		return printStatusTable(registry, cred, clientErr, isConsole)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	result, validateErr := client.Validate(ctx, cred.Token)
	if validateErr != nil {
		if jsonOutput {
			return printStatusJSON(registry, nil, validateErr)
		}
		return printStatusTable(registry, nil, validateErr, isConsole)
	}

	// Update orgs from server response
	if len(result.Orgs) > 0 {
		cred.Orgs = make([]string, len(result.Orgs))
		for i, org := range result.Orgs {
			cred.Orgs[i] = org.Name
		}
	}

	if jsonOutput {
		return printStatusJSON(registry, cred, nil)
	}

	return printStatusTable(registry, cred, nil, isConsole)
}

func runAuthToken(cmd *cobra.Command, _ []string) error {
	registry, _ := cmd.Flags().GetString("registry")

	projectDir, _ := os.Getwd()
	cfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(cfg)

	if registry == "" {
		registry = store.DefaultRegistry()
	}

	cred, err := store.Get(registry)
	if err != nil || cred == nil || cred.Token == "" {
		return bootauth.NewTokenReadError(fmt.Errorf("no token stored for %s", registry))
	}

	fmt.Println(cred.Token)
	return nil
}

func readTokenInteractive(registry string) (string, error) {
	fmt.Printf("Enter API token for %s: ", registry)

	tokenBytes, err := term.ReadPassword(stdinFd())
	if err != nil {
		reader := bufio.NewReader(os.Stdin)
		token, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(token), nil
	}

	fmt.Println()
	return string(tokenBytes), nil
}

func printLoginSuccess(registry string, orgs []string, local bool, console bool) {
	authPrintf(console, "Logged in successfully", authSuccessStyle)
	if console {
		fmt.Printf("  Registry: %s\n", authInfoStyle.Render(registry))
		if len(orgs) > 0 {
			fmt.Printf("  Organizations: %s\n", authInfoStyle.Render(strings.Join(orgs, ", ")))
		}
		storage := "global config"
		if local {
			storage = "project config"
		}
		fmt.Printf("  Stored in: %s\n", authInfoStyle.Render(storage))
	} else {
		fmt.Printf("Registry: %s\n", registry)
		if len(orgs) > 0 {
			fmt.Printf("Organizations: %s\n", strings.Join(orgs, ", "))
		}
	}
}

func printStatusJSON(registry string, cred *auth.Credential, err error) error {
	status := map[string]any{
		"authenticated": cred != nil && err == nil,
		"registry":      registry,
	}

	if cred != nil && err == nil {
		status["scope"] = string(cred.Scope)
		status["orgs"] = cred.Orgs
		status["expired"] = cred.IsExpired()
		if !cred.ExpiresAt.IsZero() {
			status["expires_at"] = cred.ExpiresAt.Format(time.RFC3339)
		}
	}

	if err != nil {
		status["error"] = err.Error()
	}

	data, jsonErr := json.MarshalIndent(status, "", "  ")
	if jsonErr != nil {
		return jsonErr
	}
	fmt.Println(string(data))
	return nil
}

func printStatusTable(registry string, cred *auth.Credential, err error, console bool) error {
	authPrintField(console, "Registry:", registry, authInfoStyle)

	if err != nil || cred == nil {
		authPrintField(console, "Status:", "Not authenticated", authErrorStyle)
		return nil
	}

	if cred.IsExpired() {
		authPrintField(console, "Status:", "Token expired", authErrorStyle)
	} else {
		authPrintField(console, "Status:", "Authenticated", authSuccessStyle)
	}

	if cred.Scope != "" {
		authPrintField(console, "Scope:", string(cred.Scope), authInfoStyle)
	}

	if len(cred.Orgs) > 0 {
		authPrintField(console, "Organizations:", strings.Join(cred.Orgs, ", "), authInfoStyle)
	}

	if !cred.ExpiresAt.IsZero() {
		remaining := time.Until(cred.ExpiresAt)
		var expiresStr string
		if remaining > 0 {
			if remaining > 24*time.Hour {
				expiresStr = fmt.Sprintf("in %d days", int(remaining.Hours()/24))
			} else {
				expiresStr = fmt.Sprintf("in %s", remaining.Round(time.Minute))
			}
		} else {
			expiresStr = "expired"
		}
		authPrintField(console, "Expires:", fmt.Sprintf("%s (%s)", cred.ExpiresAt.Format("2006-01-02"), expiresStr), authDimStyle)
	}

	return nil
}
