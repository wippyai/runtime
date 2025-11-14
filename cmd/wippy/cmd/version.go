package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/cmd/wippy/version"
)

var shortVersion bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  "Display version, commit, build date and builder information",
	Run: func(cmd *cobra.Command, args []string) {
		if shortVersion {
			fmt.Println(version.Short())
			return
		}

		printBanner()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVar(&shortVersion, "short", false, "print only version number")
}
