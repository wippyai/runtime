package cmd

import (
	"fmt"

	"github.com/ponyruntime/pony/boot/pack"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var loadCmd = &cobra.Command{
	Use:   "load <pack1.wapp> [pack2.wapp...]",
	Short: "Load one or more pack files",
	Long: `Load pre-built .wapp pack files containing fully linked entries.

Pack files are loaded in order and entries are applied directly to the registry
without running any pipeline stages (entries are already linked).

Examples:
  wippy load snapshot.wapp
  wippy load base.wapp overlay.wapp`,
	Args: cobra.MinimumNArgs(1),
	RunE: runLoad,
}

func init() {
	rootCmd.AddCommand(loadCmd)
}

func runLoad(cmd *cobra.Command, args []string) error {
	app, err := InitApp(cmd.Context())
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}

	logger := app.Logger.Named("load")

	packer := pack.New(app.Transcoder)

	totalEntries := 0
	for _, packFile := range args {
		logger.Info("loading pack file", zap.String("file", packFile))

		entries, err := packer.Unpack(packFile)
		if err != nil {
			return fmt.Errorf("unpack %s: %w", packFile, err)
		}

		logger.Info("unpacked entries",
			zap.String("file", packFile),
			zap.Int("count", len(entries)))

		// TODO: Apply entries to registry
		// For now, just count them
		totalEntries += len(entries)
	}

	logger.Info("load completed",
		zap.Int("packs", len(args)),
		zap.Int("total_entries", totalEntries))

	return nil
}
