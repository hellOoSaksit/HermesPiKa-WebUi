// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package version holds build information, set by the linker at release time.
package version

// Set with -ldflags "-X hpk.local/hpk/internal/version.Version=v0.1.0 ...".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
