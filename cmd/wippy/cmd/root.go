// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/banner"
)

var (
	verbose      bool
	veryVerbose  bool
	console      bool
	silentLogs   bool
	eventStreams bool
	profiler     bool
	configFile   string
	memoryLimit  string
	appStartTime = time.Now()
)

const (
	defaultMemoryLimit = 1 << 30 // 1GB
	defaultConfigFile  = ".wippy.yaml"
)

var defaultLockFile = lock.DefaultFilename

var rootCmd = &cobra.Command{
	Use:           "wippy",
	Short:         "Adaptive Application Runtime",
	Long:          `Run applications with dynamic configuration and lifecycle management.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		banner.Print(silentLogs)
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// IsConsoleMode returns whether console mode is enabled
func IsConsoleMode() bool {
	return console
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file (default is .wippy.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose debug logging")
	rootCmd.PersistentFlags().BoolVar(&veryVerbose, "very-verbose", false, "enable very verbose debug logging with stack traces")
	rootCmd.PersistentFlags().BoolVarP(&console, "console", "c", false, "enable colorful humanized console logging")
	rootCmd.PersistentFlags().BoolVarP(&silentLogs, "silent", "s", false, "disable console logging entirely")
	rootCmd.PersistentFlags().BoolVarP(&eventStreams, "event-streams", "e", false, "stream logs to event bus instead of console")
	rootCmd.PersistentFlags().BoolVarP(&profiler, "profiler", "p", false, "enable pprof profiler on localhost:6060")
	rootCmd.PersistentFlags().StringVarP(&memoryLimit, "memory-limit", "m", "", "set memory limit (e.g., 1G, 512M, 2048M). Default: 1G if GOMEMLIMIT not set")
}

// initMemoryLimit sets the Go runtime memory limit.
// Priority: --memory-limit flag > GOMEMLIMIT env > default 1GB
func initMemoryLimit() int64 {
	if memoryLimit != "" {
		limit, err := parseMemorySize(memoryLimit)
		if err == nil && limit > 0 {
			debug.SetMemoryLimit(limit)
			return limit
		}
	}

	if envLimit := os.Getenv("GOMEMLIMIT"); envLimit != "" {
		limit, err := parseMemorySize(envLimit)
		if err == nil && limit > 0 {
			return limit
		}
	}

	debug.SetMemoryLimit(defaultMemoryLimit)
	return defaultMemoryLimit
}

// parseMemorySize parses memory size strings like "1G", "512M", "1024K", "1073741824"
func parseMemorySize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	multiplier := int64(1)
	lastChar := s[len(s)-1]

	switch lastChar {
	case 'K', 'k':
		multiplier = 1 << 10
		s = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1 << 20
		s = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1 << 30
		s = s[:len(s)-1]
	case 'T', 't':
		multiplier = 1 << 40
		s = s[:len(s)-1]
	case 'B', 'b':
		if len(s) >= 2 {
			switch s[len(s)-2] {
			case 'K', 'k':
				multiplier = 1 << 10
				s = s[:len(s)-2]
			case 'M', 'm':
				multiplier = 1 << 20
				s = s[:len(s)-2]
			case 'G', 'g':
				multiplier = 1 << 30
				s = s[:len(s)-2]
			case 'T', 't':
				multiplier = 1 << 40
				s = s[:len(s)-2]
			default:
				s = s[:len(s)-1]
			}
		} else {
			s = s[:len(s)-1]
		}
	}

	s = strings.TrimSpace(s)
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}

	return val * multiplier, nil
}

// formatBytes formats bytes into human-readable format
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + "B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(b)/float64(div), 'f', 1, 64) + string("KMGTPE"[exp]) + "B"
}
