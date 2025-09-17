package cmd

import (
	"fmt"

	"github.com/ponyruntime/pony/deps"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var replaceCmd = &cobra.Command{
	Use:   "replace",
	Short: "Manage module replacements",
	Long:  "Module replacements allow you to use custom paths instead of downloading modules from the registry, similar to Go's replace directive.",
}

var replaceAddCmd = &cobra.Command{
	Use:   "add <module> <path>",
	Short: "Add a replacement for a module",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")
		moduleName := args[0]
		customPath := args[1]
		folderPath := "."

		// Check if lock file exists
		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			return fmt.Errorf("lock file not found: %s", lockFile)
		}

		// Load lock file
		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			return fmt.Errorf("failed to load lock file: %w", err)
		}

		// Validate that the module exists in the lock file
		moduleExists := false
		for _, module := range lockFileObj.Modules {
			if module.Name == moduleName {
				moduleExists = true
				break
			}
		}

		if !moduleExists {
			return fmt.Errorf("module %s not found in lock file", moduleName)
		}

		// Check if replacement already exists
		for _, replacement := range lockFileObj.Replacements {
			if replacement.From == moduleName {
				return fmt.Errorf("replacement for module %s already exists", moduleName)
			}
		}

		// Add the replacement
		lockFileObj.Replacements = append(lockFileObj.Replacements, deps.Replacement{
			From: moduleName,
			To:   customPath,
		})

		// Validate the replacement path
		if err := lockFileObj.ValidateReplacements(lockPath); err != nil {
			return fmt.Errorf("invalid replacement path: %w", err)
		}

		// Save the updated lock file
		if err := lockFileObj.SaveLockFile(lockPath); err != nil {
			return fmt.Errorf("failed to save lock file: %w", err)
		}

		logger.Info("Replacement added successfully",
			zap.String("module", moduleName),
			zap.String("path", customPath))

		return nil
	},
}

var replaceRemoveCmd = &cobra.Command{
	Use:   "remove <module>",
	Short: "Remove a replacement for a module",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")
		moduleName := args[0]
		folderPath := "."

		// Check if lock file exists
		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			return fmt.Errorf("lock file not found: %s", lockFile)
		}

		// Load lock file
		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			return fmt.Errorf("failed to load lock file: %w", err)
		}

		// Find and remove the replacement
		found := false
		for i, replacement := range lockFileObj.Replacements {
			if replacement.From == moduleName {
				lockFileObj.Replacements = append(lockFileObj.Replacements[:i], lockFileObj.Replacements[i+1:]...)
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("no replacement found for module %s", moduleName)
		}

		// Save the updated lock file
		if err := lockFileObj.SaveLockFile(lockPath); err != nil {
			return fmt.Errorf("failed to save lock file: %w", err)
		}

		logger.Info("Replacement removed successfully",
			zap.String("module", moduleName))

		return nil
	},
}

var replaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all replacements",
	RunE: func(cmd *cobra.Command, _ []string) error {
		lockFile, _ := cmd.Flags().GetString("lock-file")
		folderPath := "."

		// Check if lock file exists
		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			return fmt.Errorf("lock file not found: %s", lockFile)
		}

		// Load lock file
		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			return fmt.Errorf("failed to load lock file: %w", err)
		}

		if len(lockFileObj.Replacements) == 0 {
			fmt.Println("No module replacements configured.")
			return nil
		}

		fmt.Println("Module replacements:")
		for _, replacement := range lockFileObj.Replacements {
			fmt.Printf("  %s -> %s\n", replacement.From, replacement.To)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(replaceCmd)

	// Add subcommands
	replaceCmd.AddCommand(replaceAddCmd)
	replaceCmd.AddCommand(replaceRemoveCmd)
	replaceCmd.AddCommand(replaceListCmd)

	// Add flags to all subcommands
	replaceAddCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	replaceRemoveCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	replaceListCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}
