// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build !windows

package safepath

import "io/fs"

// redirects: outside Windows only symbolic links redirect (IsLink checks
// them); anything Go cannot classify is refused too.
func redirects(path string, fi fs.FileInfo) bool { return fi.Mode()&fs.ModeIrregular != 0 }
