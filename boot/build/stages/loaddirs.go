package stages

import (
	"context"
	"os"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	"go.uber.org/zap"
)

type loadDirsStage struct {
	dirs []string
}

// LoadDirs creates a new stage that loads entries from specified directories.
// This is a generic stage that has zero knowledge of lock files or module structure.
// It simply iterates through the provided directories and loads all YAML files.
// Each directory is loaded using os.DirFS() and the boot/loader package.
func LoadDirs(dirs []string) boot.Stage {
	return &loadDirsStage{
		dirs: dirs,
	}
}

func (s *loadDirsStage) Name() string {
	return "loaddirs"
}

func (s *loadDirsStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)

	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return ErrTranscoderNotFound
	}

	interpolator := interpolate.NewEntryInterpolator(dtt)
	ldr := loader.NewLoader(dtt, log, interpolator)

	for _, dir := range s.dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			log.Warn("directory not found, skipping", zap.String("dir", dir))
			continue
		}

		dirFS := os.DirFS(dir)
		loadedEntries, err := ldr.LoadFS(ctx, dirFS)
		if err != nil {
			return NewLoadDirectoryError(dir, err)
		}

		*entries = append(*entries, loadedEntries...)
	}

	return nil
}
