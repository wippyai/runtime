package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

var addCmd = &cobra.Command{
	Use:   "add <org/module[@version]>",
	Short: "Add a module dependency",
	Long: `Add a module from the hub to the lock file.

Examples:
  wippy add acme/http              # Add latest version
  wippy add acme/http@1.2.3        # Add specific version
  wippy add acme/http@latest       # Add latest label`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func init() {
	rootCmd.AddCommand(addCmd)

	addCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	addCmd.Flags().String("registry", "", "registry URL (default: from credentials)")
}

func runAdd(cmd *cobra.Command, args []string) error {
	moduleRef := args[0]
	lockFile, _ := cmd.Flags().GetString("lock-file")
	registryURL, _ := cmd.Flags().GetString("registry")

	ref, err := parseModuleRef(moduleRef)
	if err != nil {
		return NewAddParseError(err)
	}

	lockPath, err := lock.Find(".", lockFile)
	if err != nil {
		if os.IsNotExist(err) {
			lockPath = lockFile
		} else {
			return NewLockFileNotFoundError(err)
		}
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
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
		return NewAddClientError(err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	params := &hub.DownloadParams{
		Org:    ref.Org,
		Module: ref.Module,
	}

	if ref.IsLabel {
		params.Label = ref.Version
	} else if ref.Version != "" {
		params.Version = ref.Version
	}

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	fmt.Println()
	fmt.Printf("%s %s/%s\n", labelStyle.Render("Adding:"), ref.Org, ref.Module)

	info, err := client.GetDownloadURL(ctx, params)
	if err != nil {
		return NewAddResolveError(err)
	}

	fmt.Printf("%s %s\n", labelStyle.Render("Resolved:"), infoStyle.Render(info.Version))

	moduleName := fmt.Sprintf("%s/%s", ref.Org, ref.Module)
	mod := lock.Module{
		Name:    moduleName,
		Version: info.Version,
		Hash:    info.Digest,
	}

	if existing, found := lockObj.GetModule(moduleName); found {
		if existing.Version == mod.Version {
			fmt.Printf("%s %s already at version %s\n", successStyle.Render("Up to date:"), moduleName, mod.Version)
			fmt.Println()
			return nil
		}
		fmt.Printf("%s %s -> %s\n", labelStyle.Render("Updating:"), existing.Version, mod.Version)
	}

	lockObj.SetModule(mod)

	if err := lockObj.Write(); err != nil {
		return NewWriteLockFileError(err)
	}

	fmt.Printf("%s Added %s@%s\n", successStyle.Render("Done!"), moduleName, mod.Version)
	fmt.Println()
	fmt.Printf("Run 'wippy install' to download the module.\n")
	fmt.Println()

	return nil
}

type moduleRef struct {
	Org     string
	Module  string
	Version string
	IsLabel bool
}

var moduleRefPattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([a-z][a-z0-9-]*)(?:@(.+))?$`)

func parseModuleRef(ref string) (*moduleRef, error) {
	matches := moduleRefPattern.FindStringSubmatch(ref)
	if matches == nil {
		return nil, fmt.Errorf("invalid module reference: %s (expected org/module[@version])", ref)
	}

	result := &moduleRef{
		Org:    matches[1],
		Module: matches[2],
	}

	if len(matches) > 3 && matches[3] != "" {
		result.Version = matches[3]
		if !isValidSemver(matches[3]) {
			result.IsLabel = true
		}
	}

	return result, nil
}

var semverPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

func isValidSemver(v string) bool {
	return semverPattern.MatchString(v)
}

func NewAddParseError(cause error) error {
	return fmt.Errorf("invalid module reference: %w", cause)
}

func NewAddClientError(cause error) error {
	return fmt.Errorf("failed to create hub client: %w", cause)
}

func NewAddResolveError(cause error) error {
	return fmt.Errorf("failed to resolve module: %w", cause)
}
