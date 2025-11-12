package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/ponyruntime/pony/cmd/wippy/version"
	"github.com/spf13/cobra"
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

		titleColor := color.New(color.FgHiCyan, color.Bold)
		valueColor := color.New(color.FgWhite)

		titleColor.Print("Version:  ")
		valueColor.Println(version.Version)

		titleColor.Print("Commit:   ")
		valueColor.Println(version.Commit)

		titleColor.Print("Built:    ")
		valueColor.Println(version.Date)

		titleColor.Print("Built by: ")
		valueColor.Println(version.BuiltBy)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVar(&shortVersion, "short", false, "print only version number")
}
