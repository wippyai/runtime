<!-- SPDX-License-Identifier: MPL-2.0 -->

# Directory link policy

An `fs.directory` entry accepts optional `link_policy: contained | owner_safe`.
Omission (or `contained`) preserves `os.Root` containment for every operation.

On Unix, `owner_safe` also admits reads (`Stat`, `Open`, read-only `OpenFile`,
Lua `readfile` and `exists`) through links to external regular files. Resolution
allows at most 40 symlink expansions and detects repeated resolution states.
Absolute links whose canonical targets remain inside the configured volume
open through its retained `os.Root`, under the same authority as other contained
reads. Ownership checks apply only when the target escapes that boundary.
Owner-safe fallback opens are non-blocking until the opened descriptor is
validated as a regular file, so a FIFO cannot hang a read or `Stat` request.
The canonical target and **every canonical parent through the filesystem root**
must be owned by the process UID or root, with `mode & 022 == 0`. Sticky
permissions do not exempt a directory. This follows the owner/mode checks in
[OpenSSH `misc.c` `safe_path`](https://github.com/openssh/openssh-portable/blob/master/misc.c),
but always checks through filesystem root rather than stopping at the home.

The opened file and canonical parents are checked through retained descriptors,
with `O_NOFOLLOW` on each canonical component. Refusals name the failed path
and reason; Lua reports policy failures as permission errors, including from
`exists`. Missing ordinary files retain their existing not-found behavior.
Dangling links, loops, depth overflow and external nonregular targets are refused.

Writes, creates, deletes, renames, metadata changes and descriptor-relative
operations retain their existing containment. Removing or renaming an in-root
symlink changes that link only, never its external target. Directory listing
and no-follow operations do not gain external traversal.

On Windows and other non-Unix systems, ownership/ACL evidence is not implemented:
`owner_safe` behaves as `contained`. The field does not grant filesystem access;
host-selected permissions and read-only configuration still apply.
