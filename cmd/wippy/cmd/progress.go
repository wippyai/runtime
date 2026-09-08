// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// cliProgressReporter writes progress synchronously using the command's context.
type cliProgressReporter struct {
	ctx       context.Context
	out       io.Writer
	pack      *packModel
	mu        sync.Mutex
	lintTotal int
	enabled   bool
	lineOpen  bool
}

func newCLIProgressReporter(ctx context.Context, output *os.File) *cliProgressReporter {
	return newProgressReporter(ctx, output, isTerminalFile(output))
}

func newProgressReporter(ctx context.Context, output io.Writer, enabled bool) *cliProgressReporter {
	if ctx == nil {
		ctx = context.Background()
	}
	if output == nil {
		output = io.Discard
	}
	return &cliProgressReporter{
		ctx:     ctx,
		out:     output,
		enabled: enabled,
	}
}

func (r *cliProgressReporter) Err() error {
	if r == nil || r.ctx == nil {
		return nil
	}
	return r.ctx.Err()
}

// Send writes a progress event. Command execution checks cancellation through Err.
func (r *cliProgressReporter) Send(msg any) {
	if r == nil {
		return
	}
	if err := r.Err(); err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	switch msg := msg.(type) {
	case progressMsg:
		r.sendPackProgress(msg)
	case statsMsg:
		if r.pack != nil {
			r.pack.apply(msg)
		}
	case completedMsg:
		if r.pack != nil {
			r.pack.apply(msg)
			r.writePackSummary()
		}
	case errorMsg:
		if r.pack != nil {
			r.pack.apply(msg)
		}
	case logMsg:
		if r.pack != nil {
			r.pack.apply(msg)
			if r.pack.verbose && len(r.pack.logs) > 0 {
				r.writeLine(r.pack.logs[len(r.pack.logs)-1])
			}
		}
	case lintProgressMsg:
		r.sendLintProgress(msg)
	case lintCompleteMsg:
		r.finishProgressLine()
	}
}

func (r *cliProgressReporter) sendPackProgress(msg progressMsg) {
	if r.pack == nil {
		r.pack = &packModel{}
	}
	r.pack.apply(msg)
	status := strings.TrimSpace(msg.status)
	line := fmt.Sprintf("Packing %s: %3.0f%%", r.pack.outputFile, clampProgress(msg.percent)*100)
	if status != "" {
		line += " " + status
	}
	r.writeProgressLine(line)
}

func (r *cliProgressReporter) sendLintProgress(msg lintProgressMsg) {
	line := fmt.Sprintf("Linting... %d/%d entries (%3.0f%%)", msg.checked, r.lintTotal, clampProgress(msg.percent)*100)
	if msg.errors > 0 || msg.warnings > 0 {
		line += fmt.Sprintf(" [%d errors, %d warnings]", msg.errors, msg.warnings)
	}
	if msg.entry != "" {
		line += " " + msg.entry
	}
	r.writeProgressLine(line)
}

func (r *cliProgressReporter) writeProgressLine(line string) {
	if !r.enabled {
		return
	}
	_, _ = fmt.Fprintf(r.out, "\r%s", line)
	r.lineOpen = true
}

func (r *cliProgressReporter) writeLine(line string) {
	if !r.enabled {
		return
	}
	r.finishProgressLine()
	_, _ = fmt.Fprintln(r.out, line)
}

func (r *cliProgressReporter) finishProgressLine() {
	if !r.enabled || !r.lineOpen {
		return
	}
	_, _ = fmt.Fprintln(r.out)
	r.lineOpen = false
}

func (r *cliProgressReporter) writePackSummary() {
	if r.pack == nil || !r.enabled {
		return
	}
	r.finishProgressLine()
	_, _ = fmt.Fprintln(r.out, "Pack created successfully")
	_, _ = fmt.Fprintf(r.out, "  File: %s\n", r.pack.outputFile)
	_, _ = fmt.Fprintf(r.out, "  Size: %s\n", formatPackSize(r.pack.fileSize))
	if desc := r.pack.metadata.GetString("description", ""); desc != "" {
		_, _ = fmt.Fprintf(r.out, "  Description: %s\n", desc)
	}
	if tags, ok := r.pack.metadata["tags"].([]string); ok && len(tags) > 0 {
		_, _ = fmt.Fprintf(r.out, "  Tags: %s\n", strings.Join(tags, ", "))
	}
	if wippyVer := r.pack.metadata.GetString("wippy_version", ""); wippyVer != "" {
		commit := r.pack.metadata.GetString("wippy_commit", "")
		if len(commit) > 7 {
			commit = commit[:7]
		}
		_, _ = fmt.Fprintf(r.out, "  Wippy: %s (%s)\n", wippyVer, commit)
	}
	if packedAt := r.pack.metadata.GetString("packed_at", ""); packedAt != "" {
		_, _ = fmt.Fprintf(r.out, "  Packed: %s\n", packedAt)
	}
	_, _ = fmt.Fprintf(r.out, "  Entries: %d\n", r.pack.entryCount)
	if r.pack.resourceCount > 0 {
		_, _ = fmt.Fprintln(r.out, "  Embedded resources:")
		for _, res := range r.pack.resources {
			_, _ = fmt.Fprintf(r.out, "    - %s (%d files, %s)\n", res.name, res.fileCount, formatPackSize(int64(res.size)))
		}
	}
}

func (r *cliProgressReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishProgressLine()
}

func clampProgress(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 1 {
		return 1
	}
	return percent
}

func formatPackSize(size int64) string {
	sizeKB := float64(size) / 1024
	if sizeKB > 1024 {
		return fmt.Sprintf("%.2f MB", sizeKB/1024)
	}
	return fmt.Sprintf("%.2f KB", sizeKB)
}

func isTerminalFile(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}
