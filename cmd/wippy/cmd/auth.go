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
	Long: `Authenticate with the wippy registry using an API token.

Tokens can be created at https://modules.wippy.ai/settings/tokens

By default, credentials are stored globally in ~/.config/wippy/credentials.yaml.
Use --local to store credentials in the current project's .wippy/ directory.

Examples:
  wippy auth login                           # Interactive token prompt
  wippy auth login --token wpy_xxx           # Use token from flag
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
	Use:   "status",
	Short: "Show authentication status",
	RunE:  runAuthStatus,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)

	authLoginCmd.Flags().String("token", "", "API token")
	authLoginCmd.Flags().String("registry", "", "registry URL")
	authLoginCmd.Flags().Bool("local", false, "store in project config")

	authLogoutCmd.Flags().String("registry", "", "registry URL")
	authLogoutCmd.Flags().Bool("local", false, "remove from project config")

	authStatusCmd.Flags().Bool("json", false, "output as JSON")
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	isConsole := console

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
		// Scope intentionally left empty: the registry's /api/v1/account/orgs
		// validate endpoint does not echo the token's scope, and hard-coding
		// "read" here misled `wippy auth status` into reporting read-only for
		// publish/admin tokens. Human-readable status output omits the field
		// when unknown.
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
