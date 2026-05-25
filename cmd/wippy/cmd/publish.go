// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/version"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/wapp"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish module to the hub",
	Long: `Publish a module to the wippy hub.

Reads configuration from wippy.yaml in the current directory,
packs the module, and uploads it to the hub.

Version can be provided via --version flag, wippy.yaml, or
selected interactively by bumping the latest published version.

Examples:
  wippy publish                       # Auto-bump from latest version
  wippy publish --version 1.2.0       # Publish specific version
  wippy publish --dry-run             # Pack only, don't upload
  wippy publish --label latest        # Publish as mutable label`,
	RunE: runPublish,
}

func init() {
	rootCmd.AddCommand(publishCmd)

	publishCmd.Flags().String("version", "", "version to publish (overrides wippy.yaml)")
	publishCmd.Flags().Bool("dry-run", false, "pack only, don't upload")
	publishCmd.Flags().String("label", "", "publish as mutable label instead of version")
	publishCmd.Flags().String("release-notes", "", "release notes text")
	publishCmd.Flags().Bool("protected", false, "mark version as protected")
	publishCmd.Flags().String("config", ".", "path to directory containing wippy.yaml")
	publishCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
	publishCmd.Flags().StringSlice("embed", nil, "embed fs.directory entries by id or name (default: none)")
	publishCmd.Flags().Bool("create", false, "create the module on the registry if it does not yet exist")
	publishCmd.Flags().String("module-visibility", "private", "visibility for newly created modules (--create only): public or private")
	publishCmd.Flags().String("module-type", "application", "module type for newly created modules (--create only): library, application, agent or plugin")
	publishCmd.Flags().String("module-display-name", "", "display name for newly created modules (--create only)")
}

func runPublish(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	configDir, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	versionFlag, _ := cmd.Flags().GetString("version")
	label, _ := cmd.Flags().GetString("label")
	releaseNotes, _ := cmd.Flags().GetString("release-notes")
	protected, _ := cmd.Flags().GetBool("protected")
	registryURL, _ := cmd.Flags().GetString("registry")
	embedFlag, _ := cmd.Flags().GetStringSlice("embed")
	embedChanged := cmd.Flags().Changed("embed")
	createIfMissing, _ := cmd.Flags().GetBool("create")
	moduleVisibility, _ := cmd.Flags().GetString("module-visibility")
	moduleType, _ := cmd.Flags().GetString("module-type")
	moduleDisplayName, _ := cmd.Flags().GetString("module-display-name")

	cfg, err := config.Load(configDir)
	if err != nil {
		return NewPublishConfigError(err)
	}

	if versionFlag != "" {
		cfg.Version = versionFlag
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
		return NewPublishNotAuthenticatedError(registryURL, err)
	}

	// Resolve version interactively when not provided and not publishing a label
	if label == "" && cfg.Version == "" {
		hubClient, clientErr := hub.NewClient(hub.Options{
			BaseURL: registryURL,
			Token:   cred.Token,
		})
		if clientErr != nil {
			return NewPublishClientError(registryURL, clientErr)
		}

		resolved, resolveErr := promptVersion(cmd.Context(), hubClient, cfg, registryURL)
		if resolveErr != nil {
			return resolveErr
		}
		cfg.Version = resolved
	}

	if label == "" {
		if err := config.ValidateVersion(cfg.Version); err != nil {
			return NewPublishConfigError(err)
		}
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

	embedPatterns := cfg.Embed
	if embedChanged {
		embedPatterns = embedFlag
	}
	packResult, err := packModule(app.Ctx, app, cfg, configDir, outputFile, embedPatterns)
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
		return NewPublishClientError(registryURL, err)
	}

	registeredThisRun := false
	if createIfMissing {
		if err := ensureModuleRegistered(cmd.Context(), client, registryURL, cfg, moduleDisplayName, moduleType, moduleVisibility); err != nil {
			return err
		}
		registeredThisRun = true
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	// Prefer the hub-mediated upload path (one robust HTTP hop, hub-side
	// retry into S3, never the brittle client→S3 path that's bitten
	// Windows users with winsock resets). Fall back to the legacy
	// InitiatePublish/PUT/ConfirmPublish triplet only if the new endpoint
	// is missing — i.e., publishing against an older hub.
	publishID, err := publishViaHubOrLegacy(ctx, client, registryURL, cfg, outputFile, label, releaseNotes, protected)
	if err != nil && !registeredThisRun && hub.IsModuleNotFound(err) {
		// First publish of a module that was never registered: create it
		// (private by default) and retry, so `wippy publish` just works.
		// A user without create permission gets the real 403 here — that
		// is the honest failure, not something to paper over.
		printStatus(fmt.Sprintf("Module %s/%s not registered yet — creating it", cfg.Organization, cfg.ModuleName))
		if regErr := ensureModuleRegistered(cmd.Context(), client, registryURL, cfg, moduleDisplayName, moduleType, moduleVisibility); regErr != nil {
			return regErr
		}
		publishID, err = publishViaHubOrLegacy(ctx, client, registryURL, cfg, outputFile, label, releaseNotes, protected)
	}
	if err != nil {
		if errors.Is(err, hub.ErrQuotaExceeded) {
			return NewPublishQuotaExceededError(hub.QuotaReason(err))
		}

		return err
	}

	printStatus("Processing...")

	status, err := client.WaitForCompletion(ctx, publishID, func(s *hub.StatusResult) {
		printStatus(fmt.Sprintf("Status: %s", s.StatusString()))
	})
	if err != nil {
		printPublishFailure(publishID, status)
		return NewPublishProcessingError(registryURL, err)
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

// publishViaHubOrLegacy runs the publish flow once. It prefers the new
// hub-mediated upload endpoint (single HTTPS hop to the hub, hub retries
// into S3) and falls back to the legacy presigned-URL triplet only if the
// hub responds 404 to the new path. Returns the publish workflow id to
// poll on.
func publishViaHubOrLegacy(
	ctx context.Context,
	client *hub.Client,
	registryURL string,
	cfg *config.ModuleConfig,
	outputFile, label, releaseNotes string,
	protected bool,
) (string, error) {
	printStatus("Uploading via hub...")

	in := hub.UploadInput{
		Org:          cfg.Organization,
		Module:       cfg.ModuleName,
		Version:      cfg.Version,
		Label:        label,
		ReleaseNotes: releaseNotes,
		FilePath:     outputFile,
		Protected:    protected,
	}
	if label != "" {
		// When publishing a label, the version is resolved server-side.
		in.Version = ""
	}

	out, err := client.PublishViaHub(ctx, in)
	if err == nil {
		return out.PublishID, nil
	}
	// Only fall back when the *server* tells us the hub-mediated endpoint
	// doesn't exist. Any other failure (auth, validation, network, or a
	// module-not-found that runPublish auto-registers + retries) is the
	// real failure and shouldn't be papered over by the legacy path.
	if !hub.IsHubEndpointMissing(err) {
		return "", NewPublishUploadError(registryURL, err)
	}

	printStatus("Hub-mediated upload not available, using legacy flow...")

	// Legacy fallback: InitiatePublish → PUT to presigned URL → ConfirmPublish.
	params := &hub.PublishParams{
		Org:          cfg.Organization,
		Module:       cfg.ModuleName,
		Digest:       "", // hub fills in from the upload body it sees
		ReleaseNotes: releaseNotes,
		Protected:    protected,
	}
	if label != "" {
		params.Label = label
	} else {
		params.Version = cfg.Version
	}
	// The legacy InitiatePublish needs Digest + Size so the hub can size
	// the presigned URL; the caller computed them while packing.
	params.Digest, params.Size, err = digestAndSizeFromFile(outputFile)
	if err != nil {
		return "", NewPublishUploadError(registryURL, err)
	}
	result, err := client.InitiatePublish(ctx, params)
	if err != nil {
		return "", NewPublishInitiateError(registryURL, err)
	}
	if err := client.Upload(ctx, result.UploadURL, outputFile); err != nil {
		return "", NewPublishUploadError(registryURL, err)
	}
	if err := client.ConfirmPublish(ctx, result.PublishID); err != nil {
		return "", NewPublishConfirmError(registryURL, err)
	}
	return result.PublishID, nil
}

func digestAndSizeFromFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// promptVersion fetches the latest published version from the hub and presents
// bump options for the user to select interactively.
func promptVersion(ctx context.Context, client *hub.Client, cfg *config.ModuleConfig, registryURL string) (string, error) {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println()
	fmt.Printf("%s %s/%s\n", labelStyle.Render("Module:"), cfg.Organization, cfg.ModuleName)

	info, err := client.GetModule(ctx, cfg.Organization, cfg.ModuleName)
	if err != nil && !errors.Is(err, hub.ErrModuleNotFound) {
		return "", fmt.Errorf("failed to fetch module info for %s/%s from %s: %w", cfg.Organization, cfg.ModuleName, registryURL, err)
	}

	// New module with no published versions
	if err != nil || info == nil || info.LatestVersion == "" {
		fmt.Printf("%s\n\n", dimStyle.Render("No published versions found"))
		fmt.Printf("  [1] 0.1.0 %s\n", dimStyle.Render("(initial)"))
		fmt.Printf("  [2] 1.0.0\n")
		fmt.Printf("  [c] custom\n\n")

		choice := readInput("Select version [1]: ")
		switch choice {
		case "", "1":
			return "0.1.0", nil
		case "2":
			return "1.0.0", nil
		case "c":
			return readVersion()
		default:
			return parseOrDefault(choice, "0.1.0")
		}
	}

	current, parseErr := semver.NewVersion(info.LatestVersion)
	if parseErr != nil {
		return "", fmt.Errorf("invalid version from hub %q: %w", info.LatestVersion, parseErr)
	}

	patch := current.IncPatch()
	minor := current.IncMinor()
	major := current.IncMajor()

	fmt.Printf("%s %s\n\n", labelStyle.Render("Latest:"), info.LatestVersion)
	fmt.Printf("  [1] %s %s\n", patch.String(), dimStyle.Render("(patch)"))
	fmt.Printf("  [2] %s %s\n", minor.String(), dimStyle.Render("(minor)"))
	fmt.Printf("  [3] %s %s\n", major.String(), dimStyle.Render("(major)"))
	fmt.Printf("  [c] custom\n\n")

	choice := readInput("Select version [1]: ")
	switch choice {
	case "", "1":
		return patch.String(), nil
	case "2":
		return minor.String(), nil
	case "3":
		return major.String(), nil
	case "c":
		return readVersion()
	default:
		return parseOrDefault(choice, patch.String())
	}
}

func readInput(prompt string) string {
	fmt.Print(prompt)
	var input string
	_, _ = fmt.Scanln(&input)
	return strings.TrimSpace(input)
}

func readVersion() (string, error) {
	v := readInput("Enter version: ")
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return "", fmt.Errorf("no version provided")
	}
	if _, err := semver.NewVersion(v); err != nil {
		return "", fmt.Errorf("invalid semver %q: %w", v, err)
	}
	return v, nil
}

func parseOrDefault(input, fallback string) (string, error) {
	v := strings.TrimPrefix(input, "v")
	if _, err := semver.NewVersion(v); err == nil {
		return v, nil
	}
	return fallback, nil
}

type packResult struct {
	Path   string
	Digest string
	Size   int64
}

func packModule(ctx context.Context, app *appinit.Context, cfg *config.ModuleConfig, srcDir, outputPath string, embedPatterns []string) (*packResult, error) {
	srcPath := srcDir
	if _, err := os.Stat(filepath.Join(srcDir, "src")); err == nil {
		srcPath = filepath.Join(srcDir, "src")
	}

	dirFS := os.DirFS(srcPath)
	srcEntries, err := app.Loader.LoadFS(ctx, dirFS)
	if err != nil {
		return nil, NewLoadEntriesError(fmt.Sprintf("source path %s", srcPath), err)
	}

	definitionCount := 0
	for _, e := range srcEntries {
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

	disableOpts := stages.DisableOptions{
		Entries:     cfg.Exclude,
		MetaFilters: cfg.ExcludeMeta,
	}

	pipelineStages := []boot.Stage{
		stages.Override(),
		stages.DisableWithOptions(disableOpts),
		stages.Link(),
		stages.Override(),
	}
	if len(embedPatterns) > 0 {
		pipelineStages = append(pipelineStages, stages.EmbedFS(embedPatterns...))
	}

	pipeline := build.New(pipelineStages...)
	if err := pipeline.Execute(ctx, &srcEntries); err != nil {
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
		"entry_count":   len(srcEntries),
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
	for key, value := range cfg.Metadata {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		// Preserve canonical publish metadata fields.
		if _, exists := metadata[trimmed]; exists {
			continue
		}
		metadata[trimmed] = value
	}

	packWriter := wapp.NewWriter()

	file, err := os.Create(outputPath)
	if err != nil {
		return nil, NewCreatePackFileError(fmt.Errorf("pack file %s: %w", outputPath, err))
	}
	defer file.Close()

	wappEntries := entries.ConvertToWappEntries(srcEntries)
	wappMetadata := wapp.Metadata(metadata)

	if len(resources) > 0 {
		if err := packWriter.PackWithResources(wappMetadata, wappEntries, resources, file); err != nil {
			return nil, NewPackWithResourcesError(fmt.Errorf("pack file %s: %w", outputPath, err))
		}
	} else {
		if err := packWriter.PackEntries(wappMetadata, wappEntries, file); err != nil {
			return nil, NewPackEntriesError(fmt.Errorf("pack file %s: %w", outputPath, err))
		}
	}

	if err := file.Close(); err != nil {
		return nil, NewClosePackFileError(fmt.Errorf("pack file %s: %w", outputPath, err))
	}

	if err := verifyPackedResources(outputPath, resources); err != nil {
		return nil, NewPackIntegrityError(fmt.Errorf("pack file %s: %w", outputPath, err))
	}

	stat, err := os.Stat(outputPath)
	if err != nil {
		return nil, NewStatOutputFileError(fmt.Errorf("pack file %s: %w", outputPath, err))
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

// printPublishFailure surfaces the correlation id, server error code and
// an actionable hint so a failed publish is self-explanatory instead of
// an opaque message. The command still returns a non-zero error — this
// only enriches the output, it never masks the failure.
func printPublishFailure(publishID string, status *hub.StatusResult) {
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println()
	if publishID != "" {
		fmt.Printf("  %s %s\n", dimStyle.Render("publish id:"), publishID)
	}
	if status == nil {
		return
	}
	code := status.ErrorCode
	if code != "" {
		fmt.Printf("  %s %s — %s\n", errStyle.Render("error:"), code, status.ErrorMessage)
	} else if status.ErrorMessage != "" {
		fmt.Printf("  %s %s\n", errStyle.Render("error:"), status.ErrorMessage)
	}
	if h := hintFor(code); h != "" {
		fmt.Printf("  %s %s\n", dimStyle.Render("hint:"), h)
	}
}

func hintFor(code string) string {
	switch code {
	case "version_exists":
		return "bump the version (wippy publish --version <next>) — published versions are immutable"
	case "version_missing":
		return "transient infrastructure issue — retry; if it persists report the publish id to ops"
	case "scan_unavailable":
		return "antivirus temporarily unavailable — retry shortly, your upload is preserved"
	case "malware_detected":
		return "the package was flagged by antivirus and held for security review"
	default:
		return ""
	}
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

func NewPublishNotAuthenticatedError(registryURL string, cause error) error {
	return fmt.Errorf("not authenticated for %s - run 'wippy auth login' first: %w", registryURL, cause)
}

func NewPublishClientError(registryURL string, cause error) error {
	return fmt.Errorf("failed to create hub client for %s: %w", registryURL, cause)
}

func NewPublishInitiateError(registryURL string, cause error) error {
	// Surface a hub quota refusal up-front instead of burying it behind
	// "failed to initiate publish ... resource_exhausted ...". The hub
	// already returns the actionable reason ("Private-module quota
	// exhausted (5 of 5). Ask an admin ..."); just relabel the prefix.
	if connect.CodeOf(cause) == connect.CodeResourceExhausted {
		return fmt.Errorf("quota exceeded on %s: %w", registryURL, cause)
	}
	return fmt.Errorf("failed to initiate publish on %s: %w", registryURL, cause)
}

// ensureModuleRegistered registers org/module on the hub (private by
// default). Idempotent: an existing module is a no-op. A real failure
// (notably 403 — no create permission in the org) is returned verbatim
// so the user sees the honest cause, never a masked one.
func ensureModuleRegistered(ctx context.Context, client *hub.Client, registryURL string, cfg *config.ModuleConfig, displayName, moduleType, moduleVisibility string) error {
	if displayName == "" {
		displayName = cfg.ModuleName
	}
	keywords := cfg.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	regCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	regResult, regErr := client.RegisterModule(regCtx, &hub.RegisterModuleParams{
		Org:           cfg.Organization,
		Name:          cfg.ModuleName,
		DisplayName:   displayName,
		Description:   cfg.Description,
		ModuleType:    moduleType,
		Visibility:    moduleVisibility,
		License:       cfg.License,
		Keywords:      keywords,
		RepositoryURL: cfg.Repository,
		HomepageURL:   cfg.Homepage,
	})
	switch {
	case regErr == nil:
		printStatus(fmt.Sprintf("Registered module %s/%s (visibility=%s, type=%s)",
			regResult.OrgName, regResult.Name, regResult.Visibility, regResult.ModuleType))
		return nil
	case errors.Is(regErr, hub.ErrModuleAlreadyExists):
		printStatus(fmt.Sprintf("Module %s/%s already exists", cfg.Organization, cfg.ModuleName))
		return nil
	default:
		return fmt.Errorf("register module on %s: %w", registryURL, regErr)
	}
}

func NewPublishUploadError(registryURL string, cause error) error {
	return fmt.Errorf("failed to upload package to %s: %w", registryURL, cause)
}

func NewPublishQuotaExceededError(reason string) error {
	if reason == "" {
		reason = "the organization is over its plan quota; upgrade the plan or reduce usage and try again"
	}

	return fmt.Errorf("cannot publish: %s", reason)
}

func NewPublishConfirmError(registryURL string, cause error) error {
	return fmt.Errorf("failed to confirm upload on %s: %w", registryURL, cause)
}

func NewPublishProcessingError(registryURL string, cause error) error {
	return fmt.Errorf("publish processing failed on %s: %w", registryURL, cause)
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
