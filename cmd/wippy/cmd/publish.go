package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/pack"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/wippy/version"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish module to the hub",
	Long: `Publish a module to the wippy hub.

Reads configuration from wippy.yaml in the current directory,
packs the module, and uploads it to the hub.

Examples:
  wippy publish                    # Publish version from wippy.yaml
  wippy publish --dry-run          # Pack only, don't upload
  wippy publish --label latest     # Publish as mutable label`,
	RunE: runPublish,
}

func init() {
	rootCmd.AddCommand(publishCmd)

	publishCmd.Flags().Bool("dry-run", false, "pack only, don't upload")
	publishCmd.Flags().String("label", "", "publish as mutable label instead of version")
	publishCmd.Flags().String("release-notes", "", "release notes text")
	publishCmd.Flags().Bool("protected", false, "mark version as protected")
	publishCmd.Flags().String("config", ".", "path to directory containing wippy.yaml")
	publishCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runPublish(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	configDir, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	label, _ := cmd.Flags().GetString("label")
	releaseNotes, _ := cmd.Flags().GetString("release-notes")
	protected, _ := cmd.Flags().GetBool("protected")
	registryURL, _ := cmd.Flags().GetString("registry")

	cfg, err := config.Load(configDir)
	if err != nil {
		return NewPublishConfigError(err)
	}

	if label != "" {
		if err := cfg.ValidateForLabel(); err != nil {
			return NewPublishConfigError(err)
		}
	} else {
		if err := cfg.Validate(); err != nil {
			return NewPublishConfigError(err)
		}
	}

	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, err := store.Get(registryURL)
	if err != nil {
		return NewPublishNotAuthenticatedError(err)
	}

	fmt.Println()
	printPublishInfo(cfg, label, registryURL)

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	outputFile := filepath.Join(os.TempDir(), cfg.OutputFileName())
	defer os.Remove(outputFile)

	printStatus("Packing module...")

	packResult, err := packModule(cmd.Context(), app, cfg, configDir, outputFile)
	if err != nil {
		return err
	}

	printSuccess(fmt.Sprintf("Pack created: %s (%s)", packResult.Path, formatFileSize(packResult.Size)))

	if dryRun {
		printSuccess("Dry run complete")
		fmt.Println()
		return nil
	}

	printStatus("Connecting to hub...")

	client, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   cred.Token,
	})
	if err != nil {
		return NewPublishClientError(err)
	}

	params := &hub.PublishParams{
		Org:          cfg.Organization,
		Module:       cfg.ModuleName,
		Digest:       packResult.Digest,
		Size:         packResult.Size,
		ReleaseNotes: releaseNotes,
		Protected:    protected,
	}

	if label != "" {
		params.Label = label
	} else {
		params.Version = cfg.Version
	}

	printStatus("Initiating publish...")

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()

	result, err := client.InitiatePublish(ctx, params)
	if err != nil {
		return NewPublishInitiateError(err)
	}

	printStatus("Uploading package...")

	if err := client.Upload(result.UploadURL, outputFile); err != nil {
		return NewPublishUploadError(err)
	}

	printStatus("Confirming upload...")

	if err := client.ConfirmPublish(ctx, result.PublishID); err != nil {
		return NewPublishConfirmError(err)
	}

	printStatus("Processing...")

	status, err := client.WaitForCompletion(ctx, result.PublishID, func(s *hub.StatusResult) {
		printStatus(fmt.Sprintf("Status: %s", s.StatusString()))
	})
	if err != nil {
		return NewPublishProcessingError(err)
	}

	fmt.Println()

	if status.IsCompleted() {
		if label != "" {
			printSuccess(fmt.Sprintf("Published %s/%s@%s", cfg.Organization, cfg.ModuleName, label))
		} else {
			printSuccess(fmt.Sprintf("Published %s/%s@%s", cfg.Organization, cfg.ModuleName, cfg.Version))
		}
	}

	fmt.Println()

	return nil
}

type packResult struct {
	Path   string
	Size   int64
	Digest string
}

func packModule(ctx context.Context, app *appinit.Context, cfg *config.ModuleConfig, srcDir, outputPath string) (*packResult, error) {
	srcPath := srcDir
	if _, err := os.Stat(filepath.Join(srcDir, "src")); err == nil {
		srcPath = filepath.Join(srcDir, "src")
	}

	dirFS := os.DirFS(srcPath)
	entries, err := app.Loader.LoadFS(ctx, dirFS)
	if err != nil {
		return nil, NewLoadEntriesError(srcPath, err)
	}

	definitionCount := 0
	for _, e := range entries {
		if e.Kind == "ns.definition" {
			definitionCount++
		}
	}

	if definitionCount == 0 {
		return nil, NewPublishNoDefinitionError()
	}

	if definitionCount > 1 {
		return nil, NewPublishMultipleDefinitionsError(definitionCount)
	}

	pipelineStages := []boot.Stage{
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	}

	pipeline := build.New(pipelineStages...)
	if err := pipeline.Execute(ctx, &entries); err != nil {
		return nil, NewExecutePipelineError(err)
	}

	resources := stages.GetResources(ctx)

	metadata := attrs.Bag{
		"name":          cfg.ModuleName,
		"namespace":     cfg.Namespace(),
		"version":       cfg.Version,
		"wippy_version": version.Version,
		"wippy_commit":  version.Commit,
		"packed_at":     time.Now().UTC().Format(time.RFC3339),
		"entry_count":   len(entries),
	}

	if cfg.Description != "" {
		metadata["description"] = cfg.ResolveDescription(srcDir)
	}
	if cfg.License != "" {
		metadata["license"] = cfg.License
	}
	if cfg.Repository != "" {
		metadata["repository"] = cfg.Repository
	}
	if cfg.Homepage != "" {
		metadata["homepage"] = cfg.Homepage
	}
	if len(cfg.Keywords) > 0 {
		metadata["keywords"] = cfg.Keywords
	}
	if len(cfg.Authors) > 0 {
		metadata["authors"] = cfg.Authors
	}

	packWriter := pack.NewWriter(app.Transcoder)

	file, err := os.Create(outputPath)
	if err != nil {
		return nil, NewCreatePackFileError(err)
	}
	defer file.Close()

	if len(resources) > 0 {
		if err := packWriter.PackWithResources(metadata, entries, resources, file); err != nil {
			return nil, NewPackWithResourcesError(err)
		}
	} else {
		if err := packWriter.PackEntries(metadata, entries, file); err != nil {
			return nil, NewPackEntriesError(err)
		}
	}

	if err := file.Close(); err != nil {
		return nil, NewClosePackFileError(err)
	}

	stat, err := os.Stat(outputPath)
	if err != nil {
		return nil, NewStatOutputFileError(err)
	}

	digest, err := computeFileDigest(outputPath)
	if err != nil {
		return nil, NewPublishDigestError(err)
	}

	return &packResult{
		Path:   outputPath,
		Size:   stat.Size(),
		Digest: digest,
	}, nil
}

func computeFileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func printPublishInfo(cfg *config.ModuleConfig, label, registry string) {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	fmt.Printf("%s %s/%s\n", labelStyle.Render("Module:"), cfg.Organization, cfg.ModuleName)
	if label != "" {
		fmt.Printf("%s %s\n", labelStyle.Render("Label:"), label)
	} else {
		fmt.Printf("%s %s\n", labelStyle.Render("Version:"), cfg.Version)
	}
	fmt.Printf("%s %s\n", labelStyle.Render("Registry:"), infoStyle.Render(registry))
	fmt.Println()
}

func printStatus(msg string) {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	fmt.Printf("  %s\n", dimStyle.Render(msg))
}

func printSuccess(msg string) {
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	fmt.Printf("  %s\n", successStyle.Render(msg))
}

func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func NewPublishConfigError(cause error) error {
	return fmt.Errorf("invalid publish config: %w", cause)
}

func NewPublishNotAuthenticatedError(cause error) error {
	return fmt.Errorf("not authenticated - run 'wippy auth login' first: %w", cause)
}

func NewPublishClientError(cause error) error {
	return fmt.Errorf("failed to create hub client: %w", cause)
}

func NewPublishInitiateError(cause error) error {
	return fmt.Errorf("failed to initiate publish: %w", cause)
}

func NewPublishUploadError(cause error) error {
	return fmt.Errorf("failed to upload package: %w", cause)
}

func NewPublishConfirmError(cause error) error {
	return fmt.Errorf("failed to confirm upload: %w", cause)
}

func NewPublishProcessingError(cause error) error {
	return fmt.Errorf("publish processing failed: %w", cause)
}

func NewPublishDigestError(cause error) error {
	return fmt.Errorf("failed to compute digest: %w", cause)
}

func NewPublishNoDefinitionError() error {
	return fmt.Errorf("module must have exactly one ns.definition entry")
}

func NewPublishMultipleDefinitionsError(count int) error {
	return fmt.Errorf("module has %d ns.definition entries, must have exactly 1", count)
}
