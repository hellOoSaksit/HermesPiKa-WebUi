// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package safepath

import (
	"io/fs"
	"syscall"

	"golang.org/x/sys/windows"
)

// nameSurrogate is the reparse-tag bit Windows sets on tags that name another
// file or folder: symbolic links, junctions (mount points) and the like.
const nameSurrogate = 0x20000000

// redirects reports a reparse point that names another place. Go reports a
// junction as ModeIrregular, not ModeSymlink. Reparse points that only serve
// content (OneDrive placeholders, deduplicated files) are not links. When the
// tag cannot be read, the answer is yes.
func redirects(path string, fi fs.FileInfo) bool {
	attrs := uint32(0)
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		attrs = d.FileAttributes
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 && fi.Mode()&fs.ModeIrregular == 0 {
		return false
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	var data windows.Win32finddata
	h, err := windows.FindFirstFile(p, &data)
	if err != nil {
		return true
	}
	windows.FindClose(h)
	if data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return fi.Mode()&fs.ModeIrregular != 0
	}
	return data.Reserved0&nameSurrogate != 0
}
