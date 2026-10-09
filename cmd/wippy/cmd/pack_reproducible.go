// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"io/fs"
	"slices"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/wapp"
)

// A reusable module describes source content, not when a checkout or pack was
// created. Apply this at the writer boundary, including resources carried from
// dependency packs. Whole-application snapshots retain their original clocks.
func normalizeModulePack(metadata attrs.Bag, resources []wapp.ResourceSpec) []wapp.ResourceSpec {
	delete(metadata, "packed_at")
	result := slices.Clone(resources)
	for i := range result {
		result[i].FS = modulePackFS{result[i].FS}
	}
	return result
}

type modulePackFS struct{ fs.FS }

func (f modulePackFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return modulePackFile{file}, nil
}

func (f modulePackFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if err != nil {
		return nil, err
	}
	entries = slices.Clone(entries)
	for i := range entries {
		entries[i] = modulePackDirEntry{entries[i]}
	}
	return entries, nil
}

type modulePackFile struct{ fs.File }

func (f modulePackFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return modulePackFileInfo{info}, nil
}

type modulePackDirEntry struct{ fs.DirEntry }

func (e modulePackDirEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return modulePackFileInfo{info}, nil
}

type modulePackFileInfo struct{ fs.FileInfo }

func (modulePackFileInfo) ModTime() time.Time { return time.Unix(0, 0).UTC() }
