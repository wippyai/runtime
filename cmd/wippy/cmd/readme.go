package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
)

var readmeCmd = &cobra.Command{
	Use:   "readme <org/module[@version]>",
	Short: "Fetch a module README from the hub",
	Long: `Fetch a module README from the wippy hub.

Examples:
  wippy readme wippy/terminal
  wippy readme wippy/terminal@1.2.3
  wippy readme --json wippy/terminal@latest`,
	Args: cobra.ExactArgs(1),
	RunE: runReadme,
}

func init() {
	rootCmd.AddCommand(readmeCmd)

	readmeCmd.Flags().Bool("json", false, "output as JSON")
	readmeCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runReadme(cmd *cobra.Command, args []string) error {
	moduleRef := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	registryURL, _ := cmd.Flags().GetString("registry")

	ref, err := parseModuleRef(moduleRef)
	if err != nil {
		return NewReadmeParseError(err)
	}

	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

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
		return NewReadmeClientError(registryURL, err)
	}

	params := &hub.GetReadmeParams{
		Org:    ref.Org,
		Module: ref.Module,
	}

	if ref.IsLabel {
		params.Label = ref.Version
	} else {
		params.Version = ref.Version
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	info, err := client.GetReadme(ctx, params)
	if err != nil {
		return NewReadmeError(moduleRef, registryURL, err)
	}

	if jsonOutput {
		return printReadmeJSON(ref, info)
	}

	return printReadmeText(ref, info)
}

func printReadmeJSON(ref *moduleRef, info *hub.ReadmeInfo) error {
	type readmeJSON struct {
		Org      string `json:"org"`
		Name     string `json:"name"`
		Filename string `json:"filename,omitempty"`
		Version  string `json:"version,omitempty"`
		Content  string `json:"content"`
	}

	data, err := json.MarshalIndent(readmeJSON{
		Org:      ref.Org,
		Name:     ref.Module,
		Filename: info.Filename,
		Version:  info.Version,
		Content:  info.Content,
	}, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

func printReadmeText(ref *moduleRef, info *hub.ReadmeInfo) error {
	fmt.Printf("Module: %s/%s\n", ref.Org, ref.Module)
	if info.Version != "" {
		fmt.Printf("Version: %s\n", info.Version)
	}
	if info.Filename != "" {
		fmt.Printf("File: %s\n", info.Filename)
	}
	fmt.Println()
	fmt.Print(info.Content)
	if len(info.Content) == 0 || info.Content[len(info.Content)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

func NewReadmeParseError(cause error) error {
	return fmt.Errorf("invalid module reference: %w", cause)
}

func NewReadmeClientError(registryURL string, cause error) error {
	return fmt.Errorf("failed to create hub client for %s: %w", registryURL, cause)
}

func NewReadmeError(moduleRef, registryURL string, cause error) error {
	return fmt.Errorf("readme fetch failed for %q on %s: %w", moduleRef, registryURL, cause)
}
