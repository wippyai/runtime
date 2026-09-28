// SPDX-License-Identifier: MPL-2.0

package fs

import "io/fs"

// DescriptorOpenRequest is the complete, already-authorized open contract for
// a child of a retained directory descriptor. Implementations must apply every
// requested condition as part of one kernel/capability operation; callers must
// not emulate any of these fields with Stat followed by Open.
type DescriptorOpenRequest struct {
	Read            bool
	Write           bool
	MutateDirectory bool
	Create          bool
	Directory       bool
	Exclusive       bool
	Truncate        bool
	NoFollow        bool
}

// DescriptorOpener opens a child relative to an already-open directory handle.
// It is deliberately separate from FS.OpenFile: a pathname re-resolution can
// change the target after a directory is renamed or replaced. Filesystems that
// cannot preserve this capability relationship must not implement it.
type DescriptorOpener interface {
	OpenDescriptorAt(directory fs.File, name string, request DescriptorOpenRequest) (File, error)
}

// DescriptorMutator performs a namespace mutation relative to retained
// directory handles. Each method must use a descriptor-rooted primitive; an
// implementation must return an unsupported error when it cannot do so.
type DescriptorMutator interface {
	CreateDirectoryAt(directory fs.File, name string, mode fs.FileMode) error
	RenameAt(oldDirectory fs.File, oldName string, newDirectory fs.File, newName string) error
	UnlinkFileAt(directory fs.File, name string) error
	RemoveDirectoryAt(directory fs.File, name string) error
}
