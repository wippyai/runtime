// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/version"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

var packCmd = &cobra.Command{
	Use:   "pack <output.wapp>",
	Short: "Create a snapshot pack of the application state",
	Long: `Load all entries and dependencies, execute full pipeline (override, disable, link),
and serialize to a compressed binary .wapp file.

The pack file contains fully linked entries ready for loading without additional processing.

Examples:
  wippy pack snapshot.wapp
  wippy pack release-v1.2.3.wapp
  wippy pack --embed app:assets snapshot.wapp`,
	Args: cobra.ExactArgs(1),
	RunE: runPack,
}

func init() {
	rootCmd.AddCommand(packCmd)

	packCmd.Flags().StringP("lock-file", "l", defaultLockFile, "path to lock file")
	packCmd.Flags().StringP("description", "d", "", "pack description")
	packCmd.Flags().StringSliceP("tags", "t", nil, "pack tags")
	packCmd.Flags().StringArray("meta", nil, "custom metadata (key=value, supports dotted notation)")
	packCmd.Flags().StringSlice("embed", nil, "embed patterns (entry IDs or names to embed, e.g., app:assets,app:static)")
	packCmd.Flags().Bool("list", false, "list all fs.directory entries and exit (dry-run mode)")
	packCmd.Flags().StringSlice("exclude-ns", nil, "exclude entries by namespace patterns (e.g., app.**,test.*)")
	packCmd.Flags().StringSlice("exclude", nil, "exclude entries by ID patterns (e.g., app:internal,test:*)")
	packCmd.Flags().StringSlice("bytecode", nil, "compile Lua to bytecode (** for all, or patterns: app:**, lib:utils)")
}

type packStage string

const (
	stageInit        packStage = "init"
	stageLoadLock    packStage = "load_lock"
	stageLoadEntries packStage = "load_entries"
	stagePipeline    packStage = "pipeline"
	stageCollect     packStage = "collect_resources"
	stageWrite       packStage = "write_pack"
	stageDone        packStage = "done"
	stageError       packStage = "error"
)

type packModel struct {
	err           error
	metadata      attrs.Bag
	status        string
	stage         packStage
	outputFile    string
	logs          []string
	resources     []resourceInfo
	embedPatterns []string
	progress      progress.Model
	entryCount    int
	fileSize      int64
	resourceCount int
	percent       float64
	maxLogs       int
	done          bool
	verbose       bool
}

type progressMsg struct {
	stage   packStage
	status  string
	percent float64
}

type resourceInfo struct {
	name      string
	fileCount int
	size      uint64
}

type statsMsg struct {
	resources     []resourceInfo
	entryCount    int
	resourceCount int
}

type completedMsg struct {
	metadata attrs.Bag
	fileSize int64
}

type errorMsg struct {
	err error
}

type logMsg struct {
	fields  map[string]any
	level   string
	message string
}

func (m *packModel) Init() tea.Cmd {
	return nil
}

func (m *packModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case logMsg:
		if m.verbose {
			m.addLog(msg)
		}
		return m, nil

	case progressMsg:
		m.stage = msg.stage
		m.percent = msg.percent
		m.status = msg.status
		return m, m.progress.SetPercent(msg.percent)

	case statsMsg:
		m.entryCount = msg.entryCount
		m.resourceCount = msg.resourceCount
		m.resources = msg.resources
		return m, nil

	case completedMsg:
		m.stage = stageDone
		m.fileSize = msg.fileSize
		m.metadata = msg.metadata
		m.percent = 1.0
		m.done = true
		return m, tea.Sequence(m.progress.SetPercent(1.0), tea.Quit)

	case errorMsg:
		m.stage = stageError
		m.err = msg.err
		m.done = true
		return m, tea.Quit

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m *packModel) addLog(msg logMsg) {
	levelColor := "8"
	levelIcon := "•"

	switch msg.level {
	case "info":
		levelColor = "14"
		levelIcon = "●"
	case "warn":
		levelColor = "11"
		levelIcon = "⚠"
	case "error":
		levelColor = "9"
		levelIcon = "✗"
	case "debug":
		levelColor = "8"
		levelIcon = "○"
	}

	levelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(levelColor)).Bold(true)
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	logLine := fmt.Sprintf("%s %s", levelStyle.Render(levelIcon), msgStyle.Render(msg.message))

	if len(msg.fields) > 0 {
		var fields []string
		for k, v := range msg.fields {
			fields = append(fields, fmt.Sprintf("%s=%v", k, v))
		}
		logLine += " " + dimStyle.Render(strings.Join(fields, " "))
	}

	m.logs = append(m.logs, logLine)
	if len(m.logs) > m.maxLogs {
		m.logs = m.logs[len(m.logs)-m.maxLogs:]
	}
}

func (m *packModel) View() string {
	if m.done && m.err != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Render(fmt.Sprintf("\n✗ Error: %v\n", m.err))
	}

	if m.done {
		successStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true)

		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))

		sizeKB := float64(m.fileSize) / 1024
		var sizeStr string
		if sizeKB > 1024 {
			sizeStr = fmt.Sprintf("%.2f MB", sizeKB/1024)
		} else {
			sizeStr = fmt.Sprintf("%.2f KB", sizeKB)
		}

		info := fmt.Sprintf("\n  %s %s", labelStyle.Render("File:"), m.outputFile)
		info += fmt.Sprintf("\n  %s %s", labelStyle.Render("Size:"), sizeStr)

		if desc := m.metadata.GetString("description", ""); desc != "" {
			info += fmt.Sprintf("\n  %s %s", labelStyle.Render("Description:"), desc)
		}

		if tags, ok := m.metadata["tags"].([]string); ok && len(tags) > 0 {
			info += fmt.Sprintf("\n  %s %s", labelStyle.Render("Tags:"), strings.Join(tags, ", "))
		}

		if wippyVer := m.metadata.GetString("wippy_version", ""); wippyVer != "" {
			commit := m.metadata.GetString("wippy_commit", "")
			if len(commit) > 7 {
				commit = commit[:7]
			}
			info += fmt.Sprintf("\n  %s %s (%s)", labelStyle.Render("Wippy:"), wippyVer, commit)
		}

		if packedAt := m.metadata.GetString("packed_at", ""); packedAt != "" {
			info += fmt.Sprintf("\n  %s %s", labelStyle.Render("Packed:"), packedAt)
		}

		info += fmt.Sprintf("\n  %s %d", labelStyle.Render("Entries:"), m.entryCount)

		if m.resourceCount > 0 {
			info += fmt.Sprintf("\n\n  %s", labelStyle.Render("Embedded resources:"))
			for _, res := range m.resources {
				resSize := float64(res.size) / 1024
				var resSizeStr string
				if resSize > 1024 {
					resSizeStr = fmt.Sprintf("%.2f MB", resSize/1024)
				} else {
					resSizeStr = fmt.Sprintf("%.2f KB", resSize)
				}
				info += fmt.Sprintf("\n    • %s (%d files, %s)",
					dimStyle.Render(res.name),
					res.fileCount,
					resSizeStr)
			}
		}

		return successStyle.Render("\n✓ Pack created successfully") +
			dimStyle.Render(info) +
			"\n\n"
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	var embedInfo string
	if len(m.embedPatterns) > 0 {
		embedInfo = statusStyle.Render(fmt.Sprintf("  Embed patterns: %s\n", strings.Join(m.embedPatterns, ", ")))
	}

	var view strings.Builder
	view.WriteString("\n")
	view.WriteString(titleStyle.Render("Creating pack: " + m.outputFile))
	view.WriteString("\n")
	view.WriteString(embedInfo)
	view.WriteString("\n")

	if m.verbose && len(m.logs) > 0 {
		logStyle := lipgloss.NewStyle().
			MaxHeight(15).
			PaddingLeft(1)

		view.WriteString(logStyle.Render(strings.Join(m.logs, "\n")))
		view.WriteString("\n\n")
	}

	view.WriteString("  ")
	view.WriteString(m.progress.View())
	view.WriteString("\n\n  ")
	view.WriteString(statusStyle.Render(m.status))
	view.WriteString("\n\n")

	return view.String()
}

func runPack(cmd *cobra.Command, args []string) error {
	// Auto-enable compact mode for pack command
	silentLogs = true

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	outputFile := args[0]
	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."
	listMode, _ := cmd.Flags().GetBool("list")

	// Install modules BEFORE starting TUI to avoid log pollution
	lockPath, err := lock.Find(folderPath, lockFile)
	if err != nil {
		return NewLockFileNotFoundError(err)
	}

	if err := entries.EnsureModulesInstalled(app.Ctx, lockPath, app.Logger.Named("pack")); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	// If list mode, just load entries and display fs.directory entries
	if listMode {
		return runListMode(app, lockPath, folderPath)
	}

	embedPatterns, _ := cmd.Flags().GetStringSlice("embed")
	verboseMode := rootCmd.PersistentFlags().Lookup("verbose").Changed

	prog := progress.New(progress.WithDefaultGradient())
	m := &packModel{
		stage:         stageInit,
		progress:      prog,
		status:        "Initializing...",
		embedPatterns: embedPatterns,
		outputFile:    outputFile,
		verbose:       verboseMode,
		maxLogs:       20,
	}

	p := tea.NewProgram(m)

	go func() {
		if err := performPack(cmd, args, app, p); err != nil {
			p.Send(errorMsg{err: err})
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	if packModel, ok := finalModel.(*packModel); ok && packModel.err != nil {
		return packModel.err
	}

	return nil
}

func performPack(cmd *cobra.Command, args []string, app *appinit.Context, p *tea.Program) error {
	logger := app.Logger.Named("pack")
	outputFile := args[0]
	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."
	embedPatterns, _ := cmd.Flags().GetStringSlice("embed")
	excludeNS, _ := cmd.Flags().GetStringSlice("exclude-ns")
	excludeEntries, _ := cmd.Flags().GetStringSlice("exclude")
	bytecodePatterns, _ := cmd.Flags().GetStringSlice("bytecode")

	p.Send(progressMsg{stage: stageLoadLock, percent: 0.1, status: "Loading lock file..."})
	p.Send(logMsg{level: "info", message: "Starting pack process"})

	lockPath, err := lock.Find(folderPath, lockFile)
	if err != nil {
		return NewLockFileNotFoundError(err)
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	paths := lockObj.GetLoadPaths()

	p.Send(progressMsg{stage: stageLoadEntries, percent: 0.2, status: fmt.Sprintf("Loading entries from %d paths...", len(paths))})
	p.Send(logMsg{level: "info", message: fmt.Sprintf("Loading from %d paths", len(paths))})

	loadedEntries, err := loadEntriesFromLockPaths(app.Ctx, lockObj, logger)
	if err != nil {
		return NewLoadEntriesError(fmt.Sprintf("lock paths (%s)", lockPath), err)
	}
	p.Send(progressMsg{
		stage:   stageLoadEntries,
		percent: 0.4,
		status:  fmt.Sprintf("Loaded %d entries", len(loadedEntries)),
	})
	p.Send(logMsg{level: "info", message: fmt.Sprintf("Loaded %d entries total", len(loadedEntries))})

	p.Send(statsMsg{entryCount: len(loadedEntries)})

	p.Send(progressMsg{stage: stagePipeline, percent: 0.5, status: "Executing pipeline stages..."})

	// Load .wippy.yaml config so Override stages apply overrides to packed entries
	bootCfg, err := loadBootConfig()
	if err != nil {
		return fmt.Errorf("load boot config: %w", err)
	}
	if bootCfg != nil {
		boot.WithConfig(app.Ctx, bootCfg)
	}

	// Build pipeline with exclude stage if patterns provided
	var pipelineStages []boot.Stage
	pipelineStages = append(pipelineStages, stages.Override())

	if len(excludeNS) > 0 || len(excludeEntries) > 0 {
		p.Send(logMsg{level: "info", message: "Adding exclude filters", fields: map[string]any{
			"ns_patterns":    len(excludeNS),
			"entry_patterns": len(excludeEntries),
		}})
		pipelineStages = append(pipelineStages, stages.Disable(excludeNS, excludeEntries))
	}

	pipelineStages = append(pipelineStages, stages.Disable(), stages.Link(), stages.Override())

	// Bytecode compilation (before EmbedFS so bytecode FS can be collected)
	if len(bytecodePatterns) > 0 {
		// Check for "all" patterns: **, **:** means compile everything
		compileAll := false
		for _, pat := range bytecodePatterns {
			if pat == "**" || pat == "**:**" {
				compileAll = true
				break
			}
		}

		if compileAll {
			p.Send(logMsg{level: "info", message: "Adding bytecode compilation (all entries)"})
			pipelineStages = append(pipelineStages, stages.Bytecode())
		} else {
			p.Send(logMsg{level: "info", message: "Adding bytecode compilation", fields: map[string]any{
				"patterns": bytecodePatterns,
			}})
			pipelineStages = append(pipelineStages, stages.Bytecode(bytecodePatterns...))
		}
	}

	if len(embedPatterns) > 0 {
		p.Send(progressMsg{
			stage:   stagePipeline,
			percent: 0.55,
			status:  fmt.Sprintf("Processing embed patterns: %s", strings.Join(embedPatterns, ", ")),
		})
		pipelineStages = append(pipelineStages, stages.EmbedFS(embedPatterns...))
	}

	pipeline := build.New(pipelineStages...)

	if err := pipeline.Execute(app.Ctx, &loadedEntries); err != nil {
		return NewExecutePipelineError(err)
	}

	resources := stages.GetResources(app.Ctx)

	// Add bytecode resource if compiled
	if bcRes := stages.GetBytecodeResource(); bcRes != nil {
		resources = append(resources, *bcRes)
	}

	var resInfos []resourceInfo
	if len(resources) > 0 {
		p.Send(logMsg{level: "info", message: "Collecting embedded resources", fields: map[string]any{
			"count": len(resources),
		}})

		for _, res := range resources {
			p.Send(logMsg{level: "info", message: "Processing resource", fields: map[string]any{
				"id": res.ID.String(),
			}})

			// Count files and calculate size by walking the filesystem
			var fileCount int
			var totalSize uint64
			if walkErr := fs.WalkDir(res.FS, ".", func(_ string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					fileCount++
					if info, err := d.Info(); err == nil {
						totalSize += uint64(info.Size())
					}
				}
				return nil
			}); walkErr == nil {
				resInfos = append(resInfos, resourceInfo{
					name:      res.ID.String(),
					fileCount: fileCount,
					size:      totalSize,
				})
				p.Send(logMsg{level: "info", message: "Resource collected", fields: map[string]any{
					"id":    res.ID.String(),
					"files": fileCount,
					"size":  fmt.Sprintf("%.2fKB", float64(totalSize)/1024),
				}})
			}
		}

		p.Send(statsMsg{
			entryCount:    len(loadedEntries),
			resourceCount: len(resources),
			resources:     resInfos,
		})
		p.Send(progressMsg{
			stage:   stageCollect,
			percent: 0.7,
			status:  fmt.Sprintf("Collected %d embedded resources", len(resources)),
		})
	}

	description, _ := cmd.Flags().GetString("description")
	tags, _ := cmd.Flags().GetStringSlice("tags")
	metaFlags, _ := cmd.Flags().GetStringArray("meta")

	metadata := attrs.Bag{
		"wippy_version": version.Version,
		"wippy_commit":  version.Commit,
		"wippy_date":    version.Date,
		"packed_at":     time.Now().UTC().Format(time.RFC3339),
		"entry_count":   len(loadedEntries),
	}

	if description != "" {
		metadata["description"] = description
	}
	if len(tags) > 0 {
		metadata["tags"] = tags
	}
	if len(resources) > 0 {
		metadata["resource_count"] = len(resources)
	}

	if err := parseMetadataFlags(metaFlags, metadata, logger); err != nil {
		return NewParseMetadataError(err)
	}

	p.Send(progressMsg{stage: stageWrite, percent: 0.8, status: "Writing pack file..."})

	progressCallback := func(resourceID wapp.ID, current, total int) {
		percent := 0.8 + (float64(current)/float64(total))*0.15
		p.Send(progressMsg{
			stage:   stageWrite,
			percent: percent,
			status:  fmt.Sprintf("Packing %s: %d/%d files", resourceID.Name, current, total),
		})
	}

	packWriter := wapp.NewWriter(wapp.WithProgressCallback(progressCallback))

	file, err := os.Create(outputFile)
	if err != nil {
		return NewCreatePackFileError(fmt.Errorf("pack file %s: %w", outputFile, err))
	}
	defer func() { _ = file.Close() }()

	wappEntries := entries.ConvertToWappEntries(loadedEntries)
	wappMetadata := wapp.Metadata(metadata)

	if len(resources) > 0 {
		if err := packWriter.PackWithResources(wappMetadata, wappEntries, resources, file); err != nil {
			return NewPackWithResourcesError(fmt.Errorf("pack file %s: %w", outputFile, err))
		}
	} else {
		if err := packWriter.PackEntries(wappMetadata, wappEntries, file); err != nil {
			return NewPackEntriesError(fmt.Errorf("pack file %s: %w", outputFile, err))
		}
	}

	if err := file.Close(); err != nil {
		return NewClosePackFileError(fmt.Errorf("pack file %s: %w", outputFile, err))
	}

	if err := verifyPackedResources(outputFile, resources); err != nil {
		return NewPackIntegrityError(fmt.Errorf("pack file %s: %w", outputFile, err))
	}

	fileInfo, err := os.Stat(outputFile)
	if err != nil {
		return NewStatOutputFileError(fmt.Errorf("pack file %s: %w", outputFile, err))
	}
	p.Send(completedMsg{
		fileSize: fileInfo.Size(),
		metadata: metadata,
	})

	return nil
}

func parseMetadataFlags(metaFlags []string, metadata attrs.Bag, logger *zap.Logger) error {
	for _, flag := range metaFlags {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 {
			return NewInvalidMetadataFormatError(flag)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "" {
			return NewEmptyMetadataKeyError(flag)
		}

		if strings.HasPrefix(key, "wippy.") || strings.HasPrefix(key, "system.") {
			return NewReservedMetadataNamespaceError(key)
		}

		parsedValue := parseMetadataValue(value)
		metadata[key] = parsedValue

		logger.Debug("added custom metadata",
			zap.String("key", key),
			zap.Any("value", parsedValue))
	}

	return nil
}

func parseMetadataValue(value string) any {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	if num, err := strconv.ParseInt(value, 10, 64); err == nil {
		return num
	}

	if num, err := strconv.ParseFloat(value, 64); err == nil {
		return num
	}

	return value
}

func runListMode(app *appinit.Context, lockPath, _ string) error {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	_ = lockObj.GetLoadPaths()

	allEntries, err := loadEntriesFromLockPaths(app.Ctx, lockObj, app.Logger)
	if err != nil {
		return NewLoadEntriesError(fmt.Sprintf("lock paths (%s)", lockPath), err)
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println(titleStyle.Render("\nAvailable fs.directory entries:"))
	fmt.Println()

	count := 0
	for _, e := range allEntries {
		if e.Kind != "fs.directory" {
			continue
		}

		count++
		data := e.Data.Data()
		cfg, ok := data.(map[string]any)
		if !ok {
			continue
		}

		directory, _ := cfg["directory"].(string)
		fmt.Printf("  %s %s\n", labelStyle.Render("•"), e.ID.String())
		if directory != "" {
			fmt.Printf("    %s %s\n", dimStyle.Render("Path:"), directory)
		}
		if e.ID.NS != "" {
			fmt.Printf("    %s %s\n", dimStyle.Render("Namespace:"), e.ID.NS)
		}
		fmt.Println()
	}

	if count == 0 {
		fmt.Println(dimStyle.Render("  No fs.directory entries found"))
	} else {
		fmt.Printf(labelStyle.Render("Total: %d entries\n"), count)
	}
	fmt.Println()

	return nil
}
