package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/version"
	"github.com/wippyai/runtime/cmd/internal/banner"
)

var shortVersion bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  "Display version, commit, build date and builder information",
	Run: func(_ *cobra.Command, _ []string) {
		if shortVersion {
			fmt.Println(version.Short())
			return
		}

		banner.Print(false)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVar(&shortVersion, "short", false, "print only version number")
}
